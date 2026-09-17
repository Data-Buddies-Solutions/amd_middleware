package patient_test

import (
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/patient"
	"context"
	"testing"
)

func TestCorrectedCarrierIsValidatedBeforeAnyInsuranceMutation(t *testing.T) {
	for _, plan := range []string{"United Golden Rule", "United Individual Exchange", "United Global International Plan", "Humana Medicaid HMO", "Humana", "HUM03"} {
		records := advancedmdtest.NewAdapter()
		create := validCreateCommand()
		create.Office = "Hollywood"
		create.Insurance = plan
		created := patient.New(records).Create(context.Background(), create)
		if created.Outcome != patient.MutationValidationFailed || records.CreatePatientCalls != 0 {
			t.Fatalf("creation accepted unresolved mapping: %+v", created)
		}
		result := patient.New(records).UpdateInsurance(context.Background(), patient.UpdateInsuranceCommand{PatientID: "123", InsPlanID: "ins123", RespPartyID: "resp123", Insurance: plan, SubscriberNum: "synthetic", Office: "Hollywood"})
		if result.Outcome != patient.MutationValidationFailed {
			t.Fatalf("%s: %+v", plan, result)
		}
		if len(records.InsuranceEnds) != 0 || len(records.Insurances) != 0 {
			t.Fatal("mutation occurred before mapping validation")
		}
	}
}

func TestPreferredCareUsesItsOwnCarrierAndReceipt(t *testing.T) {
	records := advancedmdtest.NewAdapter()
	result := patient.New(records).UpdateInsurance(context.Background(), patient.UpdateInsuranceCommand{PatientID: "123", RespPartyID: "resp123", Insurance: "Preferred Care Partners", SubscriberNum: "synthetic", Office: "Hollywood", DOB: "01/02/1980"})
	if result.Status != patient.UpdateInsuranceStatusUpdated || result.InsuranceDecision == nil || result.InsuranceDecision.CarrierCode != "PRE04" {
		t.Fatalf("result=%+v", result)
	}
	if len(records.Insurances) != 1 || records.Insurances[0].CarrierID != "car40916" || result.Routing != domain.RoutingBachOnly {
		t.Fatalf("insurance=%+v", records.Insurances)
	}
}
