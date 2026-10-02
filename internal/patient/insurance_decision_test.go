package patient_test

import (
	"context"
	"testing"

	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
	"advancedmd-token-management/internal/patient"
)

func TestUndecidedInsuranceIsRejectedBeforeAnyInsuranceMutation(t *testing.T) {
	for _, plan := range []string{"Aetna", "Unknown Plan", "health plan"} {
		records := advancedmdtest.NewAdapter()
		records.Demographics["123"] = domain.PatientDemographics{DOB: "01/02/1980", RespPartyID: "resp123", InsuranceStateKnown: true}
		create := validCreateCommand()
		create.Office = "Hollywood"
		create.Insurance = plan
		created := patient.New(records, testAppointmentTokens).Create(context.Background(), create)
		if created.Outcome != patient.MutationValidationFailed || records.CreatePatientCalls != 0 {
			t.Fatalf("%s: creation accepted an undecided plan: %+v", plan, created)
		}
		result := patient.New(records, testAppointmentTokens).UpdateInsurance(context.Background(), patient.UpdateInsuranceCommand{PatientID: "123", DOB: "01/02/1980", Insurance: plan, SubscriberNum: "synthetic", Office: "Hollywood"})
		if result.Outcome != patient.MutationValidationFailed {
			t.Fatalf("%s: %+v", plan, result)
		}
		if len(records.InsuranceEnds) != 0 || len(records.Insurances) != 0 {
			t.Fatal("mutation occurred before insurance validation")
		}
	}
}

func TestInsurancePlanIDDecidesWritesWithoutMatchingTheName(t *testing.T) {
	springHill, _ := domain.LookupOfficeByID("spring_hill")
	accepted := insurance.DecideInsurance(writableMedicalPlan, "medical", springHill, "01/15/1980")
	if accepted.Outcome != "accepted" || accepted.PlanID == "" || accepted.CarrierID != writableMedicalCarrier {
		t.Fatalf("fixture plan not accepted: %+v", accepted)
	}

	records := advancedmdtest.NewAdapter()
	create := validCreateCommand()
	create.Insurance = "words that match no plan"
	create.InsurancePlanID = accepted.PlanID
	created := patient.New(records, testAppointmentTokens).Create(context.Background(), create)
	if created.Status != patient.CreateStatusCreated || created.InsuranceDecision.PlanID != accepted.PlanID ||
		len(records.Insurances) != 1 || records.Insurances[0].CarrierID != accepted.CarrierID {
		t.Fatalf("create = %+v insurances = %+v", created, records.Insurances)
	}

	records = advancedmdtest.NewAdapter()
	records.Demographics["123"] = domain.PatientDemographics{DOB: "01/15/1980", RespPartyID: "resp123", InsuranceStateKnown: true}
	update := validUpdateInsuranceCommand()
	update.Insurance = "words that match no plan"
	update.InsurancePlanID = accepted.PlanID
	updated := patient.New(records, testAppointmentTokens).UpdateInsurance(context.Background(), update)
	if updated.Status != patient.UpdateInsuranceStatusUpdated || len(records.Insurances) != 1 || records.Insurances[0].CarrierID != accepted.CarrierID {
		t.Fatalf("update = %+v insurances = %+v", updated, records.Insurances)
	}
}

func TestUnknownInsurancePlanIDIsRejectedBeforeWrites(t *testing.T) {
	records := advancedmdtest.NewAdapter()
	records.Demographics["123"] = domain.PatientDemographics{DOB: "01/15/1980", RespPartyID: "resp123", InsuranceStateKnown: true}
	create := validCreateCommand()
	create.InsurancePlanID = "no-such-plan"
	created := patient.New(records, testAppointmentTokens).Create(context.Background(), create)
	update := validUpdateInsuranceCommand()
	update.InsurancePlanID = "no-such-plan"
	updated := patient.New(records, testAppointmentTokens).UpdateInsurance(context.Background(), update)
	if created.Outcome != patient.MutationValidationFailed || updated.Outcome != patient.MutationValidationFailed ||
		records.CreatePatientCalls != 0 || len(records.Insurances) != 0 || len(records.InsuranceEnds) != 0 {
		t.Fatalf("create = %+v update = %+v", created, updated)
	}
}

func TestSelfPayPlanDefaultsTheSubscriberNumber(t *testing.T) {
	records := advancedmdtest.NewAdapter()
	create := validCreateCommand()
	create.Insurance = "Self Pay"
	create.SubscriberNum = ""
	created := patient.New(records, testAppointmentTokens).Create(context.Background(), create)
	if created.Status != patient.CreateStatusCreated || !created.InsuranceDecision.SelfPay ||
		len(records.Insurances) != 1 || records.Insurances[0].SubscriberNum != "self pay" {
		t.Fatalf("create = %+v insurances = %+v", created, records.Insurances)
	}
}
