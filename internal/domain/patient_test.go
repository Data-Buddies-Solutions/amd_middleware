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

var testNow = time.Date(2026, time.June, 1, 16, 0, 0, 0, time.UTC)

func TestIsMinor(t *testing.T) {
	tests := []struct {
		name     string
		dob      string
		expected bool
	}{
		{"adult born 30 years ago", testNow.AddDate(-30, 0, 0).Format("01/02/2006"), false},
		{"child born 10 years ago", testNow.AddDate(-10, 0, 0).Format("01/02/2006"), true},
		{"turns 18 tomorrow", testNow.AddDate(-18, 0, 1).Format("01/02/2006"), true},
		{"exactly 18 today", testNow.AddDate(-18, 0, 0).Format("01/02/2006"), false},
		{"turned 18 yesterday", testNow.AddDate(-18, 0, -1).Format("01/02/2006"), false},
		{"invalid format returns false", "not-a-date", false},
		{"empty string returns false", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMinor(tt.dob, testNow)
			if got != tt.expected {
				t.Errorf("isMinor(%q, testNow) = %v, want %v", tt.dob, got, tt.expected)
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

func TestParseRecordName(t *testing.T) {
	tests := []struct {
		full, last, given, first string
	}{
		{"SMITH,JOHN", "SMITH", "JOHN", "JOHN"},
		{"DOE,JANE MARIE", "DOE", "JANE MARIE", "JANE"},
		{"SMITH, JOHN ", "SMITH", "JOHN", "JOHN"},
		{"SMITH", "SMITH", "", ""},
		{"SMITH,", "SMITH", "", ""},
		{",JANE", "", "JANE", "JANE"},
		{"", "", "", ""},
	}
	for _, tt := range tests {
		name := ParseRecordName(tt.full)
		if name.Last != tt.last || name.Given != tt.given || name.First() != tt.first {
			t.Errorf("ParseRecordName(%q) = %+v first %q, want %q %q %q", tt.full, name, name.First(), tt.last, tt.given, tt.first)
		}
	}
}

func TestParsePatientID(t *testing.T) {
	tests := []struct {
		raw   string
		want  PatientID
		valid bool
	}{
		{"123", "123", true},
		{"pat123", "123", true},
		{" pat123 ", "123", true},
		{"", "", true},
		{"pat", "", true},
		{"PAT123", "", false},
		{"abc", "", false},
	}
	for _, tt := range tests {
		got, err := ParsePatientID(tt.raw)
		if got != tt.want || (err == nil) != tt.valid {
			t.Errorf("ParsePatientID(%q) = %q, %v; want %q valid=%v", tt.raw, got, err, tt.want, tt.valid)
		}
	}
}

func TestParseDOB(t *testing.T) {
	tests := []struct {
		raw   string
		want  DOB
		valid bool
	}{
		{"01/15/1980", "01/15/1980", true},
		{"1980-01-15", "01/15/1980", true},
		{"01-15-1980", "01/15/1980", true},
		{"1/5/1980", "01/05/1980", true},
		{"January 15, 1980", "01/15/1980", true},
		{"Jan 15 1980", "01/15/1980", true},
		{"06/01/2026", "06/01/2026", true},
		{"", "", true},
		{"06/02/2026", "", false},
		{"13/45/1980", "", false},
		{" 01/15/1980", "", false},
		{"15.01.1980", "", false},
	}
	for _, tt := range tests {
		got, err := ParseDOB(tt.raw, testNow)
		if got != tt.want || (err == nil) != tt.valid {
			t.Errorf("ParseDOB(%q) = %q, %v; want %q valid=%v", tt.raw, got, err, tt.want, tt.valid)
		}
	}
}
