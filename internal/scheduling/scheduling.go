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
	schedulerSetupCacheTTL = 6 * time.Hour
)

var eastern = domain.EasternLocation()

type Scheduling interface {
	List(ctx context.Context, command ListCommand) (AvailabilityResponse, error)
	Book(ctx context.Context, command BookCommand) (BookReceipt, error)
	Cancel(ctx context.Context, command CancelCommand) (CancelReceipt, error)
	Reschedule(ctx context.Context, command BookCommand) (RescheduleReceipt, error)
}

type Category string

const (
	CategoryValidation               Category = "validation"
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

type Error struct {
	category        Category
	providerFailure safeerrors.Category
	message         string
	missing         []string
}

func (e *Error) Error() string {
	return e.message
}

func CategoryOf(err error) Category {
	var schedulingErr *Error
	if errors.As(err, &schedulingErr) {
		return schedulingErr.category
	}
	return CategoryValidation
}

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
	now                func() time.Time

	setupMu        sync.Mutex
	setup          *domain.SchedulerSetup
	setupExpiresAt time.Time
	setupLoadedAt  time.Time
	setupFlight    *setupRefresh
}

func New(
	records advancedmd.SchedulingRecords,
	bookingTokenSecret string,
	now func() time.Time,
) Scheduling {
	if now == nil {
		now = time.Now
	}
	return &service{
		records:            records,
		bookingTokenSecret: bookingTokenSecret,
		appointmentTokens:  NewAppointmentTokens(bookingTokenSecret, now),
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

func providerWriteError(err error, conflictCategory Category, conflictMessage, rejectedMessage, failedMessage string) error {
	failure := providerCategory(err)
	switch failure {
	case safeerrors.CategoryConflict:
		return categorizedProviderError(conflictCategory, failure, conflictMessage)
	case safeerrors.CategoryRejected:
		return categorizedProviderError(CategoryProviderRejected, failure, rejectedMessage)
	default:
		return categorizedProviderError(CategoryWriteFailed, failure, providerFailureMessage(err, failedMessage))
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
