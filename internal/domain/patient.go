package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// NormalizeForLookup normalizes input strings for fuzzy map lookups.
// Strips punctuation (periods, commas), replaces slashes with spaces,
// collapses multiple spaces, lowercases, and trims whitespace.
func NormalizeForLookup(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "/", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

// StripDiacritics removes accent marks and diacritical characters from a string.
// e.g., "López Sánchez" → "Lopez Sanchez"
func StripDiacritics(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return result
}

// Patient represents a patient record.
type Patient struct {
	ID        string
	FirstName string
	LastName  string
	FullName  string // "LASTNAME,FIRSTNAME" format from AMD
	DOB       string // MM/DD/YYYY
	Phone     string
}

// StripPatientPrefix removes the "pat" prefix from patient IDs.
// AMD returns IDs like "pat45" but the booking API expects just "45".
func StripPatientPrefix(id string) string {
	return strings.TrimPrefix(id, "pat")
}

// NormalizeDOB converts various date formats to MM/DD/YYYY.
func NormalizeDOB(dob string) string {
	// Already in correct format
	if len(dob) == 10 && dob[2] == '/' && dob[5] == '/' {
		return dob
	}

	formats := []string{
		"2006-01-02",
		"01-02-2006",
		"1/2/2006",
		"01/02/2006",
		"January 2 2006",
		"January 2, 2006",
		"Jan 2 2006",
		"Jan 2, 2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dob); err == nil {
			return t.Format("01/02/2006")
		}
	}

	return dob
}

// FormatPhone normalizes a US phone number to (XXX)XXX-XXXX format.
// Strips all non-digit characters and drops a leading US country code.
func FormatPhone(phone string) string {
	var digits []byte
	for _, c := range phone {
		if c >= '0' && c <= '9' {
			digits = append(digits, byte(c))
		}
	}
	if len(digits) == 11 && digits[0] == '1' {
		digits = digits[1:]
	}
	if len(digits) == 10 {
		return fmt.Sprintf("(%s)%s-%s", string(digits[0:3]), string(digits[3:6]), string(digits[6:10]))
	}
	return phone
}

// NormalizeSex converts various sex inputs to AMD's expected format (M/F/U).
func NormalizeSex(sex string) string {
	switch strings.ToUpper(strings.TrimSpace(sex)) {
	case "M", "MALE":
		return "M"
	case "F", "FEMALE":
		return "F"
	default:
		return "U"
	}
}

// IsMinor returns true if the patient's DOB (MM/DD/YYYY) indicates they are under 18.
func IsMinor(dob string) bool {
	age, ok := AgeYears(dob)
	if !ok {
		return false
	}
	return age < 18
}

// AgeYears returns the patient's age in full years as of today.
func AgeYears(dob string) (int, bool) {
	return AgeYearsOn(dob, time.Now())
}

// AgeYearsOn returns the patient's age in full years on a specific date.
func AgeYearsOn(dob string, asOf time.Time) (int, bool) {
	t, err := time.Parse("01/02/2006", NormalizeDOB(dob))
	if err != nil {
		return 0, false
	}

	age := asOf.Year() - t.Year()
	birthdayThisYear := time.Date(asOf.Year(), t.Month(), t.Day(), 0, 0, 0, 0, asOf.Location())
	if asOf.Before(birthdayThisYear) {
		age--
	}
	if age < 0 {
		return 0, false
	}
	return age, true
}

// ParseFirstName extracts the first name from AMD's "LASTNAME,FIRSTNAME" format.
func ParseFirstName(fullName string) string {
	parts := strings.SplitN(fullName, ",", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return ""
}
