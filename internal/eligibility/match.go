// Package eligibility checks general plan activity and preserves identity uncertainty.
package eligibility

import (
	"strings"
	"time"
	"unicode"
)

type Person struct {
	FirstName   string `json:"firstName,omitempty"`
	MiddleName  string `json:"middleName,omitempty"`
	LastName    string `json:"lastName,omitempty"`
	DateOfBirth string `json:"dateOfBirth,omitempty"`
	MemberID    string `json:"memberId,omitempty"`
}

// MatchResult reports exact identity agreement or the reason staff must review.
type MatchResult struct {
	Status         string   `json:"status"`
	ReviewRequired bool     `json:"reviewRequired"`
	Reasons        []string `json:"reasons"`
}

func name(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return unicode.ToUpper(r)
		}
		return -1
	}, s)
}

func identifier(s string) string { return strings.ToUpper(strings.Join(strings.Fields(s), "")) }

func sameID(a, b string) bool { return identifier(a) != "" && identifier(a) == identifier(b) }

func validDOB(s string) bool { _, err := time.Parse("20060102", s); return err == nil }

// Match compares names and DOB only. Member-ID changes remain visible but do
// not change this name/DOB assessment. A matching member ID cannot turn a
// different name into a verified identity.
func Match(expected, returned Person) MatchResult {
	first, last := name(expected.FirstName), name(expected.LastName)
	returnedFirst, returnedLast := name(returned.FirstName), name(returned.LastName)
	// Separate a middle name only when the payer explicitly returns it.
	if middle := name(returned.MiddleName); middle != "" && first == returnedFirst+middle {
		first = returnedFirst
	}
	result := MatchResult{Status: "insufficient_data", ReviewRequired: true, Reasons: []string{}}
	if first == "" || last == "" || returnedFirst == "" || returnedLast == "" || !validDOB(expected.DateOfBirth) || !validDOB(returned.DateOfBirth) {
		result.Reasons = append(result.Reasons, "missing_or_invalid_identity")
		return result
	}
	if expected.DateOfBirth != returned.DateOfBirth {
		result.Reasons = append(result.Reasons, "dob_conflict")
	}
	if first != returnedFirst {
		result.Reasons = append(result.Reasons, "first_name_conflict")
	}
	if last != returnedLast {
		result.Reasons = append(result.Reasons, "last_name_conflict")
	}
	if len(result.Reasons) > 0 {
		result.Status = "identity_conflict"
		return result
	}
	result.Status = "exact_name_dob"
	result.ReviewRequired = false
	if expected.MemberID != "" && returned.MemberID != "" && !sameID(expected.MemberID, returned.MemberID) {
		result.Reasons = append(result.Reasons, "member_id_changed")
	}
	return result
}
