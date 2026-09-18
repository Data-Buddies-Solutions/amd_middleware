package scheduling

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"advancedmd-token-management/internal/domain"
)

const (
	cancellationTokenVersion = 1
	cancellationTokenPurpose = "appointment_cancellation"
	cancellationTokenTTL     = 15 * time.Minute
	cancellationTokenDomain  = "scheduling-token/appointment-cancellation/v1\x00"
	rescheduleTokenPurpose   = "appointment_reschedule"
	rescheduleTokenDomain    = "scheduling-token/appointment-reschedule/v1\x00"
)

var (
	ErrAppointmentTokenSecretMissing = errors.New("appointment token secret is not configured")
	ErrAppointmentTokenInvalid       = errors.New("invalid appointment token")
	ErrAppointmentTokenExpired       = errors.New("appointment token expired")
)

type appointmentTokenPolicy struct {
	Version           int    `json:"v"`
	Purpose           string `json:"purpose"`
	PatientID         string `json:"patientId"`
	AppointmentID     int    `json:"appointmentId"`
	AppointmentTypeID int    `json:"appointmentTypeId,omitempty"`
	OfficeID          string `json:"officeId"`
	Start             string `json:"start"`
	IssuedAt          int64  `json:"iat"`
	ExpiresAt         int64  `json:"exp"`

	start time.Time
}

// AppointmentTokens issues purpose-specific private appointment tokens. The
// same scheduling secret is cryptographically separated by distinct HMAC
// domains for cancellation and rescheduling.
type AppointmentTokens struct {
	secret string
	now    func() time.Time
}

func NewAppointmentTokens(secret string, now func() time.Time) *AppointmentTokens {
	if now == nil {
		now = time.Now
	}
	return &AppointmentTokens{secret: secret, now: now}
}

func (t *AppointmentTokens) IssueCancellationToken(
	patientID string,
	appointment domain.PatientAppointment,
) (string, error) {
	return t.issueToken(
		patientID,
		appointment,
		cancellationTokenPurpose,
		cancellationTokenDomain,
	)
}

func (t *AppointmentTokens) IssueRescheduleToken(
	patientID string,
	appointment domain.PatientAppointment,
) (string, error) {
	return t.issueToken(
		patientID,
		appointment,
		rescheduleTokenPurpose,
		rescheduleTokenDomain,
	)
}

func (t *AppointmentTokens) issueToken(
	patientID string,
	appointment domain.PatientAppointment,
	purpose string,
	domainSeparator string,
) (string, error) {
	if t == nil || t.secret == "" {
		return "", ErrAppointmentTokenSecretMissing
	}
	patientID = domain.StripPatientPrefix(strings.TrimSpace(patientID))
	if _, err := strconv.Atoi(patientID); err != nil ||
		appointment.ID <= 0 ||
		appointment.OfficeID == "" ||
		appointment.Start.IsZero() {
		return "", ErrAppointmentTokenInvalid
	}
	if _, ok := domain.LookupOfficeByID(appointment.OfficeID); !ok {
		return "", ErrAppointmentTokenInvalid
	}

	now := t.now().UTC()
	policy := appointmentTokenPolicy{
		Version:           cancellationTokenVersion,
		Purpose:           purpose,
		PatientID:         patientID,
		AppointmentID:     appointment.ID,
		AppointmentTypeID: appointment.AppointmentTypeID,
		OfficeID:          appointment.OfficeID,
		Start:             appointment.Start.UTC().Format(time.RFC3339),
		IssuedAt:          now.Unix(),
		ExpiresAt:         now.Add(cancellationTokenTTL).Unix(),
	}
	body, err := json.Marshal(policy)
	if err != nil {
		return "", ErrAppointmentTokenInvalid
	}
	encodedBody := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(t.secret))
	mac.Write([]byte(domainSeparator))
	mac.Write([]byte(encodedBody))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedBody + "." + signature, nil
}

func (t *AppointmentTokens) verify(
	token string,
	now time.Time,
) (appointmentTokenPolicy, error) {
	return t.verifyToken(
		token,
		now,
		cancellationTokenPurpose,
		cancellationTokenDomain,
	)
}

func (t *AppointmentTokens) verifyReschedule(
	token string,
	now time.Time,
) (appointmentTokenPolicy, error) {
	return t.verifyToken(
		token,
		now,
		rescheduleTokenPurpose,
		rescheduleTokenDomain,
	)
}

func (t *AppointmentTokens) verifyToken(
	token string,
	now time.Time,
	purpose string,
	domainSeparator string,
) (appointmentTokenPolicy, error) {
	if t == nil || t.secret == "" {
		return appointmentTokenPolicy{}, ErrAppointmentTokenSecretMissing
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return appointmentTokenPolicy{}, ErrAppointmentTokenInvalid
	}

	mac := hmac.New(sha256.New, []byte(t.secret))
	mac.Write([]byte(domainSeparator))
	mac.Write([]byte(parts[0]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return appointmentTokenPolicy{}, ErrAppointmentTokenInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return appointmentTokenPolicy{}, ErrAppointmentTokenInvalid
	}
	var policy appointmentTokenPolicy
	if err := json.Unmarshal(body, &policy); err != nil {
		return appointmentTokenPolicy{}, ErrAppointmentTokenInvalid
	}
	start, err := time.Parse(time.RFC3339, policy.Start)
	if err != nil ||
		policy.Version != cancellationTokenVersion ||
		policy.Purpose != purpose ||
		policy.PatientID == "" ||
		policy.AppointmentID <= 0 ||
		policy.OfficeID == "" ||
		policy.IssuedAt <= 0 ||
		policy.ExpiresAt <= 0 {
		return appointmentTokenPolicy{}, ErrAppointmentTokenInvalid
	}
	if _, err := strconv.Atoi(policy.PatientID); err != nil {
		return appointmentTokenPolicy{}, ErrAppointmentTokenInvalid
	}
	if _, ok := domain.LookupOfficeByID(policy.OfficeID); !ok || start.IsZero() {
		return appointmentTokenPolicy{}, ErrAppointmentTokenInvalid
	}

	issuedAt := time.Unix(policy.IssuedAt, 0)
	expiresAt := time.Unix(policy.ExpiresAt, 0)
	if !expiresAt.After(issuedAt) ||
		expiresAt.Sub(issuedAt) > cancellationTokenTTL ||
		issuedAt.After(now.Add(slotTokenClockSkew)) {
		return appointmentTokenPolicy{}, ErrAppointmentTokenInvalid
	}
	if !now.Before(expiresAt) {
		return appointmentTokenPolicy{}, ErrAppointmentTokenExpired
	}
	policy.start = start
	return policy, nil
}
