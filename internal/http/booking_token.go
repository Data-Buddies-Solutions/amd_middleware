package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const bookingTokenTTL = 30 * time.Minute

type bookingTokenPayload struct {
	ColumnID      int    `json:"columnId"`
	ProfileID     int    `json:"profileId"`
	FacilityID    string `json:"facilityId"`
	StartDatetime string `json:"startDatetime"`
	Duration      int    `json:"duration"`
	Office        string `json:"office"`
	Exp           int64  `json:"exp"`
}

func (h *Handlers) mintBookingToken(payload bookingTokenPayload) (string, error) {
	if payload.Exp == 0 {
		payload.Exp = time.Now().Add(bookingTokenTTL).Unix()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal booking token: %w", err)
	}

	payloadPart := base64.RawURLEncoding.EncodeToString(body)
	sigPart := base64.RawURLEncoding.EncodeToString(signBookingToken(h.bookingTokenSecret, payloadPart))
	return payloadPart + "." + sigPart, nil
}

func (h *Handlers) parseBookingToken(token string) (*bookingTokenPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid token format")
	}

	expectedSig := signBookingToken(h.bookingTokenSecret, parts[0])
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid token signature")
	}
	if !hmac.Equal(gotSig, expectedSig) {
		return nil, fmt.Errorf("invalid token signature")
	}

	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid token payload")
	}

	var payload bookingTokenPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid token payload")
	}
	if payload.Exp == 0 || time.Now().Unix() > payload.Exp {
		return nil, fmt.Errorf("expired token")
	}
	return &payload, nil
}

func signBookingToken(secret, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}
