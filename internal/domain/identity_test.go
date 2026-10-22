package domain

import (
	"testing"
	"time"
)

func TestAFirstNameMatchesTheFirstGivenNameOfAHyphenatedChart(t *testing.T) {
	chart := ParseRecordName("SMITH,MARY-ANN")
	for _, spoken := range []string{"Mary", "mary", "Mary-Ann", "Mary Ann", "Maryann"} {
		if !chart.MatchesFirstName(NewPersonName(spoken)) {
			t.Errorf("%q must match SMITH,MARY-ANN", spoken)
		}
	}
	for _, spoken := range []string{"Ann", "Mar", "Marianne"} {
		if chart.MatchesFirstName(NewPersonName(spoken)) {
			t.Errorf("%q must not match SMITH,MARY-ANN", spoken)
		}
	}
}

func TestPatientIDsAreCanonicalDigits(t *testing.T) {
	for raw, want := range map[string]PatientID{"101": "101", "pat101": "101", " pat101 ": "101", "": "", "pat": ""} {
		if got, err := ParsePatientID(raw); err != nil || got != want {
			t.Errorf("ParsePatientID(%q) = %q, %v, want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"+101", "-5", "0101", "0", "1e3", "10 1", "abc", "patpat101"} {
		if got, err := ParsePatientID(raw); err == nil {
			t.Errorf("ParsePatientID(%q) = %q, want an error", raw, got)
		}
	}
}

func TestDOBsArePaddedLikeOtherIdentityFields(t *testing.T) {
	asOf := time.Date(2026, time.June, 1, 16, 0, 0, 0, time.UTC)
	for _, raw := range []string{" 01/15/1980", "01/15/1980 ", " 1980-01-15 "} {
		if got, err := ParseDOB(raw, asOf); err != nil || got != "01/15/1980" {
			t.Errorf("ParseDOB(%q) = %q, %v, want 01/15/1980", raw, got, err)
		}
	}
	if got, err := ParseDOB("   ", asOf); err != nil || got != "" {
		t.Errorf("ParseDOB(blank) = %q, %v, want empty", got, err)
	}
}
