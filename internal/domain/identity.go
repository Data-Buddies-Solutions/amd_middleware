package domain

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

type PatientID string

var errPatientIDNotNumeric = errors.New("patientId must be numeric")

func ParsePatientID(raw string) (PatientID, error) {
	id := StripPatientPrefix(raw)
	if id == "" {
		return "", nil
	}
	if _, err := strconv.Atoi(id); err != nil {
		return "", errPatientIDNotNumeric
	}
	return PatientID(id), nil
}

func (id PatientID) Int() int {
	n, _ := strconv.Atoi(string(id))
	return n
}

func StripPatientPrefix(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "pat")
}

type Phone string

var errPhoneDigits = errors.New("phone must contain at least one digit")

func ParsePhone(raw string) (Phone, error) {
	if raw == "" {
		return "", nil
	}
	digits := stripToDigits(raw)
	if len(digits) == 11 && digits[0] == '1' {
		digits = digits[1:]
	}
	if digits == "" {
		return "", errPhoneDigits
	}
	return Phone(digits), nil
}

func (p Phone) Matches(recorded string) bool {
	other, err := ParsePhone(recorded)
	return err == nil && p != "" && other == p
}

type DOB string

const dobLayout = "01/02/2006"

var errInvalidDOB = errors.New("dob must be a valid date")

var dobLayouts = []string{
	dobLayout,
	"2006-01-02",
	"01-02-2006",
	"1/2/2006",
	"January 2 2006",
	"January 2, 2006",
	"Jan 2 2006",
	"Jan 2, 2006",
}

func ParseDOB(raw string, asOf time.Time) (DOB, error) {
	if raw == "" {
		return "", nil
	}
	born, ok := birthDate(raw)
	if !ok || yearsBetween(born, asOf) < 0 {
		return "", errInvalidDOB
	}
	return DOB(born.Format(dobLayout)), nil
}

func (d DOB) Matches(recorded string) bool {
	born, ok := birthDate(recorded)
	return ok && d != "" && DOB(born.Format(dobLayout)) == d
}

func AgeYearsOn(dob string, asOf time.Time) (int, bool) {
	born, ok := birthDate(dob)
	if !ok {
		return 0, false
	}
	age := yearsBetween(born, asOf)
	if age < 0 {
		return 0, false
	}
	return age, true
}

func birthDate(raw string) (time.Time, bool) {
	for _, layout := range dobLayouts {
		if born, err := time.Parse(layout, raw); err == nil {
			return born, true
		}
	}
	return time.Time{}, false
}

func yearsBetween(born, asOf time.Time) int {
	asOf = asOf.In(easternLocation)
	age := asOf.Year() - born.Year()
	if asOf.Before(time.Date(asOf.Year(), born.Month(), born.Day(), 0, 0, 0, 0, asOf.Location())) {
		age--
	}
	return age
}

type PersonName string

func NewPersonName(raw string) PersonName {
	return PersonName(StripDiacritics(raw))
}

func StripDiacritics(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return result
}

func nameKey(name string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		return -1
	}, strings.ToLower(StripDiacritics(name)))
}

func (n PersonName) Key() string {
	return nameKey(string(n))
}

type RecordName struct {
	Last  string
	Given string
}

func ParseRecordName(full string) RecordName {
	last, given, found := strings.Cut(full, ",")
	if !found {
		return RecordName{Last: strings.TrimSpace(full)}
	}
	return RecordName{Last: strings.TrimSpace(last), Given: strings.TrimSpace(given)}
}

func (n RecordName) First() string {
	if names := strings.Fields(n.Given); len(names) > 0 {
		return names[0]
	}
	return ""
}

func (n RecordName) MatchesFirstName(spoken PersonName) bool {
	key := spoken.Key()
	return key == nameKey(n.First()) || key == nameKey(n.Given)
}

func (n RecordName) GivenStartsWith(spoken PersonName) bool {
	return strings.HasPrefix(strings.ToUpper(StripDiacritics(n.Given)), strings.ToUpper(string(spoken)))
}

func (n RecordName) WrittenAs(first, last PersonName) bool {
	return strings.EqualFold(StripDiacritics(n.Given), string(first)) &&
		strings.EqualFold(StripDiacritics(n.Last), string(last))
}
