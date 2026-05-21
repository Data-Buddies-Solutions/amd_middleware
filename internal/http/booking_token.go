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
	Version       int    `json:"v"`
	OfficeID      string `json:"officeId"`
	Routing       string `json:"routing"`
	ColumnID      int    `json:"columnId"`
	ProfileID     int    `json:"profileId"`
	StartDatetime string `json:"startDatetime"`
	Duration      int    `json:"duration"`
	Provider      string `json:"provider,omitempty"`
	IssuedAt      int64  `json:"iat"`
	ExpiresAt     int64  `json:"exp"`
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

func (h *Handlers) bookingSecret() string {
	if h == nil {
		return ""
	}
	return h.bookingTokenSecret
}

func (h *Handlers) addBookingTokens(slots []domain.AvailabilitySlotOption, office *domain.OfficeConfig, routing domain.RoutingRule, now time.Time) ([]domain.AvailabilitySlotOption, error) {
	if len(slots) == 0 {
		return slots, nil
	}
	issuedAt := now.Unix()
	expiresAt := now.Add(bookingTokenTTL).Unix()
	for i := range slots {
		token, err := signBookingToken(h.bookingSecret(), bookingTokenPayload{
			OfficeID:      office.ID,
			Routing:       string(routing),
			ColumnID:      slots[i].ColumnID,
			ProfileID:     slots[i].ProfileID,
			StartDatetime: slots[i].DateTime,
			Duration:      slots[i].Duration,
			Provider:      slots[i].Provider,
			IssuedAt:      issuedAt,
			ExpiresAt:     expiresAt,
		})
		if err != nil {
			return nil, err
		}
		slots[i].BookingToken = token
	}
	return slots, nil
}

func (h *Handlers) applyBookingToken(req *BookAppointmentRequest, office *domain.OfficeConfig, now time.Time) error {
	payload, err := verifyBookingToken(h.bookingSecret(), req.BookingToken, now)
	if err != nil {
		return err
	}
	if payload.OfficeID != office.ID {
		return errBookingTokenInvalid
	}
	req.ColumnID = payload.ColumnID
	req.ProfileID = payload.ProfileID
	req.StartDatetime = payload.StartDatetime
	req.Duration = payload.Duration
	req.Routing = payload.Routing
	return nil
}
