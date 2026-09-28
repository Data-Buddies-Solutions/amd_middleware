package domain

import (
	"testing"
	"time"
)

func TestNormalizeDOB(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"already correct format", "01/15/1980", "01/15/1980"},
		{"ISO format", "1980-01-15", "01/15/1980"},
		{"dash format", "01-15-1980", "01/15/1980"},
		{"single digit month/day", "1/5/1980", "01/05/1980"},
		{"full month name", "January 15 1980", "01/15/1980"},
		{"full month with comma", "January 15, 1980", "01/15/1980"},
		{"short month name", "Jan 15 1980", "01/15/1980"},
		{"short month with comma", "Jan 15, 1980", "01/15/1980"},
		{"unknown format returns as-is", "15.01.1980", "15.01.1980"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeDOB(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeDOB(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsMinor(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		dob      string
		expected bool
	}{
		{"adult born 30 years ago", now.AddDate(-30, 0, 0).Format("01/02/2006"), false},
		{"child born 10 years ago", now.AddDate(-10, 0, 0).Format("01/02/2006"), true},
		{"turns 18 tomorrow", now.AddDate(-18, 0, 1).Format("01/02/2006"), true},
		{"exactly 18 today", now.AddDate(-18, 0, 0).Format("01/02/2006"), false},
		{"turned 18 yesterday", now.AddDate(-18, 0, -1).Format("01/02/2006"), false},
		{"invalid format returns false", "not-a-date", false},
		{"empty string returns false", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMinor(tt.dob)
			if got != tt.expected {
				t.Errorf("IsMinor(%q) = %v, want %v", tt.dob, got, tt.expected)
			}
		})
	}
}

func TestAgeYearsOn(t *testing.T) {
	asOf := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		dob  string
		age  int
		ok   bool
	}{
		{"birthday already passed", "05/13/2019", 7, true},
		{"birthday today", "05/14/2019", 7, true},
		{"birthday tomorrow", "05/15/2019", 6, true},
		{"iso date is normalized", "2019-05-14", 7, true},
		{"invalid", "not-a-date", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			age, ok := AgeYearsOn(tt.dob, asOf)
			if ok != tt.ok || age != tt.age {
				t.Fatalf("AgeYearsOn(%q) = %d, %v; want %d, %v", tt.dob, age, ok, tt.age, tt.ok)
			}
		})
	}
}

func TestParseFirstName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"SMITH,JOHN", "JOHN"},
		{"DOE,JANE MARIE", "JANE MARIE"},
		{"SMITH, JOHN", "JOHN"},
		{"SMITH", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseFirstName(tt.input)
			if got != tt.expected {
				t.Errorf("ParseFirstName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
