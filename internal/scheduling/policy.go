package scheduling

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"advancedmd-token-management/internal/domain"
)

const defaultSameStartCapacity = 1

type schedulingPolicy struct {
	office *domain.OfficeConfig
}

func newSchedulingPolicy(office *domain.OfficeConfig) schedulingPolicy {
	return schedulingPolicy{office: office}
}

func (p schedulingPolicy) AllowedAppointmentTypeIDs(routing domain.RoutingRule, dob string) []int {
	if p.office == nil {
		return nil
	}

	typeIDs := make([]int, 0, len(domain.DefaultAppointmentTypeColors))
	for typeID := range domain.DefaultAppointmentTypeColors {
		if p.office.AllowsAppointmentType(typeID, routing) && appointmentTypeMatchesDOB(typeID, dob) {
			typeIDs = append(typeIDs, typeID)
		}
	}
	sort.Ints(typeIDs)
	return typeIDs
}

func appointmentTypeMatchesDOB(typeID int, dob string) bool {
	age, ok := domain.AgeYears(dob)
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

func (p schedulingPolicy) EligibleColumns(columns []domain.SchedulerColumn, profiles map[string]domain.SchedulerProfile, routing domain.RoutingRule, dob, requestedProvider string) []domain.SchedulerColumn {
	if p.office == nil {
		return nil
	}

	routingColumns := p.office.ColumnsForRouting(routing)
	if routingColumns == nil {
		return nil
	}

	eligible := make([]domain.SchedulerColumn, 0, len(columns))
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

func (p schedulingPolicy) matchesProvider(column domain.SchedulerColumn, profile domain.SchedulerProfile, requested string) bool {
	needle := strings.ToUpper(domain.NormalizeForLookup(requested))
	if needle == "" {
		return true
	}

	candidates := []string{profile.Name, column.Name}
	if officeColumn, ok := p.office.Columns[column.ID]; ok {
		candidates = append(candidates, officeColumn.DisplayName, officeColumn.ShortName)
	}
	for _, candidate := range candidates {
		if strings.Contains(strings.ToUpper(domain.NormalizeForLookup(candidate)), needle) {
			return true
		}
	}
	return false
}

type bookingPolicyRequest struct {
	ColumnID                   int
	ProfileID                  int
	AppointmentTypeID          int
	Routing                    domain.RoutingRule
	DOB                        string
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

	columnID := strconv.Itoa(req.ColumnID)
	column, ok := p.office.Columns[columnID]
	if !ok {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Column %d is not a valid provider column for %s", req.ColumnID, p.office.DisplayName)}
	}
	if column.ProfileID != strconv.Itoa(req.ProfileID) {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Profile %d is not valid for column %d at %s", req.ProfileID, req.ColumnID, p.office.DisplayName)}
	}

	routing := p.office.SchedulingRouting(req.Routing, req.DOB)
	typeID := req.AppointmentTypeID
	preservesExistingType := req.PreservedAppointmentTypeID > 0
	if preservesExistingType {
		typeID = req.PreservedAppointmentTypeID
	}
	if typeID == 0 {
		resolution := resolveAppointmentTypeForIntent(p.office, routing, req.Intent)
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
	if !routingColumns[columnID] {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Column %d is not valid for routing %q at %s", req.ColumnID, routing, p.office.DisplayName)}
	}

	environmentTypeID, ok := domain.ResolveAppointmentTypeID(typeID)
	if !ok && !preservesExistingType {
		return bookingPolicyDecision{}, &schedulingPolicyError{Message: fmt.Sprintf("Invalid appointment type ID: %d. Valid types: 1004, 1005, 1006, 1007, 1008, 1010, 3364, 4244, 4245, 6167, 6168, 6169", typeID)}
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
	if !p.office.ColumnAllowsDOB(columnID, req.DOB) {
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

type sameStartDecision struct {
	Capacity      int
	Bookable      bool
	RequiresForce bool
}

func (p schedulingPolicy) SameStart(columnID string, start time.Time, booked int) sameStartDecision {
	capacity := defaultSameStartCapacity
	if p.office != nil {
		if column, ok := p.office.Columns[columnID]; ok {
			if configured := column.SameStartCapacityAt(start); configured > capacity {
				capacity = configured
			}
		}
	}
	return sameStartDecision{
		Capacity:      capacity,
		Bookable:      booked < capacity,
		RequiresForce: booked > 0 && booked < capacity,
	}
}
