package domain

import (
	"testing"
	"time"
)

func TestStripDiacritics(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"spanish accents", "López Sánchez", "Lopez Sanchez"},
		{"french accents", "René François", "Rene Francois"},
		{"german umlaut", "Müller", "Muller"},
		{"no accents", "Smith", "Smith"},
		{"mixed", "José García-López", "Jose Garcia-Lopez"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripDiacritics(tt.input)
			if got != tt.expected {
				t.Errorf("StripDiacritics(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestStripPatientPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"pat123", "123"},
		{"pat45", "45"},
		{"123", "123"},        // No prefix
		{"patient1", "ient1"}, // Only strips "pat"
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := StripPatientPrefix(tt.input)
			if got != tt.expected {
				t.Errorf("StripPatientPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

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

func TestNormalizeForLookup(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"lowercase and trim", "  Cigna  ", "cigna"},
		{"strips periods", "B.C.B.S.", "bcbs"},
		{"strips commas", "Blue Cross, Blue Shield", "blue cross blue shield"},
		{"replaces slashes with space", "Blue Cross/Blue Shield", "blue cross blue shield"},
		{"collapses multiple spaces", "blue   cross", "blue cross"},
		{"combined normalizations", " B.C.B.S. / of Florida ", "bcbs of florida"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeForLookup(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeForLookup(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLookupInsurance(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantID      string
		wantRouting RoutingRule
		wantFound   bool
	}{
		{"exact match lowercase", "humana medicare", "car308175", RoutingBachOnly, true},
		{"case insensitive", "HUMANA MEDICARE", "car308175", RoutingBachOnly, true},
		{"with whitespace", "  Aetna  ", "car40887", RoutingAll, true},
		{"all three default", "Florida Blue", "car40897", RoutingAll, true},
		{"bach + licht", "Tricare Prime", "car40921", RoutingBachLicht, true},
		{"not accepted", "Molina Marketplace", "car308175", RoutingNotAccepted, true},
		{"alias match", "Oscar", "car284233", RoutingBachLicht, true},
		{"alias shorthand", "Humana", "car308175", RoutingBachOnly, true},
		{"unknown carrier", "unknown", "", "", false},
		{"empty string", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, gotFound := LookupInsurance(tt.input)
			if gotFound != tt.wantFound {
				t.Errorf("LookupInsurance(%q) found = %v, want %v", tt.input, gotFound, tt.wantFound)
			}
			if gotFound {
				if entry.CarrierID != tt.wantID {
					t.Errorf("LookupInsurance(%q) carrierID = %q, want %q", tt.input, entry.CarrierID, tt.wantID)
				}
				if entry.Routing != tt.wantRouting {
					t.Errorf("LookupInsurance(%q) routing = %q, want %q", tt.input, entry.Routing, tt.wantRouting)
				}
			}
		})
	}
}

func TestLookupInsuranceForCoverage_RoutineVision(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantID    string
		wantFound bool
	}{
		{"top level VSP", "VSP", "car280695", true},
		{"misspelled Solstice", "Soltice", "car301652", true},
		{"VSP alias", "Lincoln Finacial", "car280695", true},
		{"EyeMed alias", "Humana", "car280684", true},
		{"Davis alias", "Florida Blue", "car280612", true},
		{"Spectera alias", "United Health Care", "car308790", true},
		{"iCare alias", "Simply Medcaid", "car40907", true},
		{"Alivi alias", "CarePlus", "car308796", true},
		{"medical not accepted becomes vision bucket", "Optimum", "car40907", true},
		{"unknown carrier", "unknown", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, gotFound := LookupInsuranceForCoverage(tt.input, InsuranceModeVision)
			if gotFound != tt.wantFound {
				t.Errorf("LookupInsuranceForCoverage(%q, vision) found = %v, want %v", tt.input, gotFound, tt.wantFound)
			}
			if gotFound {
				if entry.CarrierID != tt.wantID {
					t.Errorf("LookupInsuranceForCoverage(%q, vision) carrierID = %q, want %q", tt.input, entry.CarrierID, tt.wantID)
				}
				if entry.Routing != RoutingOpticalOnly {
					t.Errorf("LookupInsuranceForCoverage(%q, vision) routing = %q, want %q", tt.input, entry.Routing, RoutingOpticalOnly)
				}
			}
		})
	}
}

func TestInsuranceModeForCoverage(t *testing.T) {
	tests := []struct {
		input string
		want  InsuranceMode
	}{
		{"routine_vision", InsuranceModeVision},
		{"routine vision", InsuranceModeVision},
		{"optical_only", InsuranceModeVision},
		{"", InsuranceModeMedical},
		{"medical", InsuranceModeMedical},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := InsuranceModeForCoverage(tt.input); got != tt.want {
				t.Errorf("InsuranceModeForCoverage(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatPhone(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"10 digits raw", "5551234567", "(555)123-4567"},
		{"with dashes", "555-123-4567", "(555)123-4567"},
		{"with parens and dash", "(555)123-4567", "(555)123-4567"},
		{"with spaces", "555 123 4567", "(555)123-4567"},
		{"with dots", "555.123.4567", "(555)123-4567"},
		{"with country code", "+15551234567", "+15551234567"}, // 11 digits, returned as-is
		{"too short", "555123", "555123"},
		{"empty string", "", ""},
		{"mixed chars", "call 555-123-4567 now", "(555)123-4567"}, // 10 digits extracted
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatPhone(tt.input)
			if got != tt.expected {
				t.Errorf("FormatPhone(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeSex(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"M", "M"},
		{"m", "M"},
		{"Male", "M"},
		{"MALE", "M"},
		{"F", "F"},
		{"f", "F"},
		{"Female", "F"},
		{"FEMALE", "F"},
		{"U", "U"},
		{"Other", "U"},
		{"", "U"},
		{"  male  ", "M"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeSex(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeSex(%q) = %q, want %q", tt.input, got, tt.expected)
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

func TestParseFirstName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"SMITH,JOHN", "JOHN"},
		{"DOE,JANE MARIE", "JANE MARIE"},
		{"SMITH, JOHN", "JOHN"}, // With space after comma
		{"SMITH", ""},           // No comma
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
