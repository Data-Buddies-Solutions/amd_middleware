package domain

import (
	"fmt"
	"time"
)

type Patient struct {
	ID       string
	FullName string
	DOB      string
	Phone    string
}

func (p Patient) Name() RecordName {
	return ParseRecordName(p.FullName)
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

func ValidateOptionalDOB(dob string, asOf time.Time) error {
	if dob == "" {
		return nil
	}
	if _, ok := AgeYearsOn(dob, asOf); !ok {
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

func isMinor(dob string, asOf time.Time) bool {
	age, ok := AgeYearsOn(dob, asOf)
	if !ok {
		return false
	}
	return age < 18
}
