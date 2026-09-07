package scheduling_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
)

func TestListLoadsEveryEligibleSlotAcrossCalendarWindow(t *testing.T) {
	// Eastern clock crosses a month, and the 90-day case crosses a DST boundary.
	now := time.Date(2026, 10, 25, 15, 0, 0, 0, time.UTC)
	for _, days := range []int{0, 14, 30, 90} {
		t.Run(fmt.Sprint(days), func(t *testing.T) {
			count := days
			if count == 0 {
				count = 14
			}
			records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "10:00", 15))
			first := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
			for i := 0; i < count; i++ {
				records.ScheduleReads[first.AddDate(0, 0, i).Format("2006-01-02")] = completeRead("1513", nil, nil)
			}
			result, err := scheduling.New(records, "test-secret", func() time.Time { return now }).List(context.Background(), scheduling.ListCommand{Office: "Spring Hill", Routing: "bach_only", RangeDays: days})
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != domain.AvailabilityOutcomeFound || len(result.Slots) != count*4 {
				t.Fatalf("outcome=%s slots=%d want=%d", result.Outcome, len(result.Slots), count*4)
			}
			if result.SearchedFrom != "2026-10-26" || result.SearchedThrough != first.AddDate(0, 0, count-1).Format("2006-01-02") {
				t.Fatalf("wrong coverage: %s..%s", result.SearchedFrom, result.SearchedThrough)
			}
			if len(records.ScheduleReadQueries) != count {
				t.Fatalf("reads=%d want=%d", len(records.ScheduleReadQueries), count)
			}
			for i, slot := range result.Slots {
				if i > 0 && slot.DateTime < result.Slots[i-1].DateTime {
					t.Fatal("inventory not chronological")
				}
				policy, err := scheduling.VerifySlotToken("test-secret", slot.BookingToken, now)
				if err != nil || policy.StartDatetime != slot.DateTime || policy.ColumnID != 1513 {
					t.Fatalf("invalid slot authorization: %v", err)
				}
			}
		})
	}
}

func TestListIncompleteCalendarCannotClaimCompleteInventory(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "10:00", 15))
	// First day has real openings; the remaining days are unknown, not empty.
	records.ScheduleReads["2026-06-02"] = completeRead("1513", nil, nil)
	result, err := scheduling.New(records, "test-secret", func() time.Time { return now }).List(context.Background(), scheduling.ListCommand{Office: "Spring Hill", Routing: "bach_only"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != domain.AvailabilityOutcomeSearchIncomplete || len(result.Slots) != 0 {
		t.Fatalf("outcome=%s slots=%d", result.Outcome, len(result.Slots))
	}
}

func TestListRejectsUnsupportedRangeBeforeProviderRead(t *testing.T) {
	for _, days := range []int{-1, 1, 15, 31, 91} {
		records := recordsWithSetup()
		_, err := scheduling.New(records, "test-secret", time.Now).List(context.Background(), scheduling.ListCommand{Office: "Spring Hill", RangeDays: days})
		if err == nil || records.SchedulerSetupCalls != 0 {
			t.Fatalf("range %d: error=%v setup reads=%d", days, err, records.SchedulerSetupCalls)
		}
	}
}

func TestListExcludesHeldAndFullSlots(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "10:00", 15))
	for i := 1; i <= 14; i++ {
		day := time.Date(2026, 6, 1+i, 0, 0, 0, 0, time.UTC)
		records.ScheduleReads[day.Format("2006-01-02")] = completeRead("1513", []domain.Appointment{
			{StartDateTime: day.Add(9 * time.Hour), Duration: 15},
			{StartDateTime: day.Add(9 * time.Hour), Duration: 15},
		}, []domain.BlockHold{{StartDateTime: day.Add(9*time.Hour + 30*time.Minute), EndDateTime: day.Add(10 * time.Hour)}})
	}
	result, err := scheduling.New(records, "test-secret", func() time.Time { return now }).List(context.Background(), scheduling.ListCommand{Office: "Spring Hill", Routing: "bach_only"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Slots) != 14 {
		t.Fatalf("slots=%d want14", len(result.Slots))
	}
	for _, slot := range result.Slots {
		if slot.Time != "9:15 AM" {
			t.Fatalf("offered blocked/full slot: %s", slot.Time)
		}
	}
}

func TestPreauthInventoryExcludesOccupiedAndHeldWallClockSlots(t *testing.T) {
	for _, date := range []string{"2026-01-05", "2026-06-01", "2026-02-23", "2026-10-19"} {
		t.Run(date, func(t *testing.T) {
			today, err := time.Parse("2006-01-02", date)
			if err != nil {
				t.Fatal(err)
			}
			now := today.Add(16 * time.Hour)
			first := today.AddDate(0, 0, 14)
			records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "10:00", 15))
			for i := 0; i < 14; i++ {
				day := first.AddDate(0, 0, i)
				// AdvancedMD schedule values are clinic wall times represented in UTC.
				records.ScheduleReads[day.Format("2006-01-02")] = completeRead("1513", []domain.Appointment{
					{StartDateTime: day.Add(9 * time.Hour), Duration: 15},
					{StartDateTime: day.Add(9 * time.Hour), Duration: 15},
				}, []domain.BlockHold{{StartDateTime: day.Add(9*time.Hour + 30*time.Minute), EndDateTime: day.Add(10 * time.Hour)}})
			}
			result, err := scheduling.New(records, "test-secret", func() time.Time { return now }).List(context.Background(), scheduling.ListCommand{Office: "Spring Hill", Routing: "bach_only", PreauthRequired: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.SearchedFrom != first.Format("2006-01-02") || result.SearchedThrough != first.AddDate(0, 0, 13).Format("2006-01-02") {
				t.Fatalf("wrong coverage: %s..%s", result.SearchedFrom, result.SearchedThrough)
			}
			if len(result.Slots) != 14 {
				t.Fatalf("slots=%d want14", len(result.Slots))
			}
			for _, slot := range result.Slots {
				if slot.Time != "9:15 AM" {
					t.Fatalf("offered occupied/held slot: %s", slot.DateTime)
				}
			}
		})
	}
}
