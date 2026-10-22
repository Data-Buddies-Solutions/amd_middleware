package scheduling

import "time"

const (
	AvailabilityStatusSuccess = "success"
	AvailabilityStatusError   = "error"

	AvailabilityOutcomeFound               = "availability_found"
	AvailabilityOutcomeNoAvailability      = "no_availability"
	AvailabilityOutcomeNoEligibleProviders = "no_eligible_providers"
	AvailabilityOutcomeInvalidInput        = "invalid_input"
	AvailabilityOutcomeSearchIncomplete    = "availability_search_incomplete"

	availabilityNextActionOfferSlots                  = "offer_slots"
	AvailabilityNextActionAskDifferentPreferences     = "ask_for_different_preferences"
	AvailabilityNextActionRetryOnceThenAskPreferences = "retry_once_then_ask_preferences"
)

type AvailabilitySlotOption struct {
	Provider          string `json:"provider"`
	Time              string `json:"time"`
	DateTime          string `json:"datetime"`
	BookingToken      string `json:"bookingToken,omitempty"`
	ColumnID          int    `json:"columnId"`
	ProfileID         int    `json:"profileId"`
	Duration          int    `json:"duration"`
	SameStartBooked   int    `json:"sameStartBooked,omitempty"`
	SameStartCapacity int    `json:"sameStartCapacity,omitempty"`
	RequiresForce     bool   `json:"requiresForce,omitempty"`
}

type AvailabilityResponse struct {
	Status                string                   `json:"status"`
	Outcome               string                   `json:"outcome"`
	AvailabilityFound     bool                     `json:"availabilityFound"`
	RequestedDate         string                   `json:"requestedDate,omitempty"`
	ActualDate            string                   `json:"actualDate,omitempty"`
	DateShifted           bool                     `json:"dateShifted,omitempty"`
	SearchedFrom          string                   `json:"searchedFrom,omitempty"`
	SearchedThrough       string                   `json:"searchedThrough,omitempty"`
	BookingTokenExpiresAt string                   `json:"bookingTokenExpiresAt,omitempty"`
	ShouldRetrySameSearch bool                     `json:"shouldRetrySameSearch"`
	NextAction            string                   `json:"nextAction"`
	Message               string                   `json:"message,omitempty"`
	Slots                 []AvailabilitySlotOption `json:"slots"`
}

func formatSlotTime(t time.Time) string {
	return t.Format("3:04 PM")
}
