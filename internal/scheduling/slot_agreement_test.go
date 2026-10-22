package scheduling_test

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/scheduling"
)

const (
	agreementSeed  = 20261022
	agreementCases = 3000
	amdRowLayout   = "2006-01-02T15:04:05"
)

type agreementColumn struct {
	office     string
	routing    domain.RoutingRule
	id         string
	profileID  string
	facilityID string
	visit      string
	patient    string
}

var agreementColumns = []agreementColumn{
	{"Spring Hill", domain.RoutingBachOnly, "1513", "620", "1568", domain.AppointmentVisitMedical, domain.AppointmentPatientEstablished},
	{"Crystal River", domain.RoutingAll, "1593", "2064", "1576", domain.AppointmentVisitMedical, domain.AppointmentPatientEstablished},
	{"Sweetwater", domain.RoutingOpticalOnly, "1296", "1996", "670", domain.AppointmentVisitRoutineVision, domain.AppointmentPatientNew},
}

type amdRows struct {
	appointments []clients.AMDAppointmentResponse
	holds        []advancedmdtest.HoldRow
}

func (rows *amdRows) addAppointment(id int, columnID string, start time.Time, duration int) {
	column, _ := strconv.Atoi(columnID)
	rows.appointments = append(rows.appointments, clients.AMDAppointmentResponse{ID: id, StartDateTime: start.Format(amdRowLayout), Duration: duration, ColumnID: column})
}

func (rows *amdRows) addHold(id int, columnID string, start, end time.Time) {
	hold := advancedmdtest.HoldRow{ID: id, StartDateTime: start.Format(amdRowLayout), EndDateTime: end.Format(amdRowLayout)}
	hold.Column.ID, _ = strconv.Atoi(columnID)
	rows.holds = append(rows.holds, hold)
}

func (rows *amdRows) addRecurringHold(id int, columnID string, start time.Time, duration int) {
	hold := advancedmdtest.HoldRow{ID: id, StartDateTime: start.Format(amdRowLayout), EndDateTime: start.AddDate(0, 6, 0).Format(amdRowLayout), Duration: duration}
	hold.Column.ID, _ = strconv.Atoi(columnID)
	hold.Recurrence.RecurrenceType = 2
	rows.holds = append(rows.holds, hold)
}

func appointmentSpan(row clients.AMDAppointmentResponse) domain.Interval {
	start, _ := time.Parse(amdRowLayout, row.StartDateTime)
	return domain.Interval{Start: start, End: start.Add(time.Duration(row.Duration) * time.Minute)}
}

type agreementRecords struct {
	*advancedmdtest.Adapter
	schedule *advancedmd.Adapter
	days     map[string]domain.ScheduleReadResult
}

func (r agreementRecords) ReadSchedule(ctx context.Context, query domain.ScheduleReadQuery) (domain.ScheduleReadResult, error) {
	key := query.Date + " " + strings.Join(query.ColumnIDs, "-")
	if read, ok := r.days[key]; ok {
		return read, nil
	}
	read, err := r.schedule.ReadSchedule(ctx, query)
	if err == nil {
		r.days[key] = read
	}
	return read, err
}

func (r agreementRecords) ReadScheduleRange(ctx context.Context, query domain.ScheduleRangeQuery) (map[string]domain.ScheduleReadResult, error) {
	return r.schedule.ReadScheduleRange(ctx, query)
}

type agreementCase struct {
	target    agreementColumn
	column    domain.SchedulerColumn
	now       time.Time
	first     time.Time
	rows      amdRows
	responses map[string][]advancedmdtest.Response
}

func (c agreementCase) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s column %s %s-%s every %d min workweek %07b, now %s, search from %s\n",
		c.target.office, c.column.ID, c.column.StartTime, c.column.EndTime, c.column.Interval, c.column.Workweek,
		c.now.Format(time.RFC3339), c.first.Format("2006-01-02"))
	for _, row := range c.rows.appointments {
		fmt.Fprintf(&b, "  appointment %d at %s for %d min\n", row.ID, row.StartDateTime, row.Duration)
	}
	for _, row := range c.rows.holds {
		fmt.Fprintf(&b, "  hold %d %s..%s duration %d recurrence %d\n", row.ID, row.StartDateTime, row.EndDateTime, row.Duration, row.Recurrence.RecurrenceType)
	}
	return b.String()
}

