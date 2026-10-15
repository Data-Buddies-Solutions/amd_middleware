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
		records.ScheduleReads[date.Format("2006-01-02")] = completeRead("1513", nil, []domain.BlockHold{{
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
		for day := 0; day <= 14; day++ {
			records.ScheduleReads[searchDate.AddDate(0, 0, day).Format("2006-01-02")] = completeRead("1513", nil, nil)
		}

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

func TestListComparesPreferenceDatesAsCalendarDays(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	searchDate := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "16:00", 60))
	records.ScheduleReads["2026-06-03"] = completeRead("1513", nil, []domain.BlockHold{{
		StartDateTime: time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		EndDateTime:   time.Date(2026, 6, 3, 15, 0, 0, 0, time.UTC),
	}})
	records.ScheduleReads["2026-06-04"] = completeRead("1513", nil, []domain.BlockHold{{
		StartDateTime: time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC),
		EndDateTime:   time.Date(2026, 6, 4, 16, 0, 0, 0, time.UTC),
	}})
	for day := 2; day <= 14; day++ {
		date := searchDate.AddDate(0, 0, day)
		records.ScheduleReads[date.Format("2006-01-02")] = completeRead("1513", nil, []domain.BlockHold{{
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
		t.Fatalf("first slot = %q, want real slot on preferred calendar date", result.Slots[0].DateTime)
	}
}

func TestListReturnsIncompleteWhenProviderReadsCannotProveNone(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	searchDate := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "09:15", 15))
	for day := 0; day <= 14; day++ {
		date := searchDate.AddDate(0, 0, day).Format("2006-01-02")
		records.ScheduleReads[date] = domain.ScheduleReadResult{
			Columns: map[string]domain.ColumnSchedule{
				"1513": {
					AppointmentsComplete: false,
					BlockHoldsComplete:   true,
				},
			},
		}
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

func TestListReturnsIncompleteAfterPartialProviderFailure(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	searchDate := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	records := recordsWithSetup(
		testColumn("1513", "620", "1568", "09:00", "09:15", 15),
		testColumn("1598", "620", "1568", "09:00", "09:15", 15),
	)
	for day := 0; day <= 14; day++ {
		date := searchDate.AddDate(0, 0, day)
		records.ScheduleReads[date.Format("2006-01-02")] = domain.ScheduleReadResult{
			Columns: map[string]domain.ColumnSchedule{
				"1513": {
					AppointmentsComplete: false,
					BlockHoldsComplete:   true,
				},
				"1598": {
					BlockHolds: []domain.BlockHold{{
						StartDateTime: date.Add(9 * time.Hour),
						EndDateTime:   date.Add(9*time.Hour + 15*time.Minute),
					}},
					AppointmentsComplete: true,
					BlockHoldsComplete:   true,
				},
			},
		}
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

func TestListTreatsMissingBlockHoldsAsIncomplete(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	searchDate := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "09:15", 15))
	for day := 0; day <= 14; day++ {
		date := searchDate.AddDate(0, 0, day).Format("2006-01-02")
		records.ScheduleReads[date] = domain.ScheduleReadResult{
			Columns: map[string]domain.ColumnSchedule{
				"1513": {
					AppointmentsComplete: true,
					BlockHoldsComplete:   false,
				},
			},
		}
	}

	result, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		List(context.Background(), scheduling.ListCommand{
			StartDate: searchDate.Format("2006-01-02"), Office: "Spring Hill", Routing: string(domain.RoutingBachOnly),
		})
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if result.Outcome != scheduling.AvailabilityOutcomeSearchIncomplete {
		t.Fatalf("result = %#v, want incomplete block-hold read", result)
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

func recordsWithSetup(columns ...domain.SchedulerColumn) *advancedmdtest.Adapter {
	records := advancedmdtest.NewAdapter()
	records.SchedulerSetup = domain.SchedulerSetup{
		Columns: columns,
		Profiles: []domain.SchedulerProfile{
			{ID: "620", Name: "BACH, AUSTIN"},
			{ID: "1993", Name: "CALERO, GISSELLE"},
			{ID: "1983", Name: "OTERO, MELISSA"},
			{ID: "2064", Name: "LICHT, JOSEPH"},
			{ID: "2076", Name: "NOEL"},
		},
		Facilities: []domain.SchedulerFacility{
			{ID: "1568", Name: "ABITA EYE GROUP SPRING HILL"},
			{ID: "670", Name: "ABITA EYE GROUP SWEETWATER"},
		},
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

func completeRead(columnID string, appointments []domain.Appointment, blockHolds []domain.BlockHold) domain.ScheduleReadResult {
	return domain.ScheduleReadResult{
		Columns: map[string]domain.ColumnSchedule{
			columnID: {
				Appointments:         appointments,
				BlockHolds:           blockHolds,
				AppointmentsComplete: true,
				BlockHoldsComplete:   true,
			},
		},
	}
}
