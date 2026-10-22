package scheduling_test

import (
	"context"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/scheduling"
)

func TestListReturnsNoneOnlyAfterACompleteWindow(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	searchDate := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "09:15", 15))
	for day := 0; day <= 14; day++ {
		date := searchDate.AddDate(0, 0, day)
		records.Schedule[date.Format("2006-01-02")] = scheduleRows("1513", nil, []domain.BlockHold{{
			StartDateTime: date.Add(9 * time.Hour),
			EndDateTime:   date.Add(9*time.Hour + 15*time.Minute),
		}})
	}

	result, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		List(context.Background(), scheduling.ListCommand{
			StartDate: searchDate.Format("2006-01-02"),
			Office:    "Spring Hill",
			Routing:   string(domain.RoutingBachOnly),
			DOB:       "01/15/1980",
		})
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if result.Outcome != scheduling.AvailabilityOutcomeNoAvailability ||
		result.Status != scheduling.AvailabilityStatusSuccess ||
		result.AvailabilityFound ||
		result.ShouldRetrySameSearch ||
		result.NextAction != scheduling.AvailabilityNextActionAskDifferentPreferences ||
		result.SearchedThrough != "2026-06-16" ||
		len(result.Slots) != 0 {
		t.Fatalf("result = %#v, want explicit no-availability outcome", result)
	}
}

func TestListReportsIncompleteForUnusableColumnSchedule(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	searchDate := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	noWorkweek := testColumn("1513", "620", "1568", "09:00", "10:00", 15)
	noWorkweek.Workweek = 0
	columns := map[string]domain.SchedulerColumn{
		"zero interval":     testColumn("1513", "620", "1568", "09:00", "10:00", 0),
		"negative interval": testColumn("1513", "620", "1568", "09:00", "10:00", -15),
		"no workweek":       noWorkweek,
	}
	for name, column := range columns {
		records := recordsWithSetup(column)

		type searchResult struct {
			response scheduling.AvailabilityResponse
			err      error
		}
		done := make(chan searchResult, 1)
		go func() {
			response, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
				List(context.Background(), scheduling.ListCommand{
					StartDate: searchDate.Format("2006-01-02"),
					Office:    "Spring Hill",
					Routing:   string(domain.RoutingBachOnly),
					DOB:       "01/15/1980",
				})
			done <- searchResult{response, err}
		}()
		select {
		case result := <-done:
			if result.err != nil {
				t.Fatalf("%s: List error = %v", name, result.err)
			}
			if result.response.Outcome != scheduling.AvailabilityOutcomeSearchIncomplete || len(result.response.Slots) != 0 {
				t.Fatalf("%s: result = %#v, want incomplete search", name, result.response)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: List did not finish", name)
		}
	}
}

func TestListStartsAtStartDate(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	searchDate := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "16:00", 60))
	records.Schedule["2026-06-04"] = scheduleRows("1513", nil, []domain.BlockHold{{
		StartDateTime: time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC),
		EndDateTime:   time.Date(2026, 6, 4, 16, 0, 0, 0, time.UTC),
	}})
	for day := 2; day <= 14; day++ {
		date := searchDate.AddDate(0, 0, day)
		records.Schedule[date.Format("2006-01-02")] = scheduleRows("1513", nil, []domain.BlockHold{{
			StartDateTime: date.Add(9 * time.Hour),
			EndDateTime:   date.Add(16 * time.Hour),
		}})
	}

	result, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		List(context.Background(), scheduling.ListCommand{
			StartDate: "2026-06-04",
			Office:    "Spring Hill",
			Routing:   string(domain.RoutingBachOnly),
		})
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if result.Slots[0].DateTime != "2026-06-04T09:00" {
		t.Fatalf("first slot = %q, want first slot on the start date", result.Slots[0].DateTime)
	}
}

