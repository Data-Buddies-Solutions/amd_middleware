package scheduling

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"advancedmd-token-management/internal/domain"
)

type schedulingPolicy struct {
	office *domain.OfficeConfig
	asOf   time.Time
}

func newSchedulingPolicy(office *domain.OfficeConfig, asOf time.Time) schedulingPolicy {
	return schedulingPolicy{office: office, asOf: asOf}
}

func (p schedulingPolicy) AllowedAppointmentTypeIDs(routing domain.RoutingRule, dob domain.DOB) []int {
	if p.office == nil {
		return nil
	}

	typeIDs := make([]int, 0, len(domain.AppointmentTypes))
	for typeID := range domain.AppointmentTypes {
		if p.office.AllowsAppointmentType(typeID, routing) && appointmentTypeMatchesDOB(typeID, dob, p.asOf) {
			typeIDs = append(typeIDs, typeID)
		}
	}
	sort.Ints(typeIDs)
	return typeIDs
}

func appointmentTypeMatchesDOB(typeID int, dob domain.DOB, asOf time.Time) bool {
	patientBand := dob.AgeBandOn(asOf)
	typeBand := domain.AppointmentTypes[typeID].AgeBand
	if patientBand == "" || (typeBand != domain.AppointmentAgePediatric && typeBand != domain.AppointmentAgeAdult) {
		return true
	}
	return typeBand == patientBand
}

func (p schedulingPolicy) EligibleColumns(columns []domain.SchedulerColumn, routing domain.RoutingRule, dob domain.DOB) []domain.SchedulerColumn {
	if p.office == nil {
		return nil
	}

	routingColumns := p.office.ColumnsForRouting(routing)
	if routingColumns == nil {
		return nil
	}

	eligible := make([]domain.SchedulerColumn, 0, len(columns))
	for _, column := range columns {
		if column.FacilityID != p.office.FacilityID || !routingColumns[column.ID] || !p.office.ColumnAllowsDOB(column.ID, dob, p.asOf) {
			continue
		}
		eligible = append(eligible, column)
	}
	return eligible
}

type bookingPolicyRequest struct {
	ColumnID                   int
	ProfileID                  int
	AppointmentTypeID          int
	Routing                    domain.RoutingRule
	DOB                        domain.DOB
	Intent                     appointmentIntent
	PreservedAppointmentTypeID int
}

type bookingPolicyDecision struct {
	Routing           domain.RoutingRule
	AppointmentTypeID int
	EnvironmentTypeID int
	Color             string
}

type schedulingPolicyError struct {
	Outcome string
	Message string
	Missing []string
}

func (p schedulingPolicy) PrepareBooking(req bookingPolicyRequest) (bookingPolicyDecision, *schedulingPolicyError) {
	if p.office == nil {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: "Office is required"}
	}

	column, ok := p.office.Columns[req.ColumnID]
	if !ok {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Column %d is not a valid provider column for %s", req.ColumnID, p.office.DisplayName)}
	}
	if column.ProfileID != req.ProfileID {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Profile %d is not valid for column %d at %s", req.ProfileID, req.ColumnID, p.office.DisplayName)}
	}

	routing := p.office.SchedulingRouting(req.Routing, req.DOB, p.asOf)
	typeID := req.AppointmentTypeID
	preservesExistingType := req.PreservedAppointmentTypeID > 0
	if preservesExistingType {
		typeID = req.PreservedAppointmentTypeID
	}
	if typeID == 0 {
		resolution := resolveAppointmentTypeForIntent(p.office, routing, req.Intent, req.DOB, p.asOf)
		if resolution.AppointmentTypeID == 0 {
			message := resolution.Message
			if message == "" {
				message = "Could not resolve appointment type from booking intent."
			}
			return bookingPolicyDecision{}, &schedulingPolicyError{
				Outcome: "appointment_type_unresolved",
				Message: message,
				Missing: resolution.Missing,
			}
		}
		typeID = resolution.AppointmentTypeID
	}

	routingColumns := p.office.ColumnsForRouting(routing)
	if routingColumns == nil {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Cannot book appointment with routing %q at %s", routing, p.office.DisplayName)}
	}
	if !routingColumns[req.ColumnID] {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Column %d is not valid for routing %q at %s", req.ColumnID, routing, p.office.DisplayName)}
	}

	environmentTypeID, ok := domain.ResolveAppointmentTypeID(typeID)
	if !ok && !preservesExistingType {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Invalid appointment type ID: %d. Valid types: %s", typeID, domain.ValidAppointmentTypeIDs())}
	}
	if !ok {
		environmentTypeID = typeID
	}
	color, ok := p.office.AppointmentColor(typeID)
	if !ok && !preservesExistingType {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Invalid appointment type ID: %d", typeID)}
	}
	if !ok {
		color = domain.PreservedAppointmentTypeFallbackColor
	}
	if !preservesExistingType && !slices.Contains(p.AllowedAppointmentTypeIDs(routing, ""), typeID) {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Appointment type %d is not valid for routing %q at %s", typeID, routing, p.office.DisplayName)}
	}
	if !p.office.ColumnAllowsDOB(req.ColumnID, req.DOB, p.asOf) {
		message := fmt.Sprintf("%s requires patient age %d or older", column.ShortName, column.MinAgeYears)
		if req.DOB == "" {
			message = fmt.Sprintf("%s requires patient DOB to verify age %d or older", column.ShortName, column.MinAgeYears)
		}
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: message}
	}
	if !preservesExistingType && !slices.Contains(p.AllowedAppointmentTypeIDs(routing, req.DOB), typeID) {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Appointment type %d is not valid for routing %q at %s", typeID, routing, p.office.DisplayName)}
	}
	return bookingPolicyDecision{
		Routing:           routing,
		AppointmentTypeID: typeID,
		EnvironmentTypeID: environmentTypeID,
		Color:             color,
	}, nil
}