func agreementCaseAt(index int) agreementCase {
	rng := rand.New(rand.NewSource(agreementSeed + int64(index)))
	target := agreementColumns[rng.Intn(len(agreementColumns))]
	today := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, rng.Intn(365))
	interval := []int{10, 15, 20, 30, 45, 60}[rng.Intn(6)]
	workStart := 6*60 + 5*rng.Intn(48)
	workEnd := workStart + 5*rng.Intn(37)
	c := agreementCase{
		target: target,
		now:    today.Add(16 * time.Hour),
		first:  today.AddDate(0, 0, 1+rng.Intn(3)),
		column: domain.SchedulerColumn{
			ID: target.id, Name: "AGREEMENT", ProfileID: target.profileID, FacilityID: target.facilityID,
			StartTime: clockTime(workStart), EndTime: clockTime(workEnd), Interval: interval,
			Workweek: 1<<rng.Intn(7) | 1<<rng.Intn(7),
		},
	}
	nextID := 1
	id := func() int {
		nextID++
		return nextID
	}
	recurringID := id()
	for offset := -1; offset < 15; offset++ {
		day := c.first.AddDate(0, 0, offset)
		if !c.column.WorksOnDay(day.Weekday()) && rng.Intn(4) != 0 {
			continue
		}
		at := func(minute int) time.Time { return day.Add(time.Duration(minute) * time.Minute) }
		anyMinute := func() int { return workStart - 60 + 5*rng.Intn((workEnd-workStart+90)/5+1) }
		gridMinute := func() int { return workStart + interval*rng.Intn((workEnd-workStart)/interval+1) }
		for range rng.Intn(6) {
			minute := anyMinute()
			if rng.Intn(2) == 0 {
				minute = gridMinute()
			}
			duration := []int{0, 5, 10, 15, 20, 30, 45, 60, 90}[rng.Intn(9)]
			appointmentID := id()
			if rng.Intn(5) == 0 {
				appointmentID = 0
			}
			c.rows.addAppointment(appointmentID, target.id, at(minute), duration)
			if rng.Intn(6) == 0 {
				c.rows.addAppointment(appointmentID, target.id, at(minute), duration)
			}
			for rng.Intn(4) == 0 {
				c.rows.addAppointment(id(), target.id, at(minute), []int{0, 15, 30, 60}[rng.Intn(4)])
			}
		}
		if rng.Intn(6) == 0 {
			c.rows.addAppointment(id(), target.id, at(23*60+5*rng.Intn(12)), 30+15*rng.Intn(6))
		}
		for range rng.Intn(3) {
			switch rng.Intn(5) {
			case 0:
				start := at(anyMinute())
				c.rows.addHold(id(), target.id, start, start.Add(time.Duration(5*rng.Intn(19))*time.Minute))
			case 1:
				start := at(anyMinute())
				c.rows.addHold(id(), target.id, start, start)
			case 2:
				c.rows.addHold(id(), target.id, at(-7*60+5*rng.Intn(84)), at(5*rng.Intn(workStart/5+25)))
			case 3:
				c.rows.addRecurringHold(recurringID, target.id, at(anyMinute()), 15*(1+rng.Intn(4)))
			case 4:
				c.rows.addHold(id(), target.id, at(0), at(23*60+59))
			}
		}
	}
	return c
}

