package scheduling_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
	"advancedmd-token-management/internal/scheduling"
)

func TestExistingPatientListsAndBooksWithoutInsuranceClarification(t *testing.T) {
	for _, plan := range []string{"AETNA", "Unknown", ""} {
		t.Run(plan, func(t *testing.T) {
			records := recordsWithSetup(testColumn(1593, 2064, 1576, "09:00", "09:15", 15))
			records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: plan, CarrierID: "car40887"}
			now := mutationTestNow()
			svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
			result, err := svc.List(context.Background(), plannedSearch(scheduling.ListCommand{PatientID: "12345", Office: "Crystal River", StartDate: "2026-06-03", DOB: "01/15/1980", VisitType: "medical"}))
			if err != nil || result.Outcome != scheduling.AvailabilityOutcomeFound || len(result.Slots) == 0 {
				t.Fatalf("existing patient inventory blocked: err=%v result=%+v", err, result)
			}
			if records.DemographicCalls != 0 {
				t.Fatal("inventory unnecessarily rechecked chart insurance")
			}
			booked, err := svc.Book(context.Background(), scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", BookingToken: result.Slots[0].BookingToken, VisitCategory: "medical", AppointmentReason: "Eye follow-up", ReferringDoctor: "none"})
			if err != nil || booked.Status != "booked" || len(records.Bookings) != 1 {
				t.Fatalf("existing patient booking blocked: err=%v result=%+v writes=%d", err, booked, len(records.Bookings))
			}
		})
	}
}

func TestVisionCarrierIdentitySurvivesAvailabilityAndBooking(t *testing.T) {
	office, _ := domain.ResolveOffice("North Miami Beach Optical")
	accepted := insurance.DecidePlan("devoted-medicare-hmo-vision", "routine_vision", office, "01/15/1980", mutationTestNow())
	if !accepted.CanSchedule || accepted.CarrierID == "" {
		t.Fatalf("fixture plan not accepted: %+v", accepted)
	}
	records := recordsWithSetup(testColumn(1601, 621, 1582, "09:00", "09:15", 15))
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierID: accepted.CarrierID, CarrierName: "PREMIER EYE CARE"}
	now := mutationTestNow()
	svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
	available, err := svc.List(context.Background(), plannedSearch(scheduling.ListCommand{PatientID: "12345", Office: office.DisplayName, DOB: "01/15/1980", VisitType: "routine_vision", StartDate: "2026-06-03"}))
	if err != nil || len(available.Slots) == 0 {
		t.Fatalf("availability: %v %+v", err, available)
	}
	command := scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", BookingToken: available.Slots[0].BookingToken, VisitCategory: "routine_vision", AppointmentReason: "Routine eye exam", ReferringDoctor: "none"}
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
	column := testColumn(1550, 620, 1568, "09:00", "09:15", 15)
	provider := office.Columns[1550].ShortName
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
		result, err := svc.List(context.Background(), plannedSearch(scheduling.ListCommand{Office: "Spring Hill", StartDate: "2026-06-03", DOB: "01/15/1980", VisitType: "medical", InsurancePlanID: planID}))
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
	if _, err := svc.List(context.Background(), scheduling.ListCommand{PatientID: "12345", Office: "Crystal River", StartDate: "2026-06-03", DOB: "01/15/1980", InsurancePlanID: "self-pay-medical"}); err == nil {
		t.Fatal("List without visitType accepted an insurance plan")
	}
}

