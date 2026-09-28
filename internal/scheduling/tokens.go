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
	tokenVersion   = 1
	tokenTTL       = 15 * time.Minute
	tokenClockSkew = 2 * time.Minute

	cancellationTokenPurpose = "appointment_cancellation"
	cancellationTokenDomain  = "scheduling-token/appointment-cancellation/v1\x00"
	rescheduleTokenPurpose   = "appointment_reschedule"
	rescheduleTokenDomain    = "scheduling-token/appointment-reschedule/v1\x00"
)

var (
	errTokenSecretMissing = errors.New("token secret is not configured")
	errTokenInvalid       = errors.New("invalid token")
	errTokenExpired       = errors.New("token expired")
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

func SignSlotToken(secret string, policy SlotPolicy) (string, error) {
	policy.Version = tokenVersion
	return signToken(secret, "", policy)
}

func VerifySlotToken(secret, token string, now time.Time) (SlotPolicy, error) {
	var policy SlotPolicy
	if err := openToken(secret, "", token, &policy); err != nil {
		return SlotPolicy{}, err
	}
	if policy.Version != tokenVersion ||
		policy.OfficeID == "" ||
		policy.ColumnID <= 0 ||
		policy.ProfileID <= 0 ||
		policy.StartDatetime == "" ||
		policy.Duration <= 0 ||
		policy.IssuedAt <= 0 ||
		policy.ExpiresAt == 0 ||
		!validRouting(policy.Routing) {
		return SlotPolicy{}, errTokenInvalid
	}
	if err := checkTokenLifetime(policy.IssuedAt, policy.ExpiresAt, now); err != nil {
		return SlotPolicy{}, err
	}
	return policy, nil
}

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

func (t *AppointmentTokens) IssueCancellationToken(patientID string, appointment domain.PatientAppointment) (string, error) {
	return t.issue(patientID, appointment, cancellationTokenPurpose, cancellationTokenDomain)
}

func (t *AppointmentTokens) IssueRescheduleToken(patientID string, appointment domain.PatientAppointment) (string, error) {
	return t.issue(patientID, appointment, rescheduleTokenPurpose, rescheduleTokenDomain)
}

func (t *AppointmentTokens) verifyCancellation(token string, now time.Time) (appointmentTokenPolicy, error) {
	return t.verify(token, now, cancellationTokenPurpose, cancellationTokenDomain)
}

func (t *AppointmentTokens) verifyReschedule(token string, now time.Time) (appointmentTokenPolicy, error) {
	return t.verify(token, now, rescheduleTokenPurpose, rescheduleTokenDomain)
}

func (t *AppointmentTokens) issue(patientID string, appointment domain.PatientAppointment, purpose, domainSeparator string) (string, error) {
	patientID = domain.StripPatientPrefix(strings.TrimSpace(patientID))
	if _, err := strconv.Atoi(patientID); err != nil ||
		appointment.ID <= 0 ||
		appointment.OfficeID == "" ||
		appointment.Start.IsZero() {
		return "", errTokenInvalid
	}
	if _, ok := domain.LookupOfficeByID(appointment.OfficeID); !ok {
		return "", errTokenInvalid
	}
	now := t.now().UTC()
	return signToken(t.secret, domainSeparator, appointmentTokenPolicy{
		Version:           tokenVersion,
		Purpose:           purpose,
		PatientID:         patientID,
		AppointmentID:     appointment.ID,
		AppointmentTypeID: appointment.AppointmentTypeID,
		OfficeID:          appointment.OfficeID,
		Start:             appointment.Start.UTC().Format(time.RFC3339),
		IssuedAt:          now.Unix(),
		ExpiresAt:         now.Add(tokenTTL).Unix(),
	})
}

func (t *AppointmentTokens) verify(token string, now time.Time, purpose, domainSeparator string) (appointmentTokenPolicy, error) {
	var policy appointmentTokenPolicy
	if err := openToken(t.secret, domainSeparator, token, &policy); err != nil {
		return appointmentTokenPolicy{}, err
	}
	start, err := time.Parse(time.RFC3339, policy.Start)
	if err != nil ||
		start.IsZero() ||
		policy.Version != tokenVersion ||
		policy.Purpose != purpose ||
		policy.AppointmentID <= 0 ||
		policy.IssuedAt <= 0 ||
		policy.ExpiresAt <= 0 {
		return appointmentTokenPolicy{}, errTokenInvalid
	}
	if _, err := strconv.Atoi(policy.PatientID); err != nil {
		return appointmentTokenPolicy{}, errTokenInvalid
	}
	if _, ok := domain.LookupOfficeByID(policy.OfficeID); !ok {
		return appointmentTokenPolicy{}, errTokenInvalid
	}
	if err := checkTokenLifetime(policy.IssuedAt, policy.ExpiresAt, now); err != nil {
		return appointmentTokenPolicy{}, err
	}
	policy.start = start
	return policy, nil
}

func signToken(secret, domainSeparator string, payload any) (string, error) {
	if secret == "" {
		return "", errTokenSecretMissing
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", errTokenInvalid
	}
	encodedBody := base64.RawURLEncoding.EncodeToString(body)
	return encodedBody + "." + base64.RawURLEncoding.EncodeToString(tokenMAC(secret, domainSeparator, encodedBody)), nil
}

func openToken(secret, domainSeparator, token string, payload any) error {
	if secret == "" {
		return errTokenSecretMissing
	}
	encodedBody, encodedSignature, ok := strings.Cut(token, ".")
	if !ok || encodedBody == "" || encodedSignature == "" || strings.Contains(encodedSignature, ".") {
		return errTokenInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil || !hmac.Equal(signature, tokenMAC(secret, domainSeparator, encodedBody)) {
		return errTokenInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(encodedBody)
	if err != nil || json.Unmarshal(body, payload) != nil {
		return errTokenInvalid
	}
	return nil
}

func tokenMAC(secret, domainSeparator, encodedBody string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(domainSeparator))
	mac.Write([]byte(encodedBody))
	return mac.Sum(nil)
}

func checkTokenLifetime(issuedAtUnix, expiresAtUnix int64, now time.Time) error {
	issuedAt := time.Unix(issuedAtUnix, 0)
	expiresAt := time.Unix(expiresAtUnix, 0)
	if !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > tokenTTL || issuedAt.After(now.Add(tokenClockSkew)) {
		return errTokenInvalid
	}
	if !now.Before(expiresAt) {
		return errTokenExpired
	}
	return nil
}

func validRouting(routing string) bool {
	switch domain.RoutingRule(routing) {
	case domain.RoutingBachOnly, domain.RoutingBachLicht, domain.RoutingAll, domain.RoutingOpticalOnly:
		return true
	default:
		return false
	}
}
