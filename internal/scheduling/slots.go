package scheduling

import (
	"time"

	"advancedmd-token-management/internal/domain"
)

func (s *service) signSlots(
	slots []AvailabilitySlotOption,
	office *domain.OfficeConfig,
	routing domain.RoutingRule,
	dob string,
	now time.Time,
) ([]AvailabilitySlotOption, time.Time, error) {
	issuedAt := now.Unix()
	expiresAt := now.Add(tokenTTL).Unix()
	appointmentTypeIDs := newSchedulingPolicy(office, now).AllowedAppointmentTypeIDs(routing, dob)
	for i := range slots {
		token, err := SignSlotToken(s.bookingTokenSecret, SlotPolicy{
			OfficeID:           office.ID,
			Routing:            routing,
			ColumnID:           slots[i].ColumnID,
			ProfileID:          slots[i].ProfileID,
			StartDatetime:      slots[i].DateTime,
			Duration:           slots[i].Duration,
			DOB:                domain.NormalizeDOB(dob),
			AppointmentTypeIDs: appointmentTypeIDs,
			SameStartBooked:    slots[i].SameStartBooked,
			SameStartCapacity:  slots[i].SameStartCapacity,
			RequiresForce:      slots[i].RequiresForce,
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

const defaultSameStartCapacity = 1

type slotDecision struct {
	Booked        int
	Capacity      int
	Bookable      bool
	RequiresForce bool
}

func (p schedulingPolicy) Slot(columnID string, slot domain.Interval, schedule domain.ColumnSchedule) slotDecision {
	for _, hold := range schedule.BlockHolds {
		if slot.Overlaps(hold.Interval()) {
			return slotDecision{}
		}
	}
	booked := 0
	for _, appointment := range schedule.Appointments {
		if appointment.StartDateTime.Equal(slot.Start) {
			booked++
		} else if slot.Overlaps(appointment.Interval()) {
			return slotDecision{}
		}
	}
	capacity := defaultSameStartCapacity
	if p.office != nil {
		if column, ok := p.office.Columns[columnID]; ok {
			capacity = max(capacity, column.SameStartCapacityAt(slot.Start))
		}
	}
	return slotDecision{
		Booked:        booked,
		Capacity:      capacity,
		Bookable:      booked < capacity,
		RequiresForce: booked > 0 && booked < capacity,
	}
}