func TestBachAdultMedicalColumnShowsDrVillafuerteAndFollowsDrBachsInsurance(t *testing.T) {
	office, _ := domain.ResolveOffice("Hollywood")
	now := mutationTestNow()
	for planID, wantAccepted := range map[string]bool{"aetna-medicare-medical": true, "avmed-entrust-medical": false} {
		if d := insurance.DecidePlan(planID, "medical", office, "01/15/1980", now); d.CanSchedule != wantAccepted || (wantAccepted && !slices.Equal(d.AllowedProviders, []string{"Dr. Bach"})) {
			t.Fatalf("fixture plan %s = %+v, want accepted=%t for Dr. Bach", planID, d, wantAccepted)
		}
	}
	search := func(planID, dob string) (*advancedmdtest.Adapter, scheduling.Scheduling, scheduling.AvailabilityResponse) {
		records := recordsWithSetup(
			testColumn(1268, 620, 1480, "09:00", "09:15", 15),
			testColumn(1617, 620, 1480, "10:00", "10:15", 15),
		)
		records.Demographics["12345"] = domain.PatientDemographics{DOB: dob, CarrierName: "Aetna", CarrierID: "car40887"}
		records.CompletedVisits["12345"] = true
		records.BookAppointmentID = 98765
		svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
		result, err := svc.List(context.Background(), scheduling.ListCommand{PatientID: "12345", Office: "Hollywood", StartDate: "2026-06-03", DOB: dob, VisitType: "medical", InsurancePlanID: planID})
		if err != nil {
			t.Fatalf("List(%s, %s) error = %v", planID, dob, err)
		}
		return records, svc, result
	}

	records, svc, accepted := search("aetna-medicare-medical", "01/15/1980")
	if got := slotColumns(accepted.Slots); !slices.Equal(got, []int{1268, 1617}) {
		t.Fatalf("accepted plan columns = %v, want Dr. Bach's 1268 and the adult medical 1617", got)
	}
	villafuerte := accepted.Slots[slices.IndexFunc(accepted.Slots, func(slot scheduling.AvailabilitySlotOption) bool { return slot.ColumnID == 1617 })]
	if villafuerte.Provider != "Dr. Carol Villafuerte" || villafuerte.ProfileID != 620 {
		t.Fatalf("adult medical slot = %+v, want Dr. Carol Villafuerte on profile 620", villafuerte)
	}
	booked, err := svc.Book(context.Background(), scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", BookingToken: villafuerte.BookingToken, VisitCategory: "medical", VisitReason: "Glaucoma follow up", AppointmentReason: "Glaucoma follow up", ReferringDoctor: "none"})
	if err != nil || booked.ProviderName != "Dr. Carol Villafuerte" || len(records.Bookings) != 1 || records.Bookings[0].ColumnID != 1617 || records.Bookings[0].ProviderAppointmentTypeID != 1007 {
		t.Fatalf("book = %+v err=%v writes=%+v, want Established Adult Medical with Dr. Carol Villafuerte in 1617", booked, err, records.Bookings)
	}

	records, svc, postOpSearch := search("aetna-medicare-medical", "01/15/1980")
	postOpSlot := postOpSearch.Slots[slices.IndexFunc(postOpSearch.Slots, func(slot scheduling.AvailabilitySlotOption) bool { return slot.ColumnID == 1617 })]
	_, err = svc.Book(context.Background(), scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", BookingToken: postOpSlot.BookingToken, VisitCategory: "medical", VisitReason: "post-op check", AppointmentReason: "post-op check", ReferringDoctor: "none"})
	if err == nil || !strings.Contains(err.Error(), "Dr. Villafuerte sees only") || len(records.Bookings) != 0 {
		t.Fatalf("post-op book err = %v writes=%d, want a refusal naming Dr. Villafuerte", err, len(records.Bookings))
	}

	if _, _, minor := search("aetna-medicare-medical", "01/15/2010"); slices.Contains(slotColumns(minor.Slots), 1617) {
		t.Fatalf("minor columns = %v, want no adult medical column", slotColumns(minor.Slots))
	}
	if _, _, refused := search("avmed-entrust-medical", "01/15/1980"); refused.Outcome != scheduling.AvailabilityOutcomeInsuranceNotCleared || len(refused.Slots) != 0 {
		t.Fatalf("plan Dr. Bach does not take = %+v, want insurance_not_cleared", refused)
	}
}
