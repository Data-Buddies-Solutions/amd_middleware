package scheduling

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"advancedmd-token-management/internal/domain"
)

type rankedAvailabilitySlot struct {
	slot            domain.AvailabilitySlotOption
	mismatchCount   int
	distanceMinutes int
}

func validatePreferredTime(preferredTime *AvailabilityTimePreference) error {
	if preferredTime == nil {
		return nil
	}
	switch preferredTime.Kind {
	case AvailabilityTimeMorning, AvailabilityTimeAfternoon:
		if preferredTime.MinuteOfDay != nil {
			return errors.New("preferredTime.minuteOfDay is not allowed")
		}
	case "":
		if preferredTime.MinuteOfDay == nil ||
			*preferredTime.MinuteOfDay < 0 ||
			*preferredTime.MinuteOfDay >= 24*60 {
			return errors.New("preferredTime.minuteOfDay must be between 0 and 1439")
		}
	default:
		return errors.New("preferredTime.kind is invalid")
	}
	return nil
}

func hasTwoExactAvailabilityMatches(
	candidates []rankedAvailabilitySlot,
) bool {
	firstSlotKey := ""
	for _, candidate := range candidates {
		if candidate.mismatchCount != 0 || candidate.distanceMinutes != 0 {
			continue
		}
		slotKey := availabilitySlotKey(candidate.slot)
		if firstSlotKey != "" && slotKey != firstSlotKey {
			return true
		}
		firstSlotKey = slotKey
	}
	return false
}

func rankedSlot(
	slot domain.AvailabilitySlotOption,
	requestedDate string,
	preferredTime *AvailabilityTimePreference,
) (rankedAvailabilitySlot, error) {
	start, err := time.Parse("2006-01-02T15:04", slot.DateTime)
	if err != nil {
		return rankedAvailabilitySlot{}, fmt.Errorf("invalid slot datetime %q", slot.DateTime)
	}
	candidate := rankedAvailabilitySlot{slot: slot}
	if requestedDate != "" {
		date, err := time.Parse("2006-01-02", requestedDate)
		if err != nil {
			return rankedAvailabilitySlot{}, fmt.Errorf("invalid requested date %q", requestedDate)
		}
		slotDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		days := absoluteInt(int(slotDate.Sub(date).Hours() / 24))
		if days > 0 {
			candidate.mismatchCount++
			candidate.distanceMinutes += days * 24 * 60
		}
	}
	if preferredTime != nil {
		matches, distance := evaluateTimePreference(start, *preferredTime)
		if !matches {
			candidate.mismatchCount++
		}
		candidate.distanceMinutes += distance
	}
	return candidate, nil
}

func evaluateTimePreference(start time.Time, preference AvailabilityTimePreference) (bool, int) {
	minuteOfDay := start.Hour()*60 + start.Minute()
	switch preference.Kind {
	case AvailabilityTimeMorning:
		if minuteOfDay < 12*60 {
			return true, 0
		}
		return false, minuteOfDay - 12*60 + 1
	case AvailabilityTimeAfternoon:
		if minuteOfDay >= 12*60 {
			return true, 0
		}
		return false, 12*60 - minuteOfDay
	case "":
		distance := absoluteInt(minuteOfDay - *preference.MinuteOfDay)
		return distance == 0, distance
	}
	return false, 24 * 60
}

func selectPreferredAvailabilitySlots(candidates []rankedAvailabilitySlot) []domain.AvailabilitySlotOption {
	sort.SliceStable(candidates, func(i, j int) bool {
		return rankedAvailabilitySlotLess(candidates[i], candidates[j])
	})
	if len(candidates) == 0 {
		return nil
	}
	slots := make([]domain.AvailabilitySlotOption, 0, min(2, len(candidates)))
	selectedSlotKeys := make(map[string]bool, 2)
	for _, candidate := range candidates {
		key := availabilitySlotKey(candidate.slot)
		if selectedSlotKeys[key] {
			continue
		}
		slots = append(slots, candidate.slot)
		selectedSlotKeys[key] = true
		if len(slots) == 2 {
			break
		}
	}
	return slots
}

func availabilitySlotKey(slot domain.AvailabilitySlotOption) string {
	return slot.Provider + "|" + slot.DateTime
}

func selectBroadAvailabilitySlots(slots []domain.AvailabilitySlotOption) []domain.AvailabilitySlotOption {
	if len(slots) <= 2 {
		return slots
	}
	firstIsMorning := slotMinuteOfDay(slots[0]) < 12*60
	for _, slot := range slots[1:] {
		if slotMinuteOfDay(slot) < 12*60 != firstIsMorning {
			return []domain.AvailabilitySlotOption{slots[0], slot}
		}
	}
	return slots[:2]
}

func rankedAvailabilitySlotLess(left, right rankedAvailabilitySlot) bool {
	if left.mismatchCount != right.mismatchCount {
		return left.mismatchCount < right.mismatchCount
	}
	if left.distanceMinutes != right.distanceMinutes {
		return left.distanceMinutes < right.distanceMinutes
	}
	return availabilitySlotLess(left.slot, right.slot)
}

func sortAvailabilitySlots(slots []domain.AvailabilitySlotOption) {
	sort.SliceStable(slots, func(i, j int) bool {
		return availabilitySlotLess(slots[i], slots[j])
	})
}

func availabilitySlotLess(left, right domain.AvailabilitySlotOption) bool {
	if left.DateTime != right.DateTime {
		return left.DateTime < right.DateTime
	}
	if left.Provider != right.Provider {
		return left.Provider < right.Provider
	}
	if left.ColumnID != right.ColumnID {
		return left.ColumnID < right.ColumnID
	}
	return left.ProfileID < right.ProfileID
}

func slotMinuteOfDay(slot domain.AvailabilitySlotOption) int {
	start, _ := time.Parse("2006-01-02T15:04", slot.DateTime)
	return start.Hour()*60 + start.Minute()
}

func absoluteInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
