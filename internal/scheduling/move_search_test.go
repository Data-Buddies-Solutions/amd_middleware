package scheduling_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
)

const (
	springHillBach    = 1513
	springHillBach2   = 1598
	springHillLicht   = 1551
	crystalRiverLicht = 1593
	hollywoodBach     = 1268
)

type moveFixture struct {
	records *advancedmdtest.Adapter
	svc     scheduling.Scheduling
	tokens  *scheduling.AppointmentTokens
}

func newMoveFixture() moveFixture {
	now := mutationTestNow()
	clock := func() time.Time { return now }
	records := recordsWithSetup(
		testColumn(springHillBach, 620, 1568, "09:00", "09:15", 15),
		testColumn(springHillBach2, 620, 1568, "10:00", "10:15", 15),
		testColumn(springHillLicht, 2064, 1568, "11:00", "11:15", 15),
		testColumn(crystalRiverLicht, 2064, 1576, "12:00", "12:15", 15),
		testColumn(hollywoodBach, 620, 1480, "13:00", "13:15", 15),
	)
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: "Self Pay", CarrierID: "car301672"}
	records.BookAppointmentID = 98765
	return moveFixture{records: records, svc: scheduling.New(records, "test-booking-secret", clock), tokens: scheduling.NewAppointmentTokens("test-booking-secret", clock)}
}

func (f moveFixture) rescheduleToken(t *testing.T, appointment domain.PatientAppointment) string {
	t.Helper()
	token, err := f.tokens.IssueRescheduleToken("12345", appointment)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func bachVisit(id int) domain.PatientAppointment {
	visit := matchingAppointmentAt(id, time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC))
	visit.ColumnID = springHillBach
	return visit
}

func crystalRiverVisit(id int) domain.PatientAppointment {
	visit := bachVisit(id)
	visit.ColumnID, visit.OfficeID, visit.Office, visit.AppointmentTypeID = crystalRiverLicht, "crystal_river", "Crystal River", 6169
	return visit
}

func (f moveFixture) search(t *testing.T, command scheduling.ListCommand) scheduling.AvailabilityResponse {
	t.Helper()
	command.PatientID, command.StartDate, command.DOB = "12345", "2026-06-03", "01/15/1980"
	available, err := f.svc.List(context.Background(), command)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	return available
}

func moveCommand(slotToken, rescheduleToken string) scheduling.BookCommand {
	return scheduling.BookCommand{
		PatientID: "12345", DOB: "01/15/1980", BookingToken: slotToken, RescheduleToken: rescheduleToken,
		AppointmentReason: "Medical follow up", ReferringDoctor: "none",
	}
}

