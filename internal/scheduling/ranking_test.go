package scheduling

import (
	"advancedmd-token-management/internal/domain"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

func TestEarlyStopPreservesExhaustiveRanking(t *testing.T) {
	for seed := int64(0); seed < 100; seed++ {
		rng := rand.New(rand.NewSource(seed))
		requested := "2026-06-02"
		if seed%3 == 0 {
			requested = ""
		}
		preference := &AvailabilityTimePreference{Kind: AvailabilityTimeAfternoon}
		if seed%3 == 1 {
			minute := 15 * 60
			preference = &AvailabilityTimePreference{MinuteOfDay: &minute}
		}
		if seed%3 == 2 {
			preference = nil
		}
		command := SearchCommand{RequestedDate: requested, PreferredTime: preference}
		var exhaustive, early []rankedAvailabilitySlot
		stopped := false
		for day := 0; day < 15; day++ {
			date := time.Date(2026, 6, 2+day, 0, 0, 0, 0, time.UTC)
			for _, hour := range []int{9, 11, 14, 15, 16} {
				if rng.Intn(3) == 0 {
					continue
				}
				slot := domain.AvailabilitySlotOption{DateTime: date.Add(time.Duration(hour) * time.Hour).Format("2006-01-02T15:04"), Provider: "provider"}
				ranked, err := rankedSlot(slot, requested, preference)
				if err != nil {
					t.Fatal(err)
				}
				exhaustive = append(exhaustive, ranked)
				if !stopped {
					early = append(early, ranked)
				}
			}
			selectPreferredAvailabilitySlots(early)
			if !stopped {
				stopped = laterDatesCannotImprove(early, command, date.Format("2006-01-02"))
			}
		}
		if !reflect.DeepEqual(selectPreferredAvailabilitySlots(early), selectPreferredAvailabilitySlots(exhaustive)) {
			t.Fatalf("seed %d changed selected slots", seed)
		}
	}
}
