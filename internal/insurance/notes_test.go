package insurance

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

type noteRule struct {
	name    string
	pattern *regexp.Regexp
	encoded func(p plan) bool
}

var noteRules = []noteRule{
	{"staff check", regexp.MustCompile(`(?i)check w/? ?eligibility|call to confirm|call .*number`), func(p plan) bool { return false }},
	{"Dr. Austin Bach only", regexp.MustCompile(`(?i)dr\.? ?(a\.? ?|austin )?bach only|only dr\.? ?(a\.? ?|austin )?bach`), func(p plan) bool {
		for doctor, value := range p.Doctors {
			if value == "yes" && doctor != "Dr. Austin Bach" {
				return false
			}
		}
		return true
	}},
	{"PCP referral", regexp.MustCompile(`(?i)(requires?|needs) (simple )?referrals?`), func(p plan) bool { return hasRequirement(p, "pcp_referral") }},
	{"prior authorization", regexp.MustCompile(`(?i)(requires?|needs) prior[- ]?auth`), func(p plan) bool { return hasRequirement(p, "prior_authorization") }},
	{"office limit", regexp.MustCompile(`(?i)only in (miami-?dade|broward)|hollywood office only`), func(p plan) bool { return len(p.OnlyOffices) > 0 }},
}

func TestEveryRuleInANoteIsEncoded(t *testing.T) {
	for _, list := range catalog {
		for _, p := range list.Plans {
			for _, rule := range noteRules {
				if !statesRule(p.Note, rule.pattern) || rule.encoded(p) || hasRequirement(p, "staff_verify") {
					continue
				}
				t.Errorf("%s %s: note says %q (%s) but the data doesn't; encode it or add staff_verify", list.Table, p.ID, p.Note, rule.name)
			}
		}
	}
}

func TestNoteRulesIgnoreNegations(t *testing.T) {
	referral := noteRules[2].pattern
	for _, note := range []string{"Does Not Require Referral", "NO auths/referrals needed", "Does not require Prior Auth/Referral"} {
		if statesRule(note, referral) {
			t.Errorf("%q read as a referral rule", note)
		}
	}
	if !statesRule("REQUIRES SIMPLE REFERRAL FROM THE PCP OFFICE", referral) {
		t.Error("referral rule not found")
	}
}

func statesRule(note string, pattern *regexp.Regexp) bool {
	for _, match := range pattern.FindAllStringIndex(note, -1) {
		before := strings.ToLower(note[max(0, match[0]-5):match[0]])
		if !strings.Contains(before, "not ") && !strings.Contains(before, "no ") {
			return true
		}
	}
	return false
}

func hasRequirement(p plan, kind string) bool {
	return slices.ContainsFunc(p.Requirements, func(r planRequirement) bool { return r.Kind == kind })
}