func clockTime(minute int) string {
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

func (c agreementCase) scheduler() (scheduling.Scheduling, *advancedmdtest.Adapter) {
	fake := recordsWithSetup(c.column)
	fake.Demographics["12345"] = domain.PatientDemographics{CarrierName: "Self Pay", DOB: "01/15/1980"}
	provider := &advancedmdtest.Provider{Appointments: c.rows.appointments, Holds: c.rows.holds, Responses: c.responses}
	reads := provider.Adapter(func() time.Time { return c.now })
	return scheduling.New(agreementRecords{Adapter: fake, schedule: reads, days: map[string]domain.ScheduleReadResult{}}, "test-booking-secret", func() time.Time { return c.now }), fake
}

func (c agreementCase) list(scheduler scheduling.Scheduling) (scheduling.AvailabilityResponse, error) {
	result, err := scheduler.List(context.Background(), scheduling.ListCommand{
		StartDate: c.first.Format("2006-01-02"), Office: c.target.office, Routing: string(c.target.routing), DOB: "01/15/1980",
	})
	if err != nil {
		return result, fmt.Errorf("List error = %w", err)
	}
	if result.Outcome != scheduling.AvailabilityOutcomeFound && result.Outcome != scheduling.AvailabilityOutcomeNoAvailability {
		return result, fmt.Errorf("List outcome = %s, want a complete search", result.Outcome)
	}
	return result, nil
}

func (c agreementCase) check() ([]scheduling.AvailabilitySlotOption, error) {
	scheduler, fake := c.scheduler()
	result, err := c.list(scheduler)
	if err != nil {
		return nil, err
	}
	var listed []string
	for _, slot := range result.Slots {
		listed = append(listed, fmt.Sprintf("%s %d/%d force=%v", slot.DateTime, slot.SameStartBooked, slot.SameStartCapacity, slot.RequiresForce))
	}
	if want := c.expectedSlots(); !slices.Equal(listed, want) {
		return nil, fmt.Errorf("listed\n  %v\nwant the slots the schedule leaves open\n  %v", listed, want)
	}
	for _, slot := range result.Slots {
		before := len(fake.Bookings)
		receipt, err := c.book(scheduler, slot.BookingToken)
		if err != nil || receipt.Status != "booked" || len(fake.Bookings) != before+1 {
			return nil, fmt.Errorf("listed %s (%d/%d force=%v) but booking returned %q: %v",
				slot.DateTime, slot.SameStartBooked, slot.SameStartCapacity, slot.RequiresForce, scheduling.CategoryOf(err), err)
		}
		if booking := fake.Bookings[before]; domain.FormatSlotDateTime(booking.Start) != slot.DateTime || booking.Duration != slot.Duration || booking.Force != slot.RequiresForce {
			return nil, fmt.Errorf("listed %s but booked %s for %d min force=%v", slot.DateTime, domain.FormatSlotDateTime(booking.Start), booking.Duration, booking.Force)
		}
	}
	return result.Slots, nil
}

func (c agreementCase) book(scheduler scheduling.Scheduling, token string) (scheduling.BookReceipt, error) {
	return scheduler.Book(context.Background(), scheduling.BookCommand{
		PatientID: "12345", DOB: "01/15/1980", Office: c.target.office, BookingToken: token,
		VisitCategory: c.target.visit, PatientStatus: c.target.patient,
		AppointmentReason: "Eye follow-up", ReferringDoctor: "none",
	})
}

func (c agreementCase) expectedSlots() []string {
	office, _ := domain.ResolveOffice(c.target.office)
	length := time.Duration(c.column.Interval) * time.Minute
	open, closed := clockMinutes(c.column.StartTime), clockMinutes(c.column.EndTime)
	var want []string
	for offset := range 14 {
		day := c.first.AddDate(0, 0, offset)
		if c.column.Workweek&(1<<day.Weekday()) == 0 {
			continue
		}
		for start := day.Add(open); !start.Add(length).After(day.Add(closed)); start = start.Add(length) {
			if booked, open := c.sameStartBookings(domain.Interval{Start: start, End: start.Add(length)}); open {
				capacity := max(1, office.Columns[c.column.ID].SameStartCapacityAt(start))
				if booked < capacity {
					shownCapacity := capacity
					if booked == 0 {
						shownCapacity = 0
					}
					want = append(want, fmt.Sprintf("%s %d/%d force=%v", domain.FormatSlotDateTime(start), booked, shownCapacity, booked > 0))
				}
			}
		}
	}
	return want
}

func (c agreementCase) sameStartBookings(slot domain.Interval) (int, bool) {
	for _, hold := range c.rows.holds {
		if span := hold.Span(); span.Start.Equal(span.End) {
			if slot.Start.Before(span.Start) && span.Start.Before(slot.End) {
				return 0, false
			}
		} else if overlaps(slot, span) {
			return 0, false
		}
	}
	booked := 0
	counted := map[int]bool{}
	for _, appointment := range c.rows.appointments {
		switch other := appointmentSpan(appointment); {
		case other.Start.Equal(slot.Start):
			if appointment.ID <= 0 || !counted[appointment.ID] {
				counted[appointment.ID] = true
				booked++
			}
		case overlaps(slot, other):
			return 0, false
		}
	}
	return booked, true
}

func overlaps(a, b domain.Interval) bool {
	return a.Start.Before(b.End) && b.Start.Before(a.End)
}

func (c agreementCase) idlessSameStarts() int {
	starts := map[string]int{}
	for _, row := range c.rows.appointments {
		if row.ID <= 0 {
			starts[row.StartDateTime]++
		}
	}
	shared := 0
	for _, count := range starts {
		if count > 1 {
			shared++
		}
	}
	return shared
}

func clockMinutes(clock string) time.Duration {
	parsed, _ := time.Parse("15:04", clock)
	return time.Duration(parsed.Hour()*60+parsed.Minute()) * time.Minute
}

func TestEveryListedSlotBooksAgainstTheSameSchedule(t *testing.T) {
	slotsByCase := make([][]scheduling.AvailabilitySlotOption, agreementCases)
	failures := make([]error, agreementCases)
	var next atomic.Int64
	var workers sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		workers.Go(func() {
			for i := int(next.Add(1) - 1); i < agreementCases; i = int(next.Add(1) - 1) {
				slotsByCase[i], failures[i] = agreementCaseAt(i).check()
			}
		})
	}
	workers.Wait()
	listed, forced, crossingMonths, idlessSameStarts := 0, 0, 0, 0
	for i, failure := range failures {
		if failure != nil {
			t.Fatalf("case %d: %v\n%s", i, failure, agreementCaseAt(i))
		}
		listed += len(slotsByCase[i])
		for _, slot := range slotsByCase[i] {
			if slot.RequiresForce {
				forced++
			}
		}
		c := agreementCaseAt(i)
		if c.first.Month() != c.first.AddDate(0, 0, 13).Month() {
			crossingMonths++
		}
		idlessSameStarts += c.idlessSameStarts()
	}
	if listed < agreementCases || forced == 0 || crossingMonths == 0 || idlessSameStarts == 0 {
		t.Fatalf("listed=%d forced=%d month-crossing searches=%d id-less same starts=%d across %d schedules; the generator is too narrow to prove agreement", listed, forced, crossingMonths, idlessSameStarts, agreementCases)
	}
	t.Logf("%d schedules (%d crossing a month), %d starts shared by id-less appointments, %d listed slots (%d same-start with force), every one booked", agreementCases, crossingMonths, idlessSameStarts, listed, forced)
}

