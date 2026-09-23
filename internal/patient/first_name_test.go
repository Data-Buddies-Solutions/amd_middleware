package patient_test

import (
	"context"
	"fmt"
	"testing"

	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/patient"
)

func TestFirstNameDOBResolution(t *testing.T) {
	domain.InitRegistry("")
	valid := domain.Patient{ID: "1", FirstName: "Jane", LastName: "Meyer", FullName: "MEYER,JANE", DOB: "01/01/1980"}
	missing := domain.Patient{ID: "2", FirstName: "Jane", LastName: "Other", FullName: "OTHER,JANE"}
	otherName := missing
	otherName.FirstName = "Janet"
	otherName.FullName = "OTHER,JANET"
	manyMissing := []domain.Patient{valid}
	for i := 0; i < 20; i++ {
		manyMissing = append(manyMissing, domain.Patient{ID: fmt.Sprint(i + 2), FirstName: "Jane", FullName: "OTHER,JANE"})
	}
	otherDOB := missing
	otherDOB.FirstName = ""
	otherDOB.FullName = ""
	otherDOB.DOB = "01/01/1990"
	for _, tc := range []struct {
		name                string
		rows                []domain.Patient
		incomplete          bool
		ignoredDemographics domain.PatientDemographics
		want                patient.Status
		reason              string
		reads               int
	}{
		{name: "two exact matches remain ambiguous", rows: []domain.Patient{valid, {ID: "2", FirstName: "Jane", LastName: "Other", FullName: "OTHER,JANE", DOB: "01/01/1980"}}, want: patient.StatusMultipleMatches},
		{name: "exact match ignores many missing DOB records", rows: manyMissing, want: patient.StatusVerified, reads: 1},
		{name: "unique", rows: []domain.Patient{valid}, want: patient.StatusVerified, reads: 1},
		{name: "missing DOB on nonmatching prefix", rows: []domain.Patient{valid, otherName}, want: patient.StatusVerified, reads: 1},
		{name: "missing name with different DOB", rows: []domain.Patient{valid, otherDOB}, want: patient.StatusVerified, reads: 1},
		{name: "missing DOB neighbor is discarded", rows: []domain.Patient{valid, missing}, ignoredDemographics: domain.PatientDemographics{FullName: "OTHER,JANE", DOB: "01/01/1990"}, want: patient.StatusVerified, reads: 1},
		{name: "discarded neighbor is not read", rows: []domain.Patient{valid, missing}, ignoredDemographics: domain.PatientDemographics{FullName: "OTHER,JANE", DOB: "01/01/1980"}, want: patient.StatusVerified, reads: 1},
		{name: "missing DOB does not block exact match", rows: []domain.Patient{valid, missing}, want: patient.StatusVerified, reads: 1},
		{name: "only missing DOB records means no match", rows: []domain.Patient{missing}, want: patient.StatusNotFound},
		{name: "empty complete", want: patient.StatusNotFound},
		{name: "empty incomplete", incomplete: true, want: patient.StatusUnresolved, reason: "incomplete_search"},
		{name: "unique but truncated", rows: []domain.Patient{valid}, incomplete: true, want: patient.StatusUnresolved, reason: "incomplete_search"},
		{name: "different exact name", rows: []domain.Patient{otherName}, want: patient.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			amd := advancedmdtest.NewAdapter()
			amd.CandidateReads["Jane"] = domain.PatientCandidateRead{Patients: tc.rows, Complete: !tc.incomplete}
			amd.Demographics["1"] = domain.PatientDemographics{FullName: valid.FullName, DOB: valid.DOB}
			amd.Demographics["2"] = tc.ignoredDemographics
			result, err := patient.New(amd).Resolve(context.Background(), patient.ResolveCommand{FirstName: "Jane", DOB: "1980-01-01", OfficeID: "spring_hill"})
			if err != nil || result.Status != tc.want || result.Reason != tc.reason {
				t.Fatalf("status=%s reason=%s err=%v", result.Status, result.Reason, err)
			}
			if amd.DemographicCalls != tc.reads {
				t.Fatalf("demographics=%d want=%d", amd.DemographicCalls, tc.reads)
			}
			if result.Observation.DemographicReads != tc.reads {
				t.Fatalf("reported demographics=%d", result.Observation.DemographicReads)
			}
			if tc.want == patient.StatusVerified {
				if result.PatientID != "1" || amd.AppointmentReadCalls != 1 {
					t.Fatal("must hydrate only the verified patient")
				}
			} else if amd.AppointmentReadCalls != 0 {
				t.Fatal("must not hydrate unresolved/ambiguous patients")
			}
			if tc.want == patient.StatusMultipleMatches && len(result.Matches) != 2 {
				t.Fatal("must preserve ambiguity")
			}
		})
	}
}

func TestFirstNameDOBMissingRecordsAndChangedIdentity(t *testing.T) {
	domain.InitRegistry("")
	for _, changed := range []bool{false, true} {
		amd := advancedmdtest.NewAdapter()
		rows := []domain.Patient{}
		for i := 0; i < 6; i++ {
			rows = append(rows, domain.Patient{ID: fmt.Sprint(i), FirstName: "Jane", FullName: "EXAMPLE,JANE"})
		}
		if changed {
			rows = []domain.Patient{{ID: "0", FirstName: "Jane", FullName: "EXAMPLE,JANE", DOB: "01/01/1980"}}
			amd.Demographics["0"] = domain.PatientDemographics{FullName: "EXAMPLE,JOHN", DOB: "01/01/1980"}
		}
		amd.CandidateReads["Jane"] = domain.PatientCandidateRead{Patients: rows, Complete: true}
		result, err := patient.New(amd).Resolve(context.Background(), patient.ResolveCommand{FirstName: "Jane", DOB: "01/01/1980", OfficeID: "spring_hill"})
		want := patient.StatusNotFound
		if changed {
			want = patient.StatusUnresolved
		}
		if err != nil || result.Status != want || amd.DemographicCalls > 1 || amd.AppointmentReadCalls != 0 {
			t.Fatalf("status=%s demographics=%d err=%v", result.Status, amd.DemographicCalls, err)
		}
	}
}
