package insurance

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type InsuranceCatalog struct {
	Carriers []CatalogCarrier `json:"carriers"`
	Regions  []CatalogRegion  `json:"regions"`
}

type CatalogCarrier struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type CatalogRegion struct {
	ID     string         `json:"id"`
	Plans  []CatalogPlan  `json:"plans"`
	Tables []CatalogTable `json:"tables"`
}

type CatalogPlan struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Family    string   `json:"family"`
	Coverage  string   `json:"coverage"`
	CarrierID string   `json:"carrierId"`
	Aliases   []string `json:"aliases"`
}

type CatalogTable struct {
	ID      string       `json:"id"`
	Offices []string     `json:"offices"`
	Doctors []string     `json:"doctors"`
	Rows    []CatalogRow `json:"rows"`
}

type CatalogRow struct {
	PlanID        string            `json:"planId"`
	Doctors       map[string]string `json:"doctors"`
	BeforeBooking BeforeBooking     `json:"beforeBooking"`
	OnlyOffices   []string          `json:"onlyOffices"`
	Notice        string            `json:"notice"`
}

type BeforeBooking string

const (
	BeforeBookingNone               BeforeBooking = "none"
	BeforeBookingPCPReferral        BeforeBooking = "pcp_referral"
	BeforeBookingPriorAuthorization BeforeBooking = "prior_authorization"
	BeforeBookingStaffVerify        BeforeBooking = "staff_verify"
)

var blockedBeforeBooking = map[BeforeBooking]string{
	BeforeBookingPCPReferral:        answerReferral,
	BeforeBookingPriorAuthorization: answerPriorAuth,
	BeforeBookingStaffVerify:        answerStaffVerify,
}

var selfPayPlanIDs = map[string]string{
	"medical":        "self-pay-medical",
	"routine_vision": "self-pay-routine-vision",
}

type CatalogError struct {
	Region  string      `json:"region"`
	Table   string      `json:"table"`
	PlanID  string      `json:"planId"`
	Rule    CatalogRule `json:"rule"`
	Message string      `json:"message"`
}

type CatalogRule string

func ValidateCatalog(candidate InsuranceCatalog) []CatalogError {
	_, errs, drift := parseCatalog(candidate)
	return append(errs, drift...)
}

func DecodeCatalog(body []byte) (InsuranceCatalog, error) {
	var catalog InsuranceCatalog
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return InsuranceCatalog{}, fmt.Errorf("not a v2 catalog: %v", err)
	}
	return catalog, nil
}
