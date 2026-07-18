package domain

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultSameStartCapacity = 1

// SchedulingPolicy owns the scheduling decisions for one office.
type SchedulingPolicy struct {
	office *OfficeConfig
}

func NewSchedulingPolicy(office *OfficeConfig) SchedulingPolicy {
	return SchedulingPolicy{office: office}
}

// SchedulingRouting applies the office's pediatric routing rule without changing
// explicitly rejected or optical-only lanes.
func (p SchedulingPolicy) SchedulingRouting(routing RoutingRule, dob string) RoutingRule {
	if routing == RoutingNotAccepted || routing == RoutingOpticalOnly {
		return routing
	}
	if p.office != nil && IsMinor(dob) {
		return p.office.PediatricRouting
	}
	return routing
}

// PatientRouting preserves the office's patient-resolution rule: every accepted
// minor is reported on the pediatric lane, including optical coverage.
func (p SchedulingPolicy) PatientRouting(routing RoutingRule, dob string) RoutingRule {
	if routing != RoutingNotAccepted && p.office != nil && IsMinor(dob) {
		return p.office.PediatricRouting
	}
	return routing
}

func (p SchedulingPolicy) SupportsRouting(routing RoutingRule) bool {
	return p.office != nil && len(p.office.ColumnsForRouting(routing)) > 0
}

func (p SchedulingPolicy) SupportsMedical() bool {
	return p.SupportsRouting(RoutingAll) ||
		p.SupportsRouting(RoutingBachOnly) ||
		p.SupportsRouting(RoutingBachLicht)
}

func (p SchedulingPolicy) ProviderNames(routing RoutingRule, dob string) []string {
	if p.office == nil {
		return nil
	}
	return p.office.ProvidersForRoutingAndDOB(routing, dob)
}

// AllowedAppointmentTypeIDs returns the canonical appointment types accepted by
// this office, routing lane, and patient DOB.
func (p SchedulingPolicy) AllowedAppointmentTypeIDs(routing RoutingRule, dob string) []int {
	if p.office == nil {
		return nil
	}

	typeIDs := make([]int, 0, len(DefaultAppointmentTypeColors))
	for typeID := range DefaultAppointmentTypeColors {
		if p.office.AllowsAppointmentType(typeID, routing) && appointmentTypeMatchesDOB(typeID, dob) {
			typeIDs = append(typeIDs, typeID)
		}
	}
	sort.Ints(typeIDs)
	return typeIDs
}

func appointmentTypeMatchesDOB(typeID int, dob string) bool {
	age, ok := AgeYears(dob)
	if !ok {
		return true
	}

	switch typeID {
	case 1004, 1005, 4244, 4245:
		return age < 18
	case 1006, 1007, 1010, 3364:
		return age >= 18
	default:
		return true
	}
}

// EligibleColumns applies office, routing, DOB, and provider eligibility in one pass.
func (p SchedulingPolicy) EligibleColumns(columns []SchedulerColumn, profiles map[string]SchedulerProfile, routing RoutingRule, dob, requestedProvider string) []SchedulerColumn {
	if p.office == nil {
		return nil
	}

	routingColumns := p.office.ColumnsForRouting(routing)
	if routingColumns == nil {
		return nil
	}

	eligible := make([]SchedulerColumn, 0, len(columns))
	for _, column := range columns {
		if column.FacilityID != p.office.FacilityID || !routingColumns[column.ID] || !p.office.ColumnAllowsDOB(column.ID, dob) {
			continue
		}
		if requestedProvider != "" && !p.matchesProvider(column, profiles[column.ProfileID], requestedProvider) {
			continue
		}
		eligible = append(eligible, column)
	}
	return eligible
}

func (p SchedulingPolicy) matchesProvider(column SchedulerColumn, profile SchedulerProfile, requested string) bool {
	needle := strings.ToUpper(NormalizeForLookup(requested))
	if needle == "" {
		return true
	}

	candidates := []string{profile.Name, column.Name}
	if officeColumn, ok := p.office.Columns[column.ID]; ok {
		candidates = append(candidates, officeColumn.DisplayName, officeColumn.ShortName)
	}
	for _, candidate := range candidates {
		if strings.Contains(strings.ToUpper(NormalizeForLookup(candidate)), needle) {
			return true
		}
	}
	return false
}

type BookingPolicyRequest struct {
	ColumnID          int
	ProfileID         int
	AppointmentTypeID int
	Routing           RoutingRule
	DOB               string
	Intent            AppointmentIntent
}

type BookingPolicyDecision struct {
	Routing           RoutingRule
	AppointmentTypeID int
	EnvironmentTypeID int
	Color             string
}

type SchedulingPolicyError struct {
	Outcome string
	Message string
	Missing []string
}

