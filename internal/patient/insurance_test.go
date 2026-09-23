package patient_test

import (
	"context"
	"testing"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/patient"
	"advancedmd-token-management/internal/safeerrors"
)

// A bounded read sequence models the authoritative chart before and after a
// write. Provider mutations are still recorded by the shared test adapter.
type insuranceRead struct {
	chart domain.PatientDemographics
	err   error
}
type insuranceRecords struct {
	*advancedmdtest.Adapter
	reads []insuranceRead
}

func (r *insuranceRecords) GetPatientDemographics(_ context.Context, _ string) (domain.PatientDemographics, error) {
	r.DemographicCalls++
	read := r.reads[0]
	if len(r.reads) > 1 {
		r.reads = r.reads[1:]
	}
	return read.chart, read.err
}
func TestInsuranceReplacementOwnsReferencesAndWriteEffects(t *testing.T) {
	domain.InitRegistry("")
	old := domain.PatientDemographics{DOB: "01/15/1980", CarrierName: "Old", CarrierID: "car-old", InsPlanID: "fresh-plan", RespPartyID: "fresh-party", SubscriberNum: "OLD", InsuranceStateKnown: true}
	none := old
	none.CarrierName = ""
	none.CarrierID = ""
	none.InsPlanID = ""
	none.SubscriberNum = ""
	replaced := old
	replaced.CarrierID = writableMedicalCarrier
	replaced.SubscriberNum = "H123"
	replaced.InsPlanID = "new-plan"
	wrongDOB := old
	wrongDOB.DOB = "01/15/1990"
	unknown := old
	unknown.InsuranceStateKnown = false
	missingParty := old
	missingParty.RespPartyID = ""
	missingPlan := old
	missingPlan.InsPlanID = ""
	rejected := advancedmd.NewError(safeerrors.CategoryRejected)
	ambiguous := advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
	readFailure := advancedmd.NewError(safeerrors.CategoryNetwork)
	for _, tc := range []struct {
		name                                string
		initial                             domain.PatientDemographics
		after                               domain.PatientDemographics
		beforeErr, afterErr, endErr, addErr error
		effect                              string
		outcome                             patient.MutationOutcome
		ends, adds                          int
	}{
		{name: "fresh IDs override stale caller", initial: old, effect: "completed", ends: 1, adds: 1},
		{name: "no current plan attaches directly", initial: none, effect: "completed", adds: 1},
		{name: "already active does not write", initial: replaced, effect: "completed"},
		{name: "DOB mismatch", initial: wrongDOB, effect: "no_effect", outcome: patient.MutationValidationFailed},
		{name: "unknown initial state", initial: unknown, effect: "no_effect", outcome: patient.MutationValidationFailed},
		{name: "missing responsible party", initial: missingParty, effect: "no_effect", outcome: patient.MutationValidationFailed},
		{name: "carrier without plan is incomplete", initial: missingPlan, effect: "no_effect", outcome: patient.MutationValidationFailed},
		{name: "initial read fails", beforeErr: readFailure, effect: "no_effect", outcome: patient.MutationFailed},
		{name: "end rejected", initial: old, endErr: rejected, effect: "no_effect", outcome: patient.MutationRejected, ends: 1},
		{name: "end confirmed not applied", initial: old, after: old, endErr: ambiguous, effect: "no_effect", outcome: patient.MutationReconciledFailure, ends: 1},
		{name: "end reconciled", initial: old, after: none, endErr: ambiguous, effect: "completed", outcome: patient.MutationReconciledSuccess, ends: 1, adds: 1},
		{name: "end reconciliation sees replacement", initial: old, after: replaced, endErr: ambiguous, effect: "completed", outcome: patient.MutationReconciledSuccess, ends: 1},
		{name: "end uncertain", initial: old, afterErr: readFailure, endErr: ambiguous, effect: "uncertain", outcome: patient.MutationIndeterminateWrite, ends: 1},
		{name: "attach rejected after end", initial: old, addErr: rejected, effect: "partial", outcome: patient.MutationRejected, ends: 1, adds: 1},
		{name: "attach rejected without old plan", initial: none, addErr: rejected, effect: "no_effect", outcome: patient.MutationRejected, adds: 1},
		{name: "attach confirmed", initial: old, after: replaced, addErr: ambiguous, effect: "completed", outcome: patient.MutationReconciledSuccess, ends: 1, adds: 1},
		{name: "attach confirmed absent", initial: old, after: none, addErr: ambiguous, effect: "partial", outcome: patient.MutationReconciledFailure, ends: 1, adds: 1},
		{name: "attach absent without old plan", initial: none, after: none, addErr: ambiguous, effect: "no_effect", outcome: patient.MutationReconciledFailure, adds: 1},
		{name: "attach wrong member does not prove success", initial: old, after: old, addErr: ambiguous, effect: "uncertain", outcome: patient.MutationIndeterminateWrite, ends: 1, adds: 1},
		{name: "attach uncertain", initial: old, afterErr: readFailure, addErr: ambiguous, effect: "uncertain", outcome: patient.MutationIndeterminateWrite, ends: 1, adds: 1},
		{name: "attach incomplete reconciliation", initial: old, after: unknown, addErr: ambiguous, effect: "uncertain", outcome: patient.MutationIndeterminateWrite, ends: 1, adds: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := &insuranceRecords{Adapter: advancedmdtest.NewAdapter(), reads: []insuranceRead{{tc.initial, tc.beforeErr}, {tc.after, tc.afterErr}}}
			if tc.beforeErr != nil {
				records.reads = records.reads[:1]
			}
			records.EndInsuranceError = tc.endErr
			records.AddInsuranceError = tc.addErr
			command := validUpdateInsuranceCommand()
			command.InsPlanID = "stale-plan"
			command.RespPartyID = "stale-party"
			command.OldInsurance = "stale name"
			got := patient.New(records).UpdateInsurance(context.Background(), command)
			if got.Effect != tc.effect || got.Outcome != tc.outcome {
				t.Fatalf("result=%+v, want %s/%s", got, tc.effect, tc.outcome)
			}
			if (got.Status == patient.UpdateInsuranceStatusUpdated) != (tc.effect == "completed") {
				t.Fatalf("status=%s effect=%s", got.Status, got.Effect)
			}
			if records.EndInsuranceCalls != tc.ends || records.AddInsuranceCalls != tc.adds {
				t.Fatalf("writes end=%d add=%d", records.EndInsuranceCalls, records.AddInsuranceCalls)
			}
			for _, write := range records.InsuranceEnds {
				if write.InsPlanID != old.InsPlanID {
					t.Fatalf("trusted stale plan: %+v", write)
				}
			}
			for _, write := range records.Insurances {
				if write.RespPartyID != old.RespPartyID || write.CarrierID != writableMedicalCarrier {
					t.Fatalf("incorrect replacement: %+v", write)
				}
			}
			if tc.effect == "completed" && (got.OldInsurance != tc.initial.CarrierName || got.InsuranceDecision == nil) {
				t.Fatalf("incorrect receipt: %+v", got)
			}
		})
	}
}

func TestInsuranceReconciliationRetriesOnlyReads(t *testing.T) {
	initial := domain.PatientDemographics{DOB: "01/15/1980", RespPartyID: "resp123", InsuranceStateKnown: true}
	attached := initial
	attached.InsPlanID = "new-plan"
	attached.CarrierID = writableMedicalCarrier
	attached.SubscriberNum = "H123"
	records := &insuranceRecords{Adapter: advancedmdtest.NewAdapter(), reads: []insuranceRead{
		{chart: initial}, {err: advancedmd.NewError(safeerrors.CategoryTimeout)}, {chart: attached},
	}}
	records.AddInsuranceError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
	got := patient.New(records).UpdateInsurance(context.Background(), validUpdateInsuranceCommand())
	if got.Effect != "completed" || got.Outcome != patient.MutationReconciledSuccess || records.AddInsuranceCalls != 1 || records.EndInsuranceCalls != 0 || records.DemographicCalls != 3 {
		t.Fatalf("result=%+v reads=%d end/add=%d/%d", got, records.DemographicCalls, records.EndInsuranceCalls, records.AddInsuranceCalls)
	}
}
