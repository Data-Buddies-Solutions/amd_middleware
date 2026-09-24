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

// surname ignores a separately spoken generational suffix, without exposing a
// suffix field or changing the payer's returned last name.
func surname(s string) string {
	parts := strings.Fields(s)
	if len(parts) > 1 {
		switch name(parts[len(parts)-1]) {
		case "JR", "SR", "II", "III", "IV":
			parts = parts[:len(parts)-1]
		}
	}
	return name(strings.Join(parts, " "))
}

// Match permits one missing leading letter only with matching DOB, member ID,
// and surname. Other name differences remain reviewable.
func Match(expected, returned Person) MatchResult {
	first, last := name(expected.FirstName), name(expected.LastName)
	returnedFirst, returnedLast := name(returned.FirstName), name(returned.LastName)
	// Separate a middle name only when the payer explicitly returns it.
	if middle := name(returned.MiddleName); middle != "" {
		parts := strings.Fields(expected.FirstName)
		for i := 1; i < len(parts); i++ {
			if name(strings.Join(parts[:i], " ")) == returnedFirst && name(strings.Join(parts[i:], " ")) == middle {
				first = returnedFirst
				break
			}
		}
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
	returnedRunes, firstRunes := []rune(returnedFirst), []rune(first)
	missingInitial := len(firstRunes) >= 3 && len(returnedRunes) == len(firstRunes)+1 && string(returnedRunes[1:]) == first
	if expected.DateOfBirth == returned.DateOfBirth && sameID(expected.MemberID, returned.MemberID) && surname(expected.LastName) == surname(returned.LastName) && (first == returnedFirst || missingInitial) && (first != returnedFirst || last != returnedLast) {
		result.Status = "matched_with_name_correction"
		result.ReviewRequired = false
		result.Reasons = []string{}
		if missingInitial {
			result.Reasons = append(result.Reasons, "first_name_corrected")
		}
		if last != returnedLast {
			result.Reasons = append(result.Reasons, "last_name_normalized")
		}
		return result
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
