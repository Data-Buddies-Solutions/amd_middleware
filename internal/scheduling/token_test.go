package scheduling_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
)

func TestSlotTokenRejectsTamperingWrongSecretAndUnsafeTimes(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	policy := scheduling.SlotPolicy{
		OfficeID:      "spring_hill",
		Routing:       domain.RoutingAll,
		ColumnID:      1513,
		ProfileID:     620,
		StartDatetime: "2026-06-02T09:00",
		Duration:      15,
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(15 * time.Minute).Unix(),
	}
	token, err := scheduling.SignSlotToken("test-booking-secret", policy)
	if err != nil {
		t.Fatalf("SignSlotToken error = %v", err)
	}

	tests := []struct {
		name   string
		secret string
		token  string
		now    time.Time
	}{
		{name: "wrong secret", secret: "wrong-secret", token: token, now: now},
		{name: "tampered", secret: "test-booking-secret", token: token + "x", now: now},
		{name: "expired", secret: "test-booking-secret", token: token, now: now.Add(15 * time.Minute)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := scheduling.VerifySlotToken(test.secret, test.token, test.now); err == nil {
				t.Fatal("VerifySlotToken error = nil")
			}
		})
	}

	oversizedTTL := policy
	oversizedTTL.ExpiresAt = now.Add(16 * time.Minute).Unix()
	oversizedToken, err := scheduling.SignSlotToken("test-booking-secret", oversizedTTL)
	if err != nil {
		t.Fatalf("SignSlotToken oversized TTL error = %v", err)
	}
	if _, err := scheduling.VerifySlotToken("test-booking-secret", oversizedToken, now); err == nil {
		t.Fatal("VerifySlotToken accepted oversized TTL")
	}

	for _, routing := range []domain.RoutingRule{"bach-only", "", domain.RoutingNotAccepted} {
		unbookable := policy
		unbookable.Routing = routing
		unbookableToken, err := scheduling.SignSlotToken("test-booking-secret", unbookable)
		if err != nil {
			t.Fatalf("SignSlotToken routing %q error = %v", routing, err)
		}
		if _, err := scheduling.VerifySlotToken("test-booking-secret", unbookableToken, now); err == nil {
			t.Fatalf("VerifySlotToken accepted routing %q", routing)
		}
	}

	futureIssued := policy
	futureIssued.IssuedAt = now.Add(3 * time.Minute).Unix()
	futureIssued.ExpiresAt = now.Add(10 * time.Minute).Unix()
	futureToken, err := scheduling.SignSlotToken("test-booking-secret", futureIssued)
	if err != nil {
		t.Fatalf("SignSlotToken future issue error = %v", err)
	}
	if _, err := scheduling.VerifySlotToken("test-booking-secret", futureToken, now); err == nil {
		t.Fatal("VerifySlotToken accepted future issuance")
	}
}

func TestSlotTokenFromPreviousBuildWithProviderStillVerifies(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	body := fmt.Sprintf(`{"v":1,"officeId":"spring_hill","routing":"all_three","columnId":1513,"profileId":620,"provider":"Dr. Austin Bach","startDatetime":"2026-06-02T09:00","duration":15,"iat":%d,"exp":%d}`,
		now.Unix(), now.Add(15*time.Minute).Unix())
	encodedBody := base64.RawURLEncoding.EncodeToString([]byte(body))
	mac := hmac.New(sha256.New, []byte("test-booking-secret"))
	mac.Write([]byte(encodedBody))
	token := encodedBody + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	policy, err := scheduling.VerifySlotToken("test-booking-secret", token, now)
	if err != nil {
		t.Fatalf("VerifySlotToken rejected a slot token issued before SlotPolicy.Provider was removed: %v", err)
	}
	if policy.ColumnID != 1513 || policy.StartDatetime != "2026-06-02T09:00" {
		t.Fatalf("policy = %+v", policy)
	}
}
