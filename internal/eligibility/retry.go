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

// NextRetry chooses one unused recorded-name request, then member-ID omission
// for codes 72/75. It never changes DOB, invents names, or retries dependents.
func NextRetry(base Request, names []RecordedName, prior []Request, codes []string, supported bool) (*Request, string) {
	if !supported {
		return nil, "payer_unsupported"
	}
	if len(base.Dependents) > 0 {
		return nil, "dependent_review"
	}
	if len(codes) == 0 {
		return nil, "no_rejection"
	}
	if !CanRetryNames(codes) {
		return nil, "requires_data_or_payer_review"
	}
	seen := map[string]bool{}
	for _, request := range prior {
		seen[Fingerprint(request)] = true
	}
	// The base produced the rejection even when a saved history omitted it.
	used := len(prior)
	baseKey := Fingerprint(base)
	if !seen[baseKey] {
		used++
	}
	if used >= maxAttempts {
		return nil, "attempt_limit"
	}
	seen[baseKey] = true
	omitAllowed := false
	for _, code := range codes {
		if code == "72" || code == "75" {
			omitAllowed = true
		}
	}
	// Prefer every complete recorded name before omitting a rejected member ID.
	for _, omitMember := range []bool{false, true} {
		if omitMember && !omitAllowed {
			break
		}
		for i := 0; i <= len(names); i++ {
			request := base
			if i < len(names) {
				n := names[i]
				if (n.Source != "chart" && n.Source != "intake") || name(n.First) == "" || name(n.Last) == "" {
					continue
				}
				request.Subscriber.FirstName, request.Subscriber.LastName = n.First, n.Last
			} else if !omitMember {
				continue
			}
			if omitMember {
				request.Subscriber.MemberID = ""
			}
			if !seen[Fingerprint(request)] {
				return &request, "recorded_name_recovery"
			}
		}
	}
	return nil, "previously_exhausted"
}
