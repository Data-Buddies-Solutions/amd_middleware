package domain

import "time"

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
	InsuranceNote       string
	DOB                 string
	InsuranceStateKnown bool
}

type PatientAppointment struct {
	ID                int
	ColumnID          int
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
	Offices   []*OfficeConfig
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
	ProfileID int
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
