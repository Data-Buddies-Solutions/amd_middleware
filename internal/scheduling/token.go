package scheduling

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
	slotTokenVersion   = 1
	slotTokenTTL       = 15 * time.Minute
	slotTokenClockSkew = 2 * time.Minute
)

type SlotPolicy struct {
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
	ErrSlotTokenSecretMissing = errors.New("booking token secret is not configured")
	ErrSlotTokenInvalid       = errors.New("invalid booking token")
	ErrSlotTokenExpired       = errors.New("booking token expired")
)

func SignSlotToken(secret string, policy SlotPolicy) (string, error) {
	if secret == "" {
		return "", ErrSlotTokenSecretMissing
	}
	policy.Version = slotTokenVersion
	body, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("marshal booking token payload: %w", err)
	}
	encodedBody := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encodedBody))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedBody + "." + signature, nil
}

func VerifySlotToken(secret, token string, now time.Time) (SlotPolicy, error) {
	if secret == "" {
		return SlotPolicy{}, ErrSlotTokenSecretMissing
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return SlotPolicy{}, ErrSlotTokenInvalid
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	actualSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(actualSignature, mac.Sum(nil)) {
		return SlotPolicy{}, ErrSlotTokenInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return SlotPolicy{}, ErrSlotTokenInvalid
	}

	var policy SlotPolicy
	if err := json.Unmarshal(body, &policy); err != nil {
		return SlotPolicy{}, ErrSlotTokenInvalid
	}
	if policy.Version != slotTokenVersion ||
		policy.OfficeID == "" ||
		policy.ColumnID <= 0 ||
		policy.ProfileID <= 0 ||
		policy.StartDatetime == "" ||
		policy.Duration <= 0 ||
		policy.IssuedAt <= 0 ||
		policy.ExpiresAt == 0 {
		return SlotPolicy{}, ErrSlotTokenInvalid
	}

	issuedAt := time.Unix(policy.IssuedAt, 0)
	expiresAt := time.Unix(policy.ExpiresAt, 0)
	if !validRouting(policy.Routing) ||
		!expiresAt.After(issuedAt) ||
		expiresAt.Sub(issuedAt) > slotTokenTTL ||
		issuedAt.After(now.Add(slotTokenClockSkew)) {
		return SlotPolicy{}, ErrSlotTokenInvalid
	}
	if !now.Before(expiresAt) {
		return SlotPolicy{}, ErrSlotTokenExpired
	}
	return policy, nil
}

func validRouting(routing string) bool {
	switch domain.RoutingRule(routing) {
	case domain.RoutingBachOnly, domain.RoutingBachLicht, domain.RoutingAll, domain.RoutingOpticalOnly:
		return true
	default:
		return false
	}
}