func TestListedSlotBooksWhenAdvancedMDRepeatsAnAppointmentRow(t *testing.T) {
	target := agreementColumns[0]
	day := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	c := agreementCase{
		target: target,
		now:    time.Date(2026, 10, 13, 16, 0, 0, 0, time.UTC),
		first:  day,
		column: domain.SchedulerColumn{
			ID: target.id, Name: "AGREEMENT", ProfileID: target.profileID, FacilityID: target.facilityID,
			StartTime: "09:00", EndTime: "09:30", Interval: 30, Workweek: 1 << day.Weekday(),
		},
	}
	c.rows.addAppointment(18, target.id, day.Add(9*time.Hour), 30)
	c.rows.addAppointment(18, target.id, day.Add(9*time.Hour), 30)

	slots, err := c.check()
	if err != nil {
		t.Fatalf("%v\n%s", err, c)
	}
	if len(slots) != 2 || slots[0].DateTime != "2026-10-15T09:00" || slots[0].SameStartBooked != 1 || !slots[0].RequiresForce {
		t.Fatalf("slots = %+v, want 09:00 on both Thursdays with the 2026-10-15 slot double-booked once\n%s", slots, c)
	}
}

func TestSlotsOverlappingTheScheduleAreNeverListed(t *testing.T) {
	target := agreementColumns[0]
	day := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	at := func(hour, minute int) time.Time {
		return day.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
	}
	c := agreementCase{
		target: target,
		now:    time.Date(2026, 6, 29, 16, 0, 0, 0, time.UTC),
		first:  day,
		column: domain.SchedulerColumn{
			ID: target.id, Name: "AGREEMENT", ProfileID: target.profileID, FacilityID: target.facilityID,
			StartTime: "08:00", EndTime: "11:45", Interval: 30, Workweek: 1 << day.Weekday(),
		},
	}
	c.rows.addAppointment(1, target.id, at(8, 45), 15)
	c.rows.addAppointment(2, target.id, at(9, 30), 30)
	c.rows.addAppointment(3, target.id, at(9, 30), 30)
	c.rows.addHold(4, target.id, at(10, 20), at(10, 40))
	c.rows.addHold(5, target.id, at(-10, 0), at(8, 0))

	scheduler, _ := c.scheduler()
	result, err := c.list(scheduler)
	if err != nil {
		t.Fatalf("%v\n%s", err, c)
	}
	var listed []string
	for _, slot := range result.Slots {
		if strings.HasPrefix(slot.DateTime, "2026-06-30") {
			listed = append(listed, slot.DateTime[11:])
		}
	}
	want := []string{"08:00", "09:00", "11:00"}
	if !slices.Equal(listed, want) {
		t.Fatalf("listed %v, want %v: 08:30 overlaps an appointment, 09:30 is full, 10:00 and 10:30 overlap a hold, 11:30 runs past work hours\n%s", listed, want, c)
	}
}

