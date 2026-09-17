// Package eligibility checks general plan activity and preserves identity uncertainty.
package eligibility

import (
	"strings"
	"time"
	"unicode"
)

type Address struct {
	Address1   string `json:"address1,omitempty"`
	Address2   string `json:"address2,omitempty"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
}

type Person struct {
	FirstName   string  `json:"firstName,omitempty"`
	MiddleName  string  `json:"middleName,omitempty"`
	LastName    string  `json:"lastName,omitempty"`
	DateOfBirth string  `json:"dateOfBirth,omitempty"`
	MemberID    string  `json:"memberId,omitempty"`
	Address     Address `json:"address,omitzero"`
}

// MatchResult is an explanation, not a calibrated probability. Even corroborated
// variants require review until independently validated against confirmed identities.
type MatchResult struct {
	Status          string   `json:"status"`
	ReviewRequired  bool     `json:"reviewRequired"`
	FirstDistance   int      `json:"firstDistance"`
	LastDistance    int      `json:"lastDistance"`
	DOBMatch        bool     `json:"dobMatch"`
	MemberMatch     bool     `json:"memberMatch"`
	PolicyMatch     bool     `json:"policyMatch"`
	AddressMatch    bool     `json:"addressMatch"`
	AddressConflict bool     `json:"addressConflict"`
	Reasons         []string `json:"reasons"`
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

func distance(a, b string) int {
	x, y := []rune(a), []rune(b)
	prev := make([]int, len(y)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, left := range x {
		next := make([]int, len(y)+1)
		next[0] = i + 1
		for j, right := range y {
			cost := 0
			if left != right {
				cost = 1
			}
			next[j+1] = min(next[j]+1, prev[j+1]+1, prev[j]+cost)
		}
		prev = next
	}
	return prev[len(y)]
}

var streetWords = map[string]string{"STREET": "ST", "DRIVE": "DR", "ROAD": "RD", "AVENUE": "AVE", "COURT": "CT", "LANE": "LN", "BOULEVARD": "BLVD", "PLACE": "PL", "CIRCLE": "CIR", "NORTH": "N", "SOUTH": "S", "EAST": "E", "WEST": "W"}

func street(s string) string {
	words := strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToUpper(r)
		}
		return ' '
	}, s))
	for i, w := range words {
		if replacement, ok := streetWords[w]; ok {
			words[i] = replacement
		}
	}
	return strings.Join(words, " ")
}

func zip(s string) string {
	if len(s) < 5 {
		return ""
	}
	for _, r := range s[:5] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s[:5]
}

func addressEvidence(a, b Address) (bool, bool) {
	as, bs := street(a.Address1), street(b.Address1)
	az, bz := zip(a.PostalCode), zip(b.PostalCode)
	stateA, stateB := strings.ToUpper(a.State), strings.ToUpper(b.State)
	conflict := (as != "" && bs != "" && as != bs) || (az != "" && bz != "" && az != bz) || (stateA != "" && stateB != "" && stateA != stateB)
	// Apartment differences matter. Missing apartment data cannot corroborate it.
	units := street(a.Address2) == street(b.Address2)
	if street(a.Address2) != "" && street(b.Address2) != "" && !units {
		conflict = true
	}
	match := as != "" && as == bs && az != "" && az == bz && len(stateA) == 2 && stateA == stateB && units
	return match, conflict
}

// Match compares the returned patient, not a different family member. Policy IDs
// corroborate a household only; a dependent's own member ID may be absent.
func Match(expected, returned Person, returnedPolicyID string) MatchResult {
	f, l := name(expected.FirstName), name(expected.LastName)
	rf, rl := name(returned.FirstName), name(returned.LastName)
	// Only separate a middle name when the response explicitly supplies that field.
	if middle := name(returned.MiddleName); middle != "" && f == rf+middle {
		f = rf
	}
	r := MatchResult{Status: "insufficient_data", ReviewRequired: true, FirstDistance: distance(f, rf), LastDistance: distance(l, rl), Reasons: []string{}}
	r.DOBMatch = validDOB(expected.DateOfBirth) && expected.DateOfBirth == returned.DateOfBirth
	r.MemberMatch = sameID(expected.MemberID, returned.MemberID)
	r.PolicyMatch = sameID(expected.MemberID, returnedPolicyID)
	r.AddressMatch, r.AddressConflict = addressEvidence(expected.Address, returned.Address)
	if f == "" || l == "" || rf == "" || rl == "" || !validDOB(expected.DateOfBirth) || !validDOB(returned.DateOfBirth) {
		r.Reasons = append(r.Reasons, "missing_or_invalid_identity")
		return r
	}
	if !r.DOBMatch {
		r.Status = "identity_conflict"
		r.Reasons = append(r.Reasons, "dob_conflict")
		return r
	}
	if r.FirstDistance == 0 && r.LastDistance == 0 {
		r.Status = "exact_name_dob"
		r.ReviewRequired = false
		if r.AddressConflict {
			r.Reasons = append(r.Reasons, "address_conflict")
		}
		if expected.MemberID != "" && returned.MemberID != "" && !r.MemberMatch {
			r.Reasons = append(r.Reasons, "member_id_changed")
		}
		return r
	}
	if r.FirstDistance+r.LastDistance == 1 {
		r.Status = "near_name"
		r.Reasons = append(r.Reasons, "one_character_difference")
		if len([]rune(f)) <= 3 && r.FirstDistance != 0 {
			r.Reasons = append(r.Reasons, "short_first_name")
		}
		if r.AddressMatch && (r.MemberMatch || r.PolicyMatch) {
			r.Status = "corroborated_variant"
		}
		if r.AddressConflict {
			r.Reasons = append(r.Reasons, "address_conflict")
		}
		return r
	}
	r.Status = "identity_conflict"
	r.Reasons = append(r.Reasons, "multiple_name_differences")
	return r
}