func TestListReturnsIncompleteWhenProviderReadsCannotProveNone(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	searchDate := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "09:15", 15))
	for day := 0; day <= 14; day++ {
		records.IncompleteDays[searchDate.AddDate(0, 0, day).Format("2006-01-02")] = true
	}

	result, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		List(context.Background(), scheduling.ListCommand{
			StartDate: searchDate.Format("2006-01-02"),
			Office:    "Spring Hill",
			Routing:   string(domain.RoutingBachOnly),
			DOB:       "01/15/1980",
		})
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if result.Outcome != scheduling.AvailabilityOutcomeSearchIncomplete ||
		result.Status != scheduling.AvailabilityStatusError ||
		result.AvailabilityFound ||
		!result.ShouldRetrySameSearch ||
		result.NextAction != scheduling.AvailabilityNextActionRetryOnceThenAskPreferences ||
		len(result.Slots) != 0 {
		t.Fatalf("result = %#v, want explicit incomplete outcome", result)
	}
}

func TestListReturnsIncompleteWhenOneDayIsIncompleteAndTheRestAreFull(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	searchDate := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	records := recordsWithSetup(
		testColumn("1513", "620", "1568", "09:00", "09:15", 15),
		testColumn("1598", "620", "1568", "09:00", "09:15", 15),
	)
	for day := 0; day <= 14; day++ {
		date := searchDate.AddDate(0, 0, day)
		hold := []domain.BlockHold{{StartDateTime: date.Add(9 * time.Hour), EndDateTime: date.Add(9*time.Hour + 15*time.Minute)}}
		records.Schedule[date.Format("2006-01-02")] = map[string]advancedmdtest.ColumnRows{
			"1513": {BlockHolds: hold},
			"1598": {BlockHolds: hold},
		}
	}
	records.IncompleteDays[searchDate.Format("2006-01-02")] = true

	result, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		List(context.Background(), scheduling.ListCommand{
			StartDate: searchDate.Format("2006-01-02"),
			Office:    "Spring Hill",
			Routing:   string(domain.RoutingBachOnly),
			DOB:       "01/15/1980",
		})
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if result.Outcome != scheduling.AvailabilityOutcomeSearchIncomplete || !result.ShouldRetrySameSearch {
		t.Fatalf("result = %#v, want partial-read incomplete outcome", result)
	}
}

func TestListExcludesRestrictedProviders(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := recordsWithSetup(testColumn("1210", "1993", "670", "09:00", "09:15", 15))

	result, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		List(context.Background(), scheduling.ListCommand{
			StartDate: "2026-06-03",
			Office:    "Sweetwater",
			Routing:   string(domain.RoutingOpticalOnly),
			DOB:       "06/01/2023",
		})
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if result.Outcome != scheduling.AvailabilityOutcomeNoEligibleProviders || result.AvailabilityFound {
		t.Fatalf("result = %#v, want age-restricted provider excluded", result)
	}
}

func TestListRoutesAPatientByTheirAgeOnTheEasternCalendar(t *testing.T) {
	for _, tt := range []struct {
		name    string
		now     time.Time
		outcome string
	}{
		{"17 at 22:00 EDT on the eve of the 18th birthday", time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC), scheduling.AvailabilityOutcomeNoEligibleProviders},
		{"18 at 00:30 EDT on the 18th birthday", time.Date(2026, 10, 21, 4, 30, 0, 0, time.UTC), scheduling.AvailabilityOutcomeFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			records := recordsWithSetup(testColumn("1550", "2076", "1568", "09:00", "09:15", 15))

			result, err := scheduling.New(records, "test-booking-secret", func() time.Time { return tt.now }).
				List(context.Background(), scheduling.ListCommand{
					StartDate: "2026-10-22",
					Office:    "Spring Hill",
					Routing:   string(domain.RoutingAll),
					VisitType: domain.AppointmentVisitMedical,
					DOB:       "10/21/2008",
				})
			if err != nil {
				t.Fatalf("List error = %v", err)
			}
			if result.Outcome != tt.outcome {
				t.Fatalf("outcome = %s, want %s: a minor routes to Dr. Bach only, an adult may see Dr. Noel", result.Outcome, tt.outcome)
			}
		})
	}
}

