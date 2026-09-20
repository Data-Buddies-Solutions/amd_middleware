package scheduling

import (
	"context"
	"errors"
	"sync"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

const (
	searchForwardDays      = 14
	schedulerSetupCacheTTL = 6 * time.Hour
)

var eastern = domain.EasternLocation()

// SearchCommand is the domain input for one availability search.
type SearchCommand struct {
	PatientID       string                      `json:"patientId,omitempty"`
	InsurancePlan   string                      `json:"insurancePlan,omitempty"`
	CoverageType    string                      `json:"coverageType,omitempty"`
	VisitType       string                      `json:"visitType,omitempty"`
	RequestedDate   string                      `json:"requestedDate,omitempty"`
	PreferredTime   *AvailabilityTimePreference `json:"preferredTime,omitempty"`
	Provider        string                      `json:"provider"`
	Office          string                      `json:"office"`
	Routing         string                      `json:"routing"`
	DOB             string                      `json:"dob,omitempty"`
	PreauthRequired bool                        `json:"preauthRequired"`
}

type AvailabilityTimeKind string

const (
	AvailabilityTimeMorning   AvailabilityTimeKind = "morning"
	AvailabilityTimeAfternoon AvailabilityTimeKind = "afternoon"
)

// AvailabilityTimePreference is a canonical clinic-local time preference.
type AvailabilityTimePreference struct {
	Kind        AvailabilityTimeKind `json:"kind,omitempty"`
	MinuteOfDay *int                 `json:"minuteOfDay,omitempty"`
}

// Scheduling is the complete scheduling boundary used by HTTP.
type Scheduling interface {
	Search(ctx context.Context, command SearchCommand) (domain.AvailabilityResponse, error)
	List(ctx context.Context, command ListCommand) (domain.AvailabilityResponse, error)
	Book(ctx context.Context, command BookCommand) (BookReceipt, error)
	Cancel(ctx context.Context, command CancelCommand) (CancelReceipt, error)
	Reschedule(ctx context.Context, command BookCommand) (RescheduleReceipt, error)
}

// Category is a stable, provider-independent scheduling outcome.
type Category string

const (
	CategoryValidation               Category = "validation"
	CategoryPolicyBlocked            Category = "policy_blocked"
	CategoryInvalidBookingToken      Category = "invalid_booking_token"
	CategoryInvalidCancellationToken Category = "invalid_cancellation_token"
	CategoryInvalidRescheduleToken   Category = "invalid_reschedule_token"
	CategoryBookingTokenRequired     Category = "booking_token_required"
	CategoryAppointmentTypeMissing   Category = "appointment_type_unresolved"
	CategoryPatientContextMismatch   Category = "patient_context_mismatch"
	CategorySlotUnavailable          Category = "slot_unavailable"
	CategoryProviderConflict         Category = "provider_conflict"
	CategoryProviderRejected         Category = "provider_rejected"
	CategoryOwnershipMismatch        Category = "ownership_mismatch"
	CategoryWriteFailed              Category = "write_failed"
	CategoryIndeterminateWrite       Category = "indeterminate_write"
)

// Error contains only caller-safe scheduling failure details.
type Error struct {
	category        Category
	providerFailure safeerrors.Category
	message         string
	missing         []string
}

func (e *Error) Error() string {
	return e.message
}

// CategoryOf returns the stable domain category for a Scheduling error.
func CategoryOf(err error) Category {
	var schedulingErr *Error
	if errors.As(err, &schedulingErr) {
		return schedulingErr.category
	}
	return CategoryValidation
}

// ProviderFailureOf returns the stable provider category carried by a
// Scheduling error, or none when the failure did not come from AdvancedMD.
func ProviderFailureOf(err error) safeerrors.Category {
	var schedulingErr *Error
	if errors.As(err, &schedulingErr) {
		if schedulingErr.providerFailure == "" {
			return safeerrors.CategoryNone
		}
		return schedulingErr.providerFailure
	}
	return safeerrors.CategoryNone
}

// MissingOf returns caller-safe fields required to complete a Scheduling
// command.
func MissingOf(err error) []string {
	var schedulingErr *Error
	if errors.As(err, &schedulingErr) {
		return append([]string(nil), schedulingErr.missing...)
	}
	return nil
}

type service struct {
	records            advancedmd.SchedulingRecords
	bookingTokenSecret string
	appointmentTokens  *AppointmentTokens
	allowRawBooking    bool
	now                func() time.Time

	setupMu        sync.Mutex
	setup          *domain.SchedulerSetup
	setupExpiresAt time.Time
	setupLoadedAt  time.Time
	setupFlight    *setupRefresh
}

// Config makes compatibility behavior explicit at composition time.
type Config struct {
	AllowRawBooking bool
}

// New constructs Scheduling with compatibility behavior disabled.
func New(records advancedmd.SchedulingRecords, bookingTokenSecret string, now func() time.Time) Scheduling {
	return NewWithConfig(records, bookingTokenSecret, now, Config{})
}

// NewWithConfig constructs the single owner for scheduling behavior.
func NewWithConfig(
	records advancedmd.SchedulingRecords,
	bookingTokenSecret string,
	now func() time.Time,
	config Config,
) Scheduling {
	if now == nil {
		now = time.Now
	}
	return &service{
		records:            records,
		bookingTokenSecret: bookingTokenSecret,
		appointmentTokens:  NewAppointmentTokens(bookingTokenSecret, now),
		allowRawBooking:    config.AllowRawBooking,
		now:                now,
	}
}

func schedulingError(message string) error {
	return categorizedError(CategoryValidation, message)
}

func categorizedError(category Category, message string) error {
	return &Error{category: category, message: message}
}

func categorizedProviderError(category Category, providerFailure safeerrors.Category, message string) error {
	return &Error{category: category, providerFailure: providerFailure, message: message}
}

func providerError(err error, fallback string) error {
	return &Error{
		category:        CategoryValidation,
		providerFailure: providerCategory(err),
		message:         providerFailureMessage(err, fallback),
	}
}

func providerFailureMessage(err error, fallback string) string {
	switch providerCategory(err) {
	case safeerrors.CategoryAuthentication, safeerrors.CategoryUnavailable:
		return "Service authentication is temporarily unavailable. Please try again."
	default:
		return fallback
	}
}

func providerCategory(err error) safeerrors.Category {
	category := advancedmd.CategoryOf(err)
	if category == safeerrors.CategoryInternal {
		return safeerrors.Classify(err)
	}
	return category
}