// PrepareBooking validates and resolves every office-owned booking decision.
func (p SchedulingPolicy) PrepareBooking(req BookingPolicyRequest) (BookingPolicyDecision, *SchedulingPolicyError) {
	if p.office == nil {
		return BookingPolicyDecision{}, &SchedulingPolicyError{Message: "Office is required"}
	}

	columnID := strconv.Itoa(req.ColumnID)
	column, ok := p.office.Columns[columnID]
	if !ok {
		return BookingPolicyDecision{}, &SchedulingPolicyError{Message: fmt.Sprintf("Column %d is not a valid provider column for %s", req.ColumnID, p.office.DisplayName)}
	}
	if column.ProfileID != strconv.Itoa(req.ProfileID) {
		return BookingPolicyDecision{}, &SchedulingPolicyError{Message: fmt.Sprintf("Profile %d is not valid for column %d at %s", req.ProfileID, req.ColumnID, p.office.DisplayName)}
	}

	routing := p.SchedulingRouting(req.Routing, req.DOB)
	typeID := req.AppointmentTypeID
	if typeID == 0 {
		resolution := ResolveAppointmentTypeForIntent(p.office, routing, req.Intent)
		if resolution.AppointmentTypeID == 0 {
			message := resolution.Message
			if message == "" {
				message = "Could not resolve appointment type from booking intent."
			}
			return BookingPolicyDecision{}, &SchedulingPolicyError{
				Outcome: "appointment_type_unresolved",
				Message: message,
				Missing: resolution.Missing,
			}
		}
		typeID = resolution.AppointmentTypeID
	}

	routingColumns := p.office.ColumnsForRouting(routing)
	if routingColumns == nil {
		return BookingPolicyDecision{}, &SchedulingPolicyError{Message: fmt.Sprintf("Cannot book appointment with routing %q at %s", routing, p.office.DisplayName)}
	}
	if !routingColumns[columnID] {
		return BookingPolicyDecision{}, &SchedulingPolicyError{Message: fmt.Sprintf("Column %d is not valid for routing %q at %s", req.ColumnID, routing, p.office.DisplayName)}
	}

	environmentTypeID, ok := ResolveAppointmentTypeID(typeID)
	if !ok {
		return BookingPolicyDecision{}, &SchedulingPolicyError{Message: fmt.Sprintf("Invalid appointment type ID: %d. Valid types: 1004, 1005, 1006, 1007, 1008, 1010, 3364, 4244, 4245, 6167, 6168, 6169", typeID)}
	}
	color, ok := p.office.AppointmentColor(typeID)
	if !ok {
		return BookingPolicyDecision{}, &SchedulingPolicyError{Message: fmt.Sprintf("Invalid appointment type ID: %d", typeID)}
	}
	if !slices.Contains(p.AllowedAppointmentTypeIDs(routing, ""), typeID) {
		return BookingPolicyDecision{}, &SchedulingPolicyError{Message: fmt.Sprintf("Appointment type %d is not valid for routing %q at %s", typeID, routing, p.office.DisplayName)}
	}
	if !p.office.ColumnAllowsDOB(columnID, req.DOB) {
		message := fmt.Sprintf("%s requires patient age %d or older", column.ShortName, column.MinAgeYears)
		if req.DOB == "" {
			message = fmt.Sprintf("%s requires patient DOB to verify age %d or older", column.ShortName, column.MinAgeYears)
		}
		return BookingPolicyDecision{}, &SchedulingPolicyError{Message: message}
	}
	if !slices.Contains(p.AllowedAppointmentTypeIDs(routing, req.DOB), typeID) {
		return BookingPolicyDecision{}, &SchedulingPolicyError{Message: fmt.Sprintf("Appointment type %d is not valid for routing %q at %s", typeID, routing, p.office.DisplayName)}
	}
	return BookingPolicyDecision{
		Routing:           routing,
		AppointmentTypeID: typeID,
		EnvironmentTypeID: environmentTypeID,
		Color:             color,
	}, nil
}

type SameStartDecision struct {
	Capacity      int
	Bookable      bool
	RequiresForce bool
}

// SameStart applies the office column's capacity and force rule at one start time.
func (p SchedulingPolicy) SameStart(columnID string, start time.Time, booked int) SameStartDecision {
	capacity := defaultSameStartCapacity
	if p.office != nil {
		if column, ok := p.office.Columns[columnID]; ok {
			if configured := column.SameStartCapacityAt(start); configured > capacity {
				capacity = configured
			}
		}
	}
	return SameStartDecision{
		Capacity:      capacity,
		Bookable:      booked < capacity,
		RequiresForce: booked > 0 && booked < capacity,
	}
}