func TestListRejectsInvalidDOB(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := recordsWithSetup(
		testColumn("1513", "620", "1568", "09:00", "09:15", 15),
		testColumn("1551", "2064", "1568", "09:00", "09:15", 15),
	)
	scheduler := scheduling.New(records, "test-booking-secret", func() time.Time { return now })

	_, err := scheduler.List(context.Background(), scheduling.ListCommand{
		StartDate: "2026-06-03", Office: "Spring Hill", Routing: string(domain.RoutingBachOnly), DOB: "not-a-date",
	})
	if err == nil || err.Error() != "dob must be a valid date" {
		t.Fatalf("invalid DOB error = %v", err)
	}

}

func TestListPreservesAuthenticationFailureContract(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	want := "Service authentication is temporarily unavailable. Please try again."

	t.Run("scheduler setup", func(t *testing.T) {
		records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "09:15", 15))
		records.SchedulerSetupError = advancedmd.NewError(safeerrors.CategoryUnavailable)
		_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
			List(context.Background(), scheduling.ListCommand{
				StartDate: "2026-06-03", Office: "Spring Hill", Routing: string(domain.RoutingBachOnly),
			})
		if err == nil || err.Error() != want {
			t.Fatalf("List error = %v, want %q", err, want)
		}
		if got := scheduling.ProviderFailureOf(err); got != safeerrors.CategoryUnavailable {
			t.Fatalf("ProviderFailureOf(error) = %q, want unavailable", got)
		}
	})

	t.Run("schedule read", func(t *testing.T) {
		records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "09:15", 15))
		records.ScheduleReadErrors["2026-06-03"] = advancedmd.NewError(safeerrors.CategoryAuthentication)
		_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
			List(context.Background(), scheduling.ListCommand{
				StartDate: "2026-06-03", Office: "Spring Hill", Routing: string(domain.RoutingBachOnly),
			})
		if err == nil || err.Error() != want {
			t.Fatalf("List error = %v, want %q", err, want)
		}
		if got := scheduling.ProviderFailureOf(err); got != safeerrors.CategoryAuthentication {
			t.Fatalf("ProviderFailureOf(error) = %q, want authentication", got)
		}
	})
}

func TestListReportsAdapterInternalFailureAsInternal(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "09:15", 15))
	records.SchedulerSetupError = advancedmd.NewError(safeerrors.CategoryInternal)
	_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		List(context.Background(), scheduling.ListCommand{
			StartDate: "2026-06-03", Office: "Spring Hill", Routing: string(domain.RoutingBachOnly),
		})
	if got := scheduling.ProviderFailureOf(err); got != safeerrors.CategoryInternal {
		t.Fatalf("ProviderFailureOf(error) = %q, want internal", got)
	}
}

func recordsWithSetup(columns ...domain.SchedulerColumn) *advancedmdtest.Adapter {
	records := advancedmdtest.NewAdapter()
	records.SchedulerSetup = domain.SchedulerSetup{
		Columns: columns,
	}
	return records
}

func testColumn(id, profileID, facilityID, start, end string, interval int) domain.SchedulerColumn {
	return domain.SchedulerColumn{
		ID:         id,
		Name:       "TEST COLUMN",
		ProfileID:  profileID,
		FacilityID: facilityID,
		StartTime:  start,
		EndTime:    end,
		Interval:   interval,
		Workweek:   127,
	}
}

func scheduleRows(columnID string, appointments []domain.Appointment, blockHolds []domain.BlockHold) map[string]advancedmdtest.ColumnRows {
	return map[string]advancedmdtest.ColumnRows{columnID: {Appointments: appointments, BlockHolds: blockHolds}}
}
