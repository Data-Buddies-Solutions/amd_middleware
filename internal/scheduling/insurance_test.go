package scheduling_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
	"advancedmd-token-management/internal/scheduling"
)

func TestExistingPatientListsAndBooksWithoutInsuranceClarification(t *testing.T) {
	for _, plan := range []string{"AETNA", "Unknown", ""} {
		t.Run(plan, func(t *testing.T) {
			records := recordsWithSetup(testColumn("1593", "2064", "1576", "09:00", "09:15", 15))
			records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: plan, CarrierID: "car40887"}
			now := mutationTestNow()
			svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
			result, err := svc.List(context.Background(), scheduling.ListCommand{PatientID: "12345", Office: "Crystal River", StartDate: "2026-06-03", DOB: "01/15/1980", VisitType: "medical"})
			if err != nil || result.Outcome != scheduling.AvailabilityOutcomeFound || len(result.Slots) == 0 {
				t.Fatalf("existing patient inventory blocked: err=%v result=%+v", err, result)
			}
			if records.DemographicCalls != 0 {
				t.Fatal("inventory unnecessarily rechecked chart insurance")
			}
			booked, err := svc.Book(context.Background(), scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", Office: "Crystal River", BookingToken: result.Slots[0].BookingToken, VisitCategory: "medical", AppointmentReason: "Eye follow-up", ReferringDoctor: "none"})
			if err != nil || booked.Status != "booked" || len(records.Bookings) != 1 {
				t.Fatalf("existing patient booking blocked: err=%v result=%+v writes=%d", err, booked, len(records.Bookings))
			}
		})
	}
}

func TestVisionCarrierIdentitySurvivesAvailabilityAndBooking(t *testing.T) {
	office, _ := domain.ResolveOffice("North Miami Beach Optical")
	accepted := insurance.DecideInsurance("Devoted", "routine_vision", office, "01/15/1980", mutationTestNow())
	records := recordsWithSetup(testColumn("1601", "621", "1582", "09:00", "09:15", 15))
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierID: accepted.CarrierID, CarrierName: "PREMIER EYE CARE"}
	now := mutationTestNow()
	svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
	available, err := svc.List(context.Background(), scheduling.ListCommand{PatientID: "12345", Office: office.DisplayName, DOB: "01/15/1980", VisitType: "routine_vision", StartDate: "2026-06-03"})
	if err != nil || len(available.Slots) == 0 {
		t.Fatalf("availability: %v %+v", err, available)
	}
	command := scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", Office: office.DisplayName, BookingToken: available.Slots[0].BookingToken, VisitCategory: "routine_vision", AppointmentReason: "Routine eye exam", ReferringDoctor: "none"}
	chart := records.Demographics["12345"]
	chart.CarrierName = "Renamed directory label"
	records.Demographics["12345"] = chart
	result, err := svc.Book(context.Background(), command)
	if err != nil || result.Status != "booked" || len(records.Bookings) != 1 {
		t.Fatalf("booking: %v %+v writes=%d", err, result, len(records.Bookings))
	}
}

func TestListWithPlanDecidesThePlanBeforeReadingInventory(t *testing.T) {
	office, _ := domain.ResolveOffice("Spring Hill")
	now := mutationTestNow()
	column := testColumn("1550", "620", "1568", "09:00", "09:15", 15)
	provider := office.Columns["1550"].ShortName
	var cleared, elsewhere, blocked string
	plans, _ := insurance.ListPlans("medical", office)
	for _, p := range plans.Plans {
		d := insurance.DecidePlan(p.PlanID, "medical", office, "01/15/1980", now)
		switch {
		case !d.CanSchedule && blocked == "":
			blocked = p.PlanID
		case d.CanSchedule && cleared == "" && slices.Contains(d.AllowedProviders, provider):
			cleared = p.PlanID
		case d.CanSchedule && elsewhere == "" && !slices.Contains(d.AllowedProviders, provider):
			elsewhere = p.PlanID
		}
	}
	if cleared == "" || blocked == "" || elsewhere == "" {
		t.Fatalf("plan data lacks a cleared (%q), elsewhere (%q) or blocked (%q) plan", cleared, elsewhere, blocked)
	}
	list := func(planID string) (scheduling.AvailabilityResponse, int) {
		records := recordsWithSetup(column)
		svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
		result, err := svc.List(context.Background(), scheduling.ListCommand{Office: "Spring Hill", StartDate: "2026-06-03", DOB: "01/15/1980", VisitType: "medical", InsurancePlanID: planID})
		if err != nil {
			t.Fatalf("List(%s) error = %v", planID, err)
		}
		return result, records.SchedulerSetupCalls
	}

	if result, _ := list(cleared); result.Outcome != scheduling.AvailabilityOutcomeFound || len(result.Slots) == 0 {
		t.Fatalf("cleared plan = %+v, want slots", result)
	}
	if result, _ := list(elsewhere); result.Outcome != scheduling.AvailabilityOutcomeNoEligibleProviders {
		t.Fatalf("plan for other doctors = %+v, want no eligible providers", result)
	}
	for _, planID := range []string{blocked, "not-a-plan"} {
		result, setupCalls := list(planID)
		if result.Outcome != scheduling.AvailabilityOutcomeInsuranceNotCleared || result.InsuranceDecision == nil || result.InsuranceDecision.CanSchedule || len(result.Slots) != 0 || setupCalls != 0 {
			t.Fatalf("plan %s = %+v setup=%d, want insurance_not_cleared before any provider read", planID, result, setupCalls)
		}
	}
}

func TestListWithPlanRequiresAVisitType(t *testing.T) {
	now := mutationTestNow()
	svc := scheduling.New(recordsWithSetup(), "test-booking-secret", func() time.Time { return now })
	if _, err := svc.List(context.Background(), scheduling.ListCommand{Office: "Crystal River", StartDate: "2026-06-03", DOB: "01/15/1980", InsurancePlanID: "self-pay-medical"}); err == nil {
		t.Fatal("List without visitType accepted an insurance plan")
	}
}