func signedPolicy(t *testing.T, slot scheduling.AvailabilitySlotOption) scheduling.SlotPolicy {
	t.Helper()
	policy, err := scheduling.VerifySlotToken("test-booking-secret", slot.BookingToken, mutationTestNow())
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func slotColumns(slots []scheduling.AvailabilitySlotOption) []int {
	var columns []int
	for _, slot := range slots {
		if !slices.Contains(columns, slot.ColumnID) {
			columns = append(columns, slot.ColumnID)
		}
	}
	slices.Sort(columns)
	return columns
}

func TestMoveSearchDerivesOfficeVisitAndADoctorSpanningTwoColumnsFromTheToken(t *testing.T) {
	f := newMoveFixture()
	available := f.search(t, scheduling.ListCommand{RescheduleToken: f.rescheduleToken(t, bachVisit(54321))})
	if available.Outcome != scheduling.AvailabilityOutcomeFound {
		t.Fatalf("%q: %s", available.Outcome, available.Message)
	}
	if got := slotColumns(available.Slots); !slices.Equal(got, []int{springHillBach, springHillBach2}) {
		t.Fatalf("columns = %v, want both of the doctor's columns and no other doctor", got)
	}
	policy := signedPolicy(t, available.Slots[0])
	if policy.OfficeID != "spring_hill" || policy.MoveOf != 54321 || policy.PatientID != "12345" {
		t.Fatalf("policy = %+v", policy)
	}
}

func TestBookThenMoveThenChainedMove(t *testing.T) {
	f := newMoveFixture()
	planned := f.search(t, scheduling.ListCommand{Office: "Spring Hill", VisitType: "medical", InsurancePlanID: "self-pay-medical"})
	if planned.Outcome != scheduling.AvailabilityOutcomeFound {
		t.Fatalf("%q: %s", planned.Outcome, planned.Message)
	}
	first := slices.IndexFunc(planned.Slots, func(slot scheduling.AvailabilitySlotOption) bool { return slot.ColumnID == springHillBach })
	if first < 0 {
		t.Fatalf("no Bach slot in %v", slotColumns(planned.Slots))
	}
	policy := signedPolicy(t, planned.Slots[first])
	if policy.MoveOf != 0 {
		t.Fatalf("plan search policy = %+v", policy)
	}
	command := moveCommand(planned.Slots[first].BookingToken, "")
	command.VisitCategory = "medical"
	booked, err := f.svc.Book(context.Background(), command)
	if err != nil || booked.RescheduleToken == "" {
		t.Fatalf("book: %+v %v", booked, err)
	}
	moved := f.moveOnce(t, booked, 99001)
	moved = f.moveOnce(t, moved, 99002)
	if moved.AppointmentID != 99002 || len(f.records.Cancellations) != 2 {
		t.Fatalf("chained move: %+v cancellations=%d", moved, len(f.records.Cancellations))
	}
}

func (f moveFixture) moveOnce(t *testing.T, current scheduling.BookReceipt, replacement int) scheduling.BookReceipt {
	t.Helper()
	available := f.search(t, scheduling.ListCommand{RescheduleToken: current.RescheduleToken})
	if available.Outcome != scheduling.AvailabilityOutcomeFound {
		t.Fatalf("move search of %d: %q %s", current.AppointmentID, available.Outcome, available.Message)
	}
	if got := slotColumns(available.Slots); !slices.Equal(got, []int{springHillBach, springHillBach2}) {
		t.Fatalf("move search of %d: columns %v", current.AppointmentID, got)
	}
	slot := available.Slots[len(available.Slots)-1]
	if policy := signedPolicy(t, slot); policy.MoveOf != current.AppointmentID {
		t.Fatalf("move search of %d: policy %+v", current.AppointmentID, policy)
	}
	start, err := time.Parse(domain.SlotDateTimeLayout, current.StartDatetime)
	if err != nil {
		t.Fatal(err)
	}
	visit := domain.PatientAppointment{ID: current.AppointmentID, ColumnID: springHillBach, Start: start, AppointmentTypeID: current.AppointmentTypeID, OfficeID: "spring_hill", Office: "Spring Hill"}
	f.records.AppointmentResults["12345"] = appointmentResult([]domain.PatientAppointment{visit}, true)
	f.records.BookAppointmentID = replacement
	receipt, err := f.svc.Reschedule(context.Background(), moveCommand(slot.BookingToken, current.RescheduleToken))
	if err != nil || receipt.Status != "completed" || receipt.Booking.AppointmentID != replacement {
		t.Fatalf("move of %d: %+v err=%v", current.AppointmentID, receipt, err)
	}
	return *receipt.Booking
}

func TestPlanGatedMoveSignsBothClaimsAndTakesOfficeFromRequestThenToken(t *testing.T) {
	f := newMoveFixture()
	token := f.rescheduleToken(t, bachVisit(54321))
	for _, office := range []string{"", "Spring Hill"} {
		available := f.search(t, scheduling.ListCommand{RescheduleToken: token, InsurancePlanID: "self-pay-medical", Office: office})
		if available.Outcome != scheduling.AvailabilityOutcomeFound {
			t.Fatalf("office %q: %q %s", office, available.Outcome, available.Message)
		}
		policy := signedPolicy(t, available.Slots[0])
		if policy.OfficeID != "spring_hill" || policy.MoveOf != 54321 {
			t.Fatalf("office %q: policy %+v", office, policy)
		}
	}
}

func TestPlanGatedMoveSearchAgreesWithRescheduleAboutTheDestinationOffice(t *testing.T) {
	for name, c := range map[string]struct {
		visit     domain.PatientAppointment
		office    string
		wantStaff bool
	}{
		"Spring Hill type to Crystal River":   {bachVisit(54321), "Crystal River", true},
		"Crystal River type to Spring Hill":   {crystalRiverVisit(54321), "Spring Hill", true},
		"Crystal River type to Hollywood":     {crystalRiverVisit(54321), "Hollywood", true},
		"Spring Hill type to Hollywood":       {bachVisit(54321), "Hollywood", false},
		"Crystal River type to Crystal River": {crystalRiverVisit(54321), "Crystal River", false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newMoveFixture()
			f.records.AppointmentResults["12345"] = appointmentResult([]domain.PatientAppointment{c.visit}, true)
			token := f.rescheduleToken(t, c.visit)
			answer := f.search(t, scheduling.ListCommand{RescheduleToken: token, InsurancePlanID: "self-pay-medical", Office: c.office})
			if !c.wantStaff {
				if answer.Outcome != scheduling.AvailabilityOutcomeFound {
					t.Fatalf("search: %q %s", answer.Outcome, answer.Message)
				}
				moved, err := f.svc.Reschedule(context.Background(), moveCommand(answer.Slots[0].BookingToken, token))
				if err != nil || moved.Status != "completed" || len(f.records.Bookings) != 1 {
					t.Fatalf("reschedule: %+v err=%v bookings=%d", moved, err, len(f.records.Bookings))
				}
				return
			}
			if answer.Outcome != scheduling.AvailabilityOutcomeMoveNeedsStaff || answer.Status != scheduling.AvailabilityStatusSuccess ||
				answer.NextAction != scheduling.AvailabilityNextActionStaffHelp || answer.Message == "" || answer.Slots == nil || len(answer.Slots) != 0 {
				t.Fatalf("search answer = %+v", answer)
			}
			if f.records.SchedulerSetupCalls != 0 {
				t.Fatalf("staff answer read the scheduler %d times", f.records.SchedulerSetupCalls)
			}
			moved, err := f.svc.Reschedule(context.Background(), moveCommand(f.moveSlotAt(t, c.office, c.visit.ID), token))
			if scheduling.CategoryOf(err) != scheduling.CategoryValidation || !strings.Contains(err.Error(), "does not support the existing appointment type") {
				t.Fatalf("reschedule of the slot the search refused to offer: %+v err=%v", moved, err)
			}
			if len(f.records.Bookings) != 0 || len(f.records.Cancellations) != 0 {
				t.Fatalf("wrote %d bookings and %d cancellations", len(f.records.Bookings), len(f.records.Cancellations))
			}
		})
	}
}

