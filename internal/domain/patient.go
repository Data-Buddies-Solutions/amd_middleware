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

func StripDiacritics(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return result
}

type Patient struct {
	ID        string
	FirstName string
	LastName  string
	FullName  string
	DOB       string
	Phone     string
}

type PatientSearch struct {
	Phone     string
	FirstName string
	LastName  string
}

type PatientCandidateRead struct {
	Patients []Patient
	Complete bool
}

type PatientDemographics struct {
	FullName            string
	CarrierName         string
	CarrierID           string
	InsPlanID           string
	RespPartyID         string
	SubscriberNum       string
	DOB                 string
	InsuranceStateKnown bool
}

type PatientAppointment struct {
	ID                int
	Start             time.Time
	Provider          string
	Type              string
	AppointmentTypeID int
	Facility          string
	OfficeID          string
	Office            string
}

type PatientAppointmentsQuery struct {
	PatientID string
	OfficeIDs []string
}

func ValidateOptionalDOB(dob string) error {
	if dob == "" {
		return nil
	}
	if _, ok := AgeYears(dob); !ok {
		return fmt.Errorf("dob must be a valid date")
	}
	return nil
}

type PatientCreate struct {
	FirstName string
	LastName  string
	DOB       string
	Phone     string
	Email     string
	Street    string
	AptSuite  string
	City      string
	State     string
	Zip       string
	Sex       string
	SSN       string
	OfficeID  string
}

type CreatedPatient struct {
	ID          string
	RespPartyID string
	Name        string
}

type PatientInsurance struct {
	PatientID     string
	RespPartyID   string
	CarrierID     string
	SubscriberNum string
}

type PatientInsuranceEnd struct {
	PatientID string
	InsPlanID string
}

func StripPatientPrefix(id string) string {
	return strings.TrimPrefix(id, "pat")
}

func NormalizeDOB(dob string) string {
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

func IsMinor(dob string) bool {
	age, ok := AgeYears(dob)
	if !ok {
		return false
	}
	return age < 18
}

func AgeYears(dob string) (int, bool) {
	return AgeYearsOn(dob, time.Now())
}

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

func ParseFirstName(fullName string) string {
	parts := strings.SplitN(fullName, ",", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return ""
}
