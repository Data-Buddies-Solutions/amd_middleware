package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"advancedmd-token-management/internal/domain"
)

const (
	bookingTokenVersion   = 1
	bookingTokenTTL       = 15 * time.Minute
	bookingTokenClockSkew = 2 * time.Minute
)

type bookingTokenPayload struct {
	Version            int    `json:"v"`
	OfficeID           string `json:"officeId"`
	Routing            string `json:"routing"`
	ColumnID           int    `json:"columnId"`
	ProfileID          int    `json:"profileId"`
	StartDatetime      string `json:"startDatetime"`
	Duration           int    `json:"duration"`
	DOB                string `json:"dob,omitempty"`
	AppointmentTypeIDs []int  `json:"appointmentTypeIds,omitempty"`
	SameStartBooked    int    `json:"sameStartBooked,omitempty"`
	SameStartCapacity  int    `json:"sameStartCapacity,omitempty"`
	RequiresForce      bool   `json:"requiresForce,omitempty"`
	Provider           string `json:"provider,omitempty"`
	IssuedAt           int64  `json:"iat"`
	ExpiresAt          int64  `json:"exp"`
}

var (
	errBookingTokenSecretMissing = errors.New("booking token secret is not configured")
	errBookingTokenInvalid       = errors.New("invalid booking token")
	errBookingTokenExpired       = errors.New("booking token expired")
)

func signBookingToken(secret string, payload bookingTokenPayload) (string, error) {
	if secret == "" {
		return "", errBookingTokenSecretMissing
	}
	payload.Version = bookingTokenVersion
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal booking token payload: %w", err)
	}
	encodedBody := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encodedBody))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedBody + "." + signature, nil
}

func verifyBookingToken(secret, token string, now time.Time) (bookingTokenPayload, error) {
	if secret == "" {
		return bookingTokenPayload{}, errBookingTokenSecretMissing
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return bookingTokenPayload{}, errBookingTokenInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	expectedSignature := mac.Sum(nil)
	actualSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(actualSignature, expectedSignature) {
		return bookingTokenPayload{}, errBookingTokenInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return bookingTokenPayload{}, errBookingTokenInvalid
	}
	var payload bookingTokenPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return bookingTokenPayload{}, errBookingTokenInvalid
	}
	if payload.Version != bookingTokenVersion ||
		payload.OfficeID == "" ||
		payload.ColumnID <= 0 ||
		payload.ProfileID <= 0 ||
		payload.StartDatetime == "" ||
		payload.Duration <= 0 ||
		payload.IssuedAt <= 0 ||
		payload.ExpiresAt == 0 {
		return bookingTokenPayload{}, errBookingTokenInvalid
	}
	issuedAt := time.Unix(payload.IssuedAt, 0)
	expiresAt := time.Unix(payload.ExpiresAt, 0)
	if !payloadHasValidRouting(payload.Routing) ||
		!expiresAt.After(issuedAt) ||
		expiresAt.Sub(issuedAt) > bookingTokenTTL ||
		issuedAt.After(now.Add(bookingTokenClockSkew)) {
		return bookingTokenPayload{}, errBookingTokenInvalid
	}
	if !now.Before(expiresAt) {
		return bookingTokenPayload{}, errBookingTokenExpired
	}
	return payload, nil
}

func payloadHasValidRouting(routing string) bool {
	switch domain.RoutingRule(routing) {
	case domain.RoutingBachOnly, domain.RoutingBachLicht, domain.RoutingAll, domain.RoutingOpticalOnly:
		return true
	default:
		return false
	}
}

func (w *schedulingWorkflow) bookingSecret() string {
	if w == nil {
		return ""
	}
	return w.bookingTokenSecret
}

func (w *schedulingWorkflow) addBookingTokens(slots []domain.AvailabilitySlotOption, office *domain.OfficeConfig, routing domain.RoutingRule, dob string, now time.Time) ([]domain.AvailabilitySlotOption, error) {
	if len(slots) == 0 {
		return slots, nil
	}
	issuedAt := now.Unix()
	expiresAt := now.Add(bookingTokenTTL).Unix()
	appointmentTypeIDs := domain.NewSchedulingPolicy(office).AllowedAppointmentTypeIDs(routing, dob)
	for i := range slots {
		token, err := signBookingToken(w.bookingSecret(), bookingTokenPayload{
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
			return nil, err
		}
		slots[i].BookingToken = token
	}
	return slots, nil
}

func (w *schedulingWorkflow) applyBookingToken(req *BookAppointmentRequest, requestedOffice *domain.OfficeConfig, now time.Time) (*domain.OfficeConfig, error) {
	payload, err := verifyBookingToken(w.bookingSecret(), req.BookingToken, now)
	if err != nil {
		return nil, err
	}
	office, ok := lookupOfficeByID(payload.OfficeID)
	if !ok {
		return nil, errBookingTokenInvalid
	}
	if requestedOffice != nil && requestedOffice.ID != office.ID {
		return nil, errBookingTokenInvalid
	}
	if payload.DOB != "" && req.DOB != "" {
		if _, valid := domain.AgeYears(req.DOB); valid && domain.NormalizeDOB(req.DOB) != payload.DOB {
			return nil, errBookingTokenInvalid
		}
	}
	req.ColumnID = payload.ColumnID
	req.ProfileID = payload.ProfileID
	req.StartDatetime = payload.StartDatetime
	req.Duration = payload.Duration
	req.Routing = payload.Routing
	if payload.DOB != "" && (req.DOB == "" || domain.NormalizeDOB(req.DOB) == payload.DOB) {
		req.DOB = payload.DOB
	}
	req.bookingRequiresForce = payload.RequiresForce
	req.bookingAppointmentTypeIDs = append([]int(nil), payload.AppointmentTypeIDs...)
	return office, nil
}

func lookupOfficeByID(officeID string) (*domain.OfficeConfig, bool) {
	return domain.LookupOfficeByID(officeID)
}