func TestBookingRejectsASlotHoldingTwoAppointmentsWithoutIDs(t *testing.T) {
	target := agreementColumns[0]
	day, _ := time.Parse("2006-01-02", slotContractDate)
	now := mutationTestNow()
	c := agreementCase{
		target: target,
		now:    now,
		first:  day,
		column: domain.SchedulerColumn{
			ID: target.id, Name: "AGREEMENT", ProfileID: target.profileID, FacilityID: target.facilityID,
			StartTime: "09:00", EndTime: "09:30", Interval: 30, Workweek: 1 << day.Weekday(),
		},
	}
	c.rows.addAppointment(0, target.id, day.Add(9*time.Hour), 30)
	c.rows.addAppointment(0, target.id, day.Add(9*time.Hour), 30)

	scheduler, fake := c.scheduler()
	result, err := c.list(scheduler)
	if err != nil {
		t.Fatalf("%v\n%s", err, c)
	}
	for _, slot := range result.Slots {
		if slot.DateTime == slotContractDate+"T09:00" {
			t.Errorf("listed %s %d/%d force=%v, want it full with two id-less appointments on a capacity-2 column\n%s", slot.DateTime, slot.SameStartBooked, slot.SameStartCapacity, slot.RequiresForce, c)
		}
	}
	office, err := domain.ResolveOffice(target.office)
	if err != nil {
		t.Fatal(err)
	}
	token := signSlotContractToken(t, now, office.ID, target.routing, c.column, slotContractToken{start: "09:00", duration: 30, booked: 1, capacity: 2, force: true})
	_, err = c.book(scheduler, token)
	if scheduling.CategoryOf(err) != scheduling.CategorySlotUnavailable || len(fake.Bookings) != 0 {
		var booked []string
		for _, booking := range fake.Bookings {
			booked = append(booked, fmt.Sprintf("%s force=%v", domain.FormatSlotDateTime(booking.Start), booking.Force))
		}
		t.Fatalf("booking a third patient into 09:00 returned %q (%v) and booked %v, want slot_unavailable and no booking\n%s", scheduling.CategoryOf(err), err, booked, c)
	}
}

