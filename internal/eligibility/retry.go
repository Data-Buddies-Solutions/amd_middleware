package eligibility

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const maxAttempts = 4

type Encounter struct {
	ServiceTypeCodes []string `json:"serviceTypeCodes"`
	DateOfService    string   `json:"dateOfService,omitempty"`
}

type Request struct {
	Payer      string    `json:"tradingPartnerServiceId"`
	Provider   Provider  `json:"provider"`
	Subscriber Person    `json:"subscriber"`
	Dependents []Person  `json:"dependents,omitempty"`
	Encounter  Encounter `json:"encounter"`
	SearchID   string    `json:"eligibilitySearchId,omitempty"`
}

type RecordedName struct {
	First  string `json:"firstName"`
	Last   string `json:"lastName"`
	Source string `json:"source"`
}

type Attempt struct {
	Label   string  `json:"label"`
	Request Request `json:"request"`
}

type RetryPlan struct {
	Reason   string    `json:"reason"`
	Attempts []Attempt `json:"attempts"`
}

// Fingerprint excludes the search-chain identifier and cosmetic name casing.
// Patient values, payer, provider, service and date remain part of the identity.
func Fingerprint(r Request) string {
	r.SearchID = ""
	r.Subscriber.FirstName = strings.ToUpper(r.Subscriber.FirstName)
	r.Subscriber.LastName = strings.ToUpper(r.Subscriber.LastName)
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// CanRetryNames excludes DOB, authorization, enrollment and transient failures.
func CanRetryNames(codes []string) bool {
	if len(codes) == 0 {
		return false
	}
	for _, code := range codes {
		if code != "72" && code != "73" && code != "75" {
			return false
		}
	}
	return true
}

// PlanRetries tries only complete recorded names and omission of a rejected member ID. It
// never generates edit-distance spellings or edits DOB/member IDs. Only a
// subscriber-shaped query is supported here; dependent recovery needs its own
// policyholder relationship context and is left for review.
func PlanRetries(base Request, names []RecordedName, prior []Request, errors []string, supported bool) RetryPlan {
	p := RetryPlan{Reason: "no_supported_recovery", Attempts: []Attempt{}}
	if !supported {
		p.Reason = "payer_unsupported"
		return p
	}
	if len(base.Dependents) > 0 {
		p.Reason = "dependent_review"
		return p
	}
	if len(errors) == 0 {
		p.Reason = "no_rejection"
		return p
	}
	if !CanRetryNames(errors) {
		p.Reason = "requires_data_or_payer_review"
		return p
	}
	seen := map[string]bool{}
	for _, r := range prior {
		seen[Fingerprint(r)] = true
	}
	// The base already produced the response being retried, even if a replay
	// omitted it from history. Count each prior send, including duplicates.
	used := len(prior)
	baseKey := Fingerprint(base)
	if !seen[baseKey] {
		used++
	}
	limit := maxAttempts - used
	if limit <= 0 {
		p.Reason = "attempt_limit"
		return p
	}
	seen[baseKey] = true
	add := func(label string, person Person) {
		if len(p.Attempts) >= limit {
			return
		}
		r := base
		r.Subscriber = person
		key := Fingerprint(r)
		if seen[key] {
			return
		}
		seen[key] = true
		p.Attempts = append(p.Attempts, Attempt{Label: label, Request: r})
	}
	type candidate struct {
		source string
		person Person
	}
	var candidates []candidate
	for _, n := range names {
		if n.Source != "chart" && n.Source != "intake" {
			continue
		}
		if name(n.First) == "" || name(n.Last) == "" {
			continue
		}
		person := base.Subscriber
		person.FirstName = n.First
		person.LastName = n.Last
		candidates = append(candidates, candidate{n.Source, person})
		add(n.Source+"_name", person)
	}
	// Only omit member ID when that field or the subscriber was rejected.
	omitMember := false
	for _, code := range errors {
		if code == "72" || code == "75" {
			omitMember = true
		}
	}
	if omitMember {
		candidates = append(candidates, candidate{"intake", base.Subscriber})
		for _, c := range candidates {
			withoutMember := c.person
			withoutMember.MemberID = ""
			add(c.source+"_without_member", withoutMember)
		}
	}
	if len(p.Attempts) > 0 {
		p.Reason = "recorded_name_recovery"
	} else {
		p.Reason = "previously_exhausted"
	}
	return p
}