func (f moveFixture) moveSlotAt(t *testing.T, office string, moveOf int) string {
	t.Helper()
	opening := f.search(t, scheduling.ListCommand{Office: office, VisitType: "medical", InsurancePlanID: "self-pay-medical"})
	if opening.Outcome != scheduling.AvailabilityOutcomeFound {
		t.Fatalf("opening at %s: %q %s", office, opening.Outcome, opening.Message)
	}
	policy := signedPolicy(t, opening.Slots[0])
	policy.MoveOf = moveOf
	token, err := scheduling.SignSlotToken("test-booking-secret", policy)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestMoveSearchChecksTheRoutingThePatientsAgeSelects(t *testing.T) {
	f := newMoveFixture()
	answer, err := f.svc.List(context.Background(), scheduling.ListCommand{
		PatientID: "12345", StartDate: "2026-06-03", DOB: "01/15/2020", RescheduleToken: f.rescheduleToken(t, crystalRiverVisit(54321)),
	})
	if err != nil || answer.Outcome != scheduling.AvailabilityOutcomeMoveNeedsStaff {
		t.Fatalf("pediatric move at an office that does not see children: %+v err=%v", answer, err)
	}
	if f.records.SchedulerSetupCalls != 0 {
		t.Fatalf("staff answer read the scheduler %d times", f.records.SchedulerSetupCalls)
	}
}

func TestMoveSearchRefusals(t *testing.T) {
	f := newMoveFixture()
	token := f.rescheduleToken(t, bachVisit(54321))
	for name, command := range map[string]scheduling.ListCommand{
		"visit type disagrees":          {RescheduleToken: token, VisitType: "routine_vision"},
		"plan visit type disagrees":     {RescheduleToken: token, VisitType: "routine_vision", InsurancePlanID: "self-pay-medical"},
		"office disagrees without plan": {RescheduleToken: token, Office: "Hollywood"},
	} {
		command.PatientID, command.StartDate, command.DOB = "12345", "2026-06-03", "01/15/1980"
		_, err := f.svc.List(context.Background(), command)
		if err == nil || scheduling.CategoryOf(err) != scheduling.CategoryValidation {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if f.records.SchedulerSetupCalls != 0 {
		t.Errorf("refused searches read the scheduler %d times", f.records.SchedulerSetupCalls)
	}
}

func TestMoveSearchRefusesAnExpiredOrForeignToken(t *testing.T) {
	f := newMoveFixture()
	token := f.rescheduleToken(t, bachVisit(54321))
	later := mutationTestNow().Add(16 * time.Minute)
	expired := scheduling.New(f.records, "test-booking-secret", func() time.Time { return later })
	for name, c := range map[string]struct {
		svc     scheduling.Scheduling
		patient string
		token   string
	}{
		"expired":       {expired, "12345", token},
		"other patient": {f.svc, "67890", token},
		"tampered":      {f.svc, "12345", token + "x"},
	} {
		_, err := c.svc.List(context.Background(), scheduling.ListCommand{PatientID: c.patient, RescheduleToken: c.token, StartDate: "2026-06-30", DOB: "01/15/1980"})
		if scheduling.CategoryOf(err) != scheduling.CategoryInvalidRescheduleToken {
			t.Errorf("%s: err=%v category=%q", name, err, scheduling.CategoryOf(err))
		}
	}
}

func TestMoveTokenWithoutADoctorKeyGoesToStaffUnlessPlanGated(t *testing.T) {
	f := newMoveFixture()
	visit := bachVisit(54321)
	visit.ColumnID = 0
	token := f.rescheduleToken(t, visit)
	answer := f.search(t, scheduling.ListCommand{RescheduleToken: token})
	if answer.Outcome != scheduling.AvailabilityOutcomeMoveNeedsStaff || answer.Status != scheduling.AvailabilityStatusSuccess ||
		answer.NextAction != scheduling.AvailabilityNextActionStaffHelp || answer.Message == "" || answer.Slots == nil || len(answer.Slots) != 0 {
		t.Fatalf("answer = %+v", answer)
	}
	if f.records.SchedulerSetupCalls != 0 {
		t.Fatalf("staff answer read the scheduler %d times", f.records.SchedulerSetupCalls)
	}
	if planned := f.search(t, scheduling.ListCommand{RescheduleToken: token, InsurancePlanID: "self-pay-medical"}); planned.Outcome != scheduling.AvailabilityOutcomeFound {
		t.Fatalf("plan-gated move without a doctor key: %q %s", planned.Outcome, planned.Message)
	}
}

func TestMoveSlotCannotBookOrMoveAnotherAppointment(t *testing.T) {
	f := newMoveFixture()
	visit := bachVisit(54321)
	other := bachVisit(54322)
	f.records.AppointmentResults["12345"] = appointmentResult([]domain.PatientAppointment{visit, other}, true)
	slot := f.search(t, scheduling.ListCommand{RescheduleToken: f.rescheduleToken(t, visit)}).Slots[0]
	command := moveCommand(slot.BookingToken, "")
	command.VisitCategory = "medical"
	if _, err := f.svc.Book(context.Background(), command); scheduling.CategoryOf(err) != scheduling.CategoryInvalidBookingToken {
		t.Errorf("book with a move slot: err=%v", err)
	}
	if _, err := f.svc.Reschedule(context.Background(), moveCommand(slot.BookingToken, f.rescheduleToken(t, other))); scheduling.CategoryOf(err) != scheduling.CategoryInvalidBookingToken {
		t.Errorf("move of another appointment: err=%v", err)
	}
	if len(f.records.Bookings) != 0 || len(f.records.Cancellations) != 0 {
		t.Errorf("wrote %d bookings and %d cancellations", len(f.records.Bookings), len(f.records.Cancellations))
	}
}

func TestPlanlessSearchWithoutAMoveTokenIsRefused(t *testing.T) {
	f := newMoveFixture()
	for name, command := range map[string]scheduling.ListCommand{
		"no plan":    {PatientID: "12345", Office: "Spring Hill", VisitType: "medical", StartDate: "2026-06-03", DOB: "01/15/1980"},
		"no patient": {Office: "Spring Hill", VisitType: "medical", InsurancePlanID: "self-pay-medical", StartDate: "2026-06-03", DOB: "01/15/1980"},
	} {
		available, err := f.svc.List(context.Background(), command)
		if err == nil || scheduling.CategoryOf(err) != scheduling.CategoryValidation || len(available.Slots) != 0 {
			t.Errorf("%s: outcome=%q err=%v", name, available.Outcome, err)
		}
	}
	if f.records.SchedulerSetupCalls != 0 {
		t.Errorf("refused searches read the scheduler %d times", f.records.SchedulerSetupCalls)
	}
}

func TestWritesRequireTheirSignedClaims(t *testing.T) {
	now := mutationTestNow()
	sign := func(t *testing.T, patient domain.PatientID, moveOf int) string {
		t.Helper()
		token, err := scheduling.SignSlotToken("test-booking-secret", scheduling.SlotPolicy{
			OfficeID: "spring_hill", Routing: domain.RoutingBachOnly, ColumnID: springHillBach, ProfileID: 620,
			StartDatetime: "2026-06-03T09:00", Duration: 15, DOB: "01/15/1980", AppointmentTypeIDs: []int{1006, 1007},
			PatientID: patient, MoveOf: moveOf, SameStartCapacity: 2, IssuedAt: now.Unix(), ExpiresAt: now.Add(15 * time.Minute).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	f := newMoveFixture()
	visit := bachVisit(54321)
	f.records.AppointmentResults["12345"] = appointmentResult([]domain.PatientAppointment{visit}, true)
	rescheduleToken := f.rescheduleToken(t, visit)
	command := moveCommand(sign(t, "", 0), "")
	command.VisitCategory = "medical"
	if _, err := f.svc.Book(context.Background(), command); scheduling.CategoryOf(err) != scheduling.CategoryInvalidBookingToken {
		t.Errorf("book, no patient claim: err=%v", err)
	}
	for name, token := range map[string]string{
		"no patient claim": sign(t, "", 54321),
		"no move claim":    sign(t, "12345", 0),
	} {
		if _, err := f.svc.Reschedule(context.Background(), moveCommand(token, rescheduleToken)); scheduling.CategoryOf(err) != scheduling.CategoryInvalidBookingToken {
			t.Errorf("move, %s: err=%v", name, err)
		}
	}
	if len(f.records.Bookings) != 0 || len(f.records.Cancellations) != 0 {
		t.Errorf("wrote %d bookings and %d cancellations", len(f.records.Bookings), len(f.records.Cancellations))
	}
}
