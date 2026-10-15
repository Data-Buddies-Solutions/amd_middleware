package scheduling

import "sort"

func sortAvailabilitySlots(slots []AvailabilitySlotOption) {
	sort.SliceStable(slots, func(i, j int) bool {
		return availabilitySlotLess(slots[i], slots[j])
	})
}

func availabilitySlotLess(left, right AvailabilitySlotOption) bool {
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