func TestBookingRevalidationFailsWithTheFirstFailedDayRead(t *testing.T) {
	target := agreementColumns[0]
	day, _ := time.Parse("2006-01-02", slotContractDate)
	now := mutationTestNow()
	appointments := "appointments day " + slotContractDate + " " + target.id
	holds := "blockholds day " + slotContractDate + " " + target.id
	unavailable := advancedmdtest.Response{Status: http.StatusServiceUnavailable, Body: `{}`}
	malformed := advancedmdtest.Response{Status: http.StatusOK, Body: `not json`}
	tests := []struct {
		name      string
		responses map[string][]advancedmdtest.Response
		provider  safeerrors.Category
		logged    []string
	}{
		{
			name:      "appointments read fails",
			responses: map[string][]advancedmdtest.Response{appointments: {unavailable}},
			provider:  safeerrors.CategoryUpstreamStatus,
			logged:    []string{"schedule read incomplete category=upstream_status"},
		},
		{
			name:      "block holds read fails",
			responses: map[string][]advancedmdtest.Response{holds: {malformed}},
			provider:  safeerrors.CategoryInvalidResponse,
			logged:    []string{"schedule read incomplete category=invalid_response"},
		},
		{
			name:      "both reads fail and the appointments read is malformed",
			responses: map[string][]advancedmdtest.Response{appointments: {malformed}, holds: {unavailable}},
			provider:  safeerrors.CategoryInvalidResponse,
			logged:    []string{"schedule read incomplete category=invalid_response", "schedule read incomplete category=upstream_status"},
		},
		{
			name:      "both reads fail and the appointments read is unavailable",
			responses: map[string][]advancedmdtest.Response{appointments: {unavailable}, holds: {malformed}},
			provider:  safeerrors.CategoryUpstreamStatus,
			logged:    []string{"schedule read incomplete category=upstream_status", "schedule read incomplete category=invalid_response"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := agreementCase{
				target: target,
				now:    now,
				first:  day,
				column: domain.SchedulerColumn{
					ID: target.id, Name: "AGREEMENT", ProfileID: target.profileID, FacilityID: target.facilityID,
					StartTime: "09:00", EndTime: "09:30", Interval: 30, Workweek: 1 << day.Weekday(),
				},
				responses: tt.responses,
			}
			office, err := domain.ResolveOffice(target.office)
			if err != nil {
				t.Fatal(err)
			}
			token := signSlotContractToken(t, now, office.ID, target.routing, c.column, slotContractToken{start: "09:00", duration: 30, capacity: 2})
			scheduler, fake := c.scheduler()
			var logs bytes.Buffer
			log.SetOutput(&logs)
			defer log.SetOutput(os.Stderr)

			_, err = c.book(scheduler, token)

			if scheduling.CategoryOf(err) != scheduling.CategoryWriteFailed ||
				scheduling.ProviderFailureOf(err) != tt.provider ||
				err.Error() != "Unable to revalidate the selected slot. Please check availability again." {
				t.Fatalf("Book error = %v, category = %q, provider failure = %q; want write_failed from %q", err, scheduling.CategoryOf(err), scheduling.ProviderFailureOf(err), tt.provider)
			}
			if len(fake.Bookings) != 0 {
				t.Fatalf("provider bookings = %d, want none after a failed day read", len(fake.Bookings))
			}
			var logged []string
			for line := range strings.Lines(logs.String()) {
				if _, entry, ok := strings.Cut(strings.TrimSpace(line), "schedule read incomplete"); ok {
					logged = append(logged, "schedule read incomplete"+entry)
				}
			}
			if !slices.Equal(logged, tt.logged) {
				t.Fatalf("logged %q, want %q", logged, tt.logged)
			}
		})
	}
}
