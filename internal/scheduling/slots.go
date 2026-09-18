package scheduling

import (
	"time"

	"advancedmd-token-management/internal/domain"
)

func (s *service) signSlots(
	slots []domain.AvailabilitySlotOption,
	office *domain.OfficeConfig,
	routing domain.RoutingRule,
	dob string,
	now time.Time,
) ([]domain.AvailabilitySlotOption, time.Time, error) {
	issuedAt := now.Unix()
	expiresAt := now.Add(slotTokenTTL).Unix()
	appointmentTypeIDs := domain.NewSchedulingPolicy(office).AllowedAppointmentTypeIDs(routing, dob)
	for i := range slots {
		token, err := SignSlotToken(s.bookingTokenSecret, SlotPolicy{
			OfficeID:           office.ID,
			Routing:            string(routing),
			ColumnID:           slots[i].ColumnID,
			ProfileID:          slots[i].ProfileID,
			StartDatetime:      slots[i].DateTime,
			Duration:           slots[i].Duration,
			DOB:                domain.NormalizeDOB(dob),
			AppointmentTypeIDs: appointmentTypeIDs,
			SameStartBooked:    slots[i].SameStartBooked,
			SameStartCapacity:  slots[i].SameStartCapacity,
			RequiresForce:      slots[i].RequiresForce,
			Provider:           slots[i].Provider,
			IssuedAt:           issuedAt,
			ExpiresAt:          expiresAt,
		})
		if err != nil {
			return nil, time.Time{}, err
		}
		slots[i].BookingToken = token
		if slots[i].SameStartBooked == 0 {
			slots[i].SameStartCapacity = 0
		}
	}
	return slots, time.Unix(expiresAt, 0).UTC(), nil
}

func availableSlots(
	policy domain.SchedulingPolicy,
	column domain.SchedulerColumn,
	appointments []domain.Appointment,
	blockHolds []domain.BlockHold,
	date time.Time,
	nowEastern time.Time,
) []domain.AvailableSlot {
	slots := make([]domain.AvailableSlot, 0)
	workStart, workEnd, err := column.ParseWorkHours(date)
	if err != nil {
		return slots
	}

	interval := time.Duration(column.Interval) * time.Minute
	if interval == 0 {
		interval = 15 * time.Minute
	}
	for slotTime := workStart; slotTime.Before(workEnd); slotTime = slotTime.Add(interval) {
		if date.Format("2006-01-02") == nowEastern.Format("2006-01-02") &&
			slotTime.Before(nowEastern.Add(30*time.Minute)) {
			continue
		}
		if domain.IsBlockedByHold(slotTime, interval, blockHolds) ||
			hasDifferentStartOverlap(slotTime, interval, appointments) {
			continue
		}

		sameStartCount := countSameStart(slotTime, appointments)
		sameStart := policy.SameStart(column.ID, slotTime, sameStartCount)
		if !sameStart.Bookable {
			continue
		}
		slot := domain.AvailableSlot{
			Time:              domain.FormatSlotTime(slotTime),
			DateTime:          domain.FormatSlotDateTime(slotTime),
			SameStartBooked:   sameStartCount,
			SameStartCapacity: sameStart.Capacity,
			RequiresForce:     sameStart.RequiresForce,
		}
		slots = append(slots, slot)
	}
	return slots
}

func hasDifferentStartOverlap(slotTime time.Time, duration time.Duration, appointments []domain.Appointment) bool {
	slotEnd := slotTime.Add(duration)
	for _, appointment := range appointments {
		if appointment.StartDateTime.Equal(slotTime) {
			continue
		}
		appointmentEnd := appointment.StartDateTime.Add(time.Duration(appointment.Duration) * time.Minute)
		if slotTime.Before(appointmentEnd) && appointment.StartDateTime.Before(slotEnd) {
			return true
		}
	}
	return false
}

func countSameStart(slotTime time.Time, appointments []domain.Appointment) int {
	count := 0
	for _, appointment := range appointments {
		if appointment.StartDateTime.Equal(slotTime) {
			count++
		}
	}
	return count
}
