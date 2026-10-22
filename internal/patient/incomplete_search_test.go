package patient_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/patient"
)

const (
	completeEmptyLookup = `{"PPMDResults":{"Results":{"patientlist":{"@itemcount":"0","@page":"1","@pagecount":"0"}}}}`
	legacyJaneLookup    = `{"PPMDResults":{"Results":{"patientlist":{"@itemcount":"1","patient":{"@id":"pat123","@name":"DOE,JANE","@dob":"01/15/1980","contactinfo":{"@homephone":"(954)287-2010"}}}}}}`
	legacyRobinLookup   = `{"PPMDResults":{"Results":{"patientlist":{"@itemcount":"1","patient":{"@id":"pat555","@name":"DOE,ROBIN","@dob":"03/20/1982","contactinfo":{"@homephone":"(954)287-2010"}}}}}}`
	addedJane           = `{"PPMDResults":{"Results":{"patientlist":{"patient":{"@id":"pat900","@name":"DOE,JANE","@respparty":"resp900"}}}}}`
	janeDemographics    = `{"PPMDResults":{"Results":{"patientlist":{"patient":{"@id":"pat123","@name":"DOE,JANE","@respparty":"resp123","@dob":"01/15/1980"}}}}}`
	insuranceSaved      = `{"PPMDResults":{"Results":{"patient":{"@id":"pat123"}}}}`
)

func TestCreateWritesOnlyAfterACompleteBaselineSearch(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		amd := advancedmdtest.NewAdapter()
		amd.IncompletePatientSearch = map[domain.PatientSearch]bool{{Phone: "9542872010"}: incomplete}

		got := patient.New(amd, testAppointmentTokens, testNow).Create(context.Background(), validCreateCommand())

		if complete := got.Observation.BaselineSearchComplete; complete == nil || *complete != !incomplete {
			t.Fatalf("incomplete=%v: BaselineSearchComplete = %v, want %v", incomplete, complete, !incomplete)
		}
		if !incomplete {
			if got.Status != patient.CreateStatusCreated || amd.CreatePatientCalls != 1 {
				t.Fatalf("complete baseline: Create() = %+v with %d writes, want one created patient", got, amd.CreatePatientCalls)
			}
			continue
		}
		if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationFailed {
			t.Fatalf("incomplete baseline: Create() = %+v, want a failed outcome with no write", got)
		}
		if !strings.Contains(got.Message, "No chart was created") || !strings.Contains(got.Message, "incomplete") {
			t.Fatalf("incomplete baseline: Message = %q, want it to say no chart was created because the search was incomplete", got.Message)
		}
		if amd.CreatePatientCalls != 0 || amd.AddInsuranceCalls != 0 {
			t.Fatalf("incomplete baseline: writes = create:%d insurance:%d, want 0/0", amd.CreatePatientCalls, amd.AddInsuranceCalls)
		}
	}
}

func TestCreateMakesNoAddPatientCallAfterALegacyUnpagedBaselineSearch(t *testing.T) {
	provider := &advancedmdtest.Provider{Responses: map[string][]advancedmdtest.Response{
		"lookuppatient": {{Body: legacyRobinLookup}},
		"addpatient":    {{Body: addedJane}},
		"addinsurance":  {{Body: insuranceSaved}},
	}}

	got := patient.New(provider.Adapter(testNow), testAppointmentTokens, testNow).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationFailed || got.PatientID != "" {
		t.Fatalf("Create() = %+v, want a failed outcome with no chart", got)
	}
	if requests := provider.Requests(); slices.Contains(requests, "addpatient") || slices.Contains(requests, "addinsurance") {
		t.Fatalf("AdvancedMD requests = %q, want no addpatient or addinsurance", requests)
	}
}

func TestCreateKeepsAnAmbiguousWriteIndeterminateWhenTheReconcileSearchIsIncomplete(t *testing.T) {
	provider := &advancedmdtest.Provider{Responses: map[string][]advancedmdtest.Response{
		"lookuppatient":  {{Body: completeEmptyLookup}, {Body: legacyJaneLookup}, {Body: legacyJaneLookup}, {Body: legacyJaneLookup}},
		"addpatient":     {{Status: http.StatusServiceUnavailable, Body: `{}`}},
		"getdemographic": {{Body: janeDemographics}},
		"addinsurance":   {{Body: insuranceSaved}},
	}}

	got := patient.New(provider.Adapter(testNow), testAppointmentTokens, testNow).Create(context.Background(), validCreateCommand())

	if got.Status != patient.CreateStatusError || got.Outcome != patient.MutationIndeterminateWrite || got.PatientID != "" {
		t.Fatalf("Create() = %+v, want an indeterminate write", got)
	}
	if requests := provider.Requests(); slices.Contains(requests, "addinsurance") {
		t.Fatalf("AdvancedMD requests = %q, want no addinsurance", requests)
	}
}
