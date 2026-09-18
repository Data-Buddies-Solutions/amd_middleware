package patient_test

import (
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/patient"
	"context"
	"testing"
)

func TestCorrectedCarrierIsValidatedBeforeAnyInsuranceMutation(t *testing.T) {
	for _, plan := range []string{"Clear Spring Health", "Unknown Plan", "HUM03"} {
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

func TestVerifiedMedicalCarriersReachInsuranceWrite(t *testing.T) {
	for _, tc := range []struct{ plan, id, code string }{
		{"Humana Medicaid HMO", "car303033", "HUM02"},
		{"Aetna", "car40887", "AET07"},
		{"Humana", "car303062", "HUM PPO"},
		{"Humana Medicare", "car40906", "HUM01"},
		{"Humana PPO", "car303062", "HUM PPO"},
		{"United Healthcare", "car40923", "UNI20"},
		{"United AARP Medicare Complete", "car302750", "AARPM"},
		{"United Global International Plan", "car284971", "UNIT15"},
		{"United Healthcare All Savers", "car284949", "ALL 1"},
		{"SunHealth", "car308086", "SUNHEALT"},
		{"Medicaid", "car40899", "FLO03"},
		{"United Golden Rule", "car40902", "GOL05"},
		{"United Individual Exchange", "car40923", "UNI20"},
		{"Cigna PPO", "car40895", "CIG09"},
		{"Molina Medicare", "car301507", "MOLI2"},
		{"Tricare Select", "car284327", "TRI00"},
	} {
		t.Run(tc.plan, func(t *testing.T) {
			records := advancedmdtest.NewAdapter()
			result := patient.New(records).UpdateInsurance(context.Background(), patient.UpdateInsuranceCommand{PatientID: "123", RespPartyID: "resp123", Insurance: tc.plan, SubscriberNum: "synthetic", Office: "Hollywood", DOB: "01/02/1980"})
			if result.Status != patient.UpdateInsuranceStatusUpdated || result.InsuranceDecision == nil || result.InsuranceDecision.CarrierCode != tc.code || len(records.Insurances) != 1 || records.Insurances[0].CarrierID != tc.id {
				t.Fatalf("wrong insurance write: result=%+v records=%+v", result, records.Insurances)
			}
			if tc.code == "HUM02" && result.InsuranceDecision.CanSchedule {
				t.Fatal("attachment cleared authorization")
			}
		})
	}
}
