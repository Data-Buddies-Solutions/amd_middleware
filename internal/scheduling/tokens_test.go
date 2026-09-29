package scheduling

import (
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
)

func TestTokenFormatIsStable(t *testing.T) {
	domain.InitRegistry("")
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	slot, err := SignSlotToken("fixture-secret", SlotPolicy{OfficeID: "spring_hill", Routing: "all_three", ColumnID: 1513, ProfileID: 620, StartDatetime: "2026-06-02T09:00", Duration: 15, IssuedAt: now.Unix(), ExpiresAt: now.Add(15 * time.Minute).Unix()})
	if err != nil || slot != "eyJ2IjoxLCJvZmZpY2VJZCI6InNwcmluZ19oaWxsIiwicm91dGluZyI6ImFsbF90aHJlZSIsImNvbHVtbklkIjoxNTEzLCJwcm9maWxlSWQiOjYyMCwic3RhcnREYXRldGltZSI6IjIwMjYtMDYtMDJUMDk6MDAiLCJkdXJhdGlvbiI6MTUsImlhdCI6MTc4MDMxNTIwMCwiZXhwIjoxNzgwMzE2MTAwfQ.aVbToR7dsPoDnN7NFFFEkhc62lSi7cEqLOpvk3PGhtY" {
		t.Fatalf("slot token = %q, err = %v", slot, err)
	}
	tokens := NewAppointmentTokens("fixture-secret", func() time.Time { return now })
	appointment := domain.PatientAppointment{ID: 42, OfficeID: "spring_hill", AppointmentTypeID: 1007, Start: time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)}
	cancellation, err := tokens.IssueCancellationToken("12345", appointment)
	if err != nil || cancellation != "eyJ2IjoxLCJwdXJwb3NlIjoiYXBwb2ludG1lbnRfY2FuY2VsbGF0aW9uIiwicGF0aWVudElkIjoiMTIzNDUiLCJhcHBvaW50bWVudElkIjo0MiwiYXBwb2ludG1lbnRUeXBlSWQiOjEwMDcsIm9mZmljZUlkIjoic3ByaW5nX2hpbGwiLCJzdGFydCI6IjIwMjYtMDYtMDNUMDk6MDA6MDBaIiwiaWF0IjoxNzgwMzE1MjAwLCJleHAiOjE3ODAzMTYxMDB9.WvFiADITbw4NqG-dzzgy6falbBYnJA3z2rDLVessC1I" {
		t.Fatalf("cancellation token = %q, err = %v", cancellation, err)
	}
	reschedule, err := tokens.IssueRescheduleToken("12345", appointment)
	if err != nil || reschedule != "eyJ2IjoxLCJwdXJwb3NlIjoiYXBwb2ludG1lbnRfcmVzY2hlZHVsZSIsInBhdGllbnRJZCI6IjEyMzQ1IiwiYXBwb2ludG1lbnRJZCI6NDIsImFwcG9pbnRtZW50VHlwZUlkIjoxMDA3LCJvZmZpY2VJZCI6InNwcmluZ19oaWxsIiwic3RhcnQiOiIyMDI2LTA2LTAzVDA5OjAwOjAwWiIsImlhdCI6MTc4MDMxNTIwMCwiZXhwIjoxNzgwMzE2MTAwfQ.hdDYMjvvuhaybNlUgx_2o6BPtiG0n8Qb6OLVfAKFS10" {
		t.Fatalf("reschedule token = %q, err = %v", reschedule, err)
	}
	if _, err := tokens.verifyCancellation(reschedule, now); err == nil {
		t.Fatal("a reschedule token must not verify as a cancellation token")
	}
	if _, err := VerifySlotToken("fixture-secret", slot, now); err != nil {
		t.Fatalf("VerifySlotToken() error = %v", err)
	}
}
