package domain

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
)

func validateOfficeTable(table officeTable) error {
	definitions := make(map[string]officeDefinition, len(table.Offices))
	displayNames := map[string]bool{}
	for _, definition := range table.Offices {
		if _, ok := definitions[definition.ID]; ok {
			return fmt.Errorf("office %s is defined twice", definition.ID)
		}
		if definition.DisplayName == "" || displayNames[definition.DisplayName] {
			return fmt.Errorf("office %s needs a display name no other office uses", definition.ID)
		}
		definitions[definition.ID] = definition
		displayNames[definition.DisplayName] = true
	}
	if _, ok := definitions[defaultOfficeID]; !ok {
		return fmt.Errorf("default office %s is not in the table", defaultOfficeID)
	}
	for _, definition := range table.Offices {
		if err := validateOfficeDefinition(definition, definitions); err != nil {
			return fmt.Errorf("office %s: %w", definition.ID, err)
		}
	}
	if len(table.Environments) != len(environments) {
		return fmt.Errorf("the table binds %d environments, want exactly %v", len(table.Environments), environments)
	}
	for _, env := range environments {
		binding, ok := table.Environments[env]
		if !ok {
			return fmt.Errorf("%s: no bindings", env)
		}
		if err := validateEnvironment(table.Offices, definitions, binding); err != nil {
			return fmt.Errorf("%s: %w", env, err)
		}
	}
	return nil
}

func validateOfficeDefinition(definition officeDefinition, definitions map[string]officeDefinition) error {
	for _, key := range slices.Sorted(maps.Keys(definition.Columns)) {
		p := definition.Columns[key].Provider
		if p.DisplayName == "" || p.ShortName == "" || p.MatchKey == "" {
			return fmt.Errorf("column %s needs a display name, short name, and match key", key)
		}
	}
	for _, rule := range slices.Sorted(maps.Keys(definition.RoutingTiers)) {
		if parsed, err := ParseRoutingRule(string(rule)); err != nil || parsed == RoutingNotAccepted {
			return fmt.Errorf("routing tier %q is not a scheduling lane", rule)
		}
		seen := map[string]bool{}
		for _, key := range definition.RoutingTiers[rule] {
			if _, ok := definition.Columns[key]; !ok {
				return fmt.Errorf("routing tier %s names unknown column %s", rule, key)
			}
			if seen[key] {
				return fmt.Errorf("routing tier %s lists column %s twice", rule, key)
			}
			seen[key] = true
		}
	}
	if _, err := ParseRoutingRule(string(definition.PediatricRouting)); err != nil {
		return fmt.Errorf("pediatric routing %q is not a routing rule", definition.PediatricRouting)
	}
	if !slices.Contains(definition.AppointmentLookupOfficeIDs, definition.ID) {
		return fmt.Errorf("appointment lookup group must include the office itself")
	}
	for i, id := range definition.AppointmentLookupOfficeIDs {
		if _, ok := definitions[id]; !ok {
			return fmt.Errorf("appointment lookup group names unknown office %s", id)
		}
		if slices.Contains(definition.AppointmentLookupOfficeIDs[:i], id) {
			return fmt.Errorf("appointment lookup group lists office %s twice", id)
		}
	}
	types := definition.AppointmentTypes
	if err := checkAppointmentType(types.PostOp, AppointmentVisitMedical, appointmentAgeAny); err != nil {
		return fmt.Errorf("post-op appointment type: %w", err)
	}
	if err := checkLadder(types.Medical, AppointmentVisitMedical); err != nil {
		return fmt.Errorf("medical appointment types: %w", err)
	}
	switch {
	case len(definition.RoutingTiers[RoutingOpticalOnly]) > 0:
		if err := checkLadder(types.RoutineVision, AppointmentVisitRoutineVision); err != nil {
			return fmt.Errorf("routine vision appointment types: %w", err)
		}
	case types.RoutineVision != AppointmentTypeLadder{}:
		return fmt.Errorf("routine vision appointment types need an optical routing tier")
	}
	if (definition.RoutineVisionMinimumAge.Years > 0) != (definition.RoutineVisionMinimumAge.Message != "") {
		return fmt.Errorf("routine vision minimum age needs both years and a message")
	}
	return nil
}

func checkLadder(ladder AppointmentTypeLadder, visitType string) error {
	for _, slot := range []struct {
		typeID  int
		ageBand string
	}{
		{ladder.NewPediatric, AppointmentAgePediatric},
		{ladder.NewAdult, AppointmentAgeAdult},
		{ladder.EstablishedPediatric, AppointmentAgePediatric},
		{ladder.EstablishedAdult, AppointmentAgeAdult},
	} {
		if err := checkAppointmentType(slot.typeID, visitType, slot.ageBand); err != nil {
			return err
		}
	}
	return nil
}

func checkAppointmentType(typeID int, visitType, ageBand string) error {
	appointmentType, ok := AppointmentTypes[typeID]
	if !ok {
		return fmt.Errorf("unknown appointment type %d", typeID)
	}
	if appointmentType.VisitType != visitType {
		return fmt.Errorf("appointment type %d is %s, want %s", typeID, appointmentType.VisitType, visitType)
	}
	if ageBand != appointmentAgeAny && appointmentType.AgeBand != appointmentAgeAny && appointmentType.AgeBand != ageBand {
		return fmt.Errorf("appointment type %d is for %s patients, want %s", typeID, appointmentType.AgeBand, ageBand)
	}
	return nil
}

func validateEnvironment(offices []officeDefinition, definitions map[string]officeDefinition, binding environmentBinding) error {
	if err := checkMarks("office", slices.Collect(maps.Keys(binding.Offices)), binding.UnavailableOffices, slices.Collect(maps.Keys(definitions))); err != nil {
		return err
	}
	if _, ok := binding.Offices[defaultOfficeID]; !ok {
		return fmt.Errorf("default office %s is unavailable", defaultOfficeID)
	}
	phoneOwners := map[string]string{}
	columnOwners := map[string]string{}
	for _, definition := range offices {
		officeBinding, ok := binding.Offices[definition.ID]
		if !ok {
			continue
		}
		if len(officeBinding.Phones) == 0 || officeBinding.FacilityID == "" || officeBinding.DefaultProfileID == "" {
			return fmt.Errorf("office %s needs a phone, a facility, and a default profile", definition.ID)
		}
		for _, phone := range officeBinding.Phones {
			if owner, ok := phoneOwners[phone]; ok {
				return fmt.Errorf("phone %s is bound to %s and %s", phone, owner, definition.ID)
			}
			phoneOwners[phone] = definition.ID
		}
		if err := checkMarks("office "+definition.ID+" column", slices.Collect(maps.Keys(officeBinding.Columns)), officeBinding.UnavailableColumns, slices.Collect(maps.Keys(definition.Columns))); err != nil {
			return err
		}
		for _, key := range slices.Sorted(maps.Keys(officeBinding.Columns)) {
			column := officeBinding.Columns[key]
			if column.ColumnID == "" || column.ProfileID == "" {
				return fmt.Errorf("office %s column %s needs a column ID and a profile ID", definition.ID, key)
			}
			if owner, ok := columnOwners[column.ColumnID]; ok {
				return fmt.Errorf("column ID %s is bound to %s and %s/%s", column.ColumnID, owner, definition.ID, key)
			}
			columnOwners[column.ColumnID] = definition.ID + "/" + key
		}
		if err := checkLaneTypesBound(definition, officeBinding, binding); err != nil {
			return fmt.Errorf("office %s: %w", definition.ID, err)
		}
	}
	typeIDs := slices.Collect(maps.Keys(AppointmentTypes))
	if err := checkMarks("appointment type", slices.Collect(maps.Keys(binding.AppointmentTypes)), binding.UnavailableAppointmentTypes, typeIDs); err != nil {
		return err
	}
	canonicalIDs := map[int]int{}
	for _, typeID := range slices.Sorted(maps.Keys(binding.AppointmentTypes)) {
		providerTypeID := binding.AppointmentTypes[typeID]
		if providerTypeID <= 0 {
			return fmt.Errorf("appointment type %d needs a positive AdvancedMD type ID", typeID)
		}
		if other, ok := canonicalIDs[providerTypeID]; ok {
			return fmt.Errorf("AdvancedMD appointment type %d is bound to %d and %d", providerTypeID, other, typeID)
		}
		canonicalIDs[providerTypeID] = typeID
	}
	return nil
}

func checkLaneTypesBound(definition officeDefinition, officeBinding officeBinding, binding environmentBinding) error {
	medical, optical := false, false
	for rule, keys := range definition.RoutingTiers {
		for _, key := range keys {
			if _, ok := officeBinding.Columns[key]; ok {
				optical = optical || rule == RoutingOpticalOnly
				medical = medical || rule != RoutingOpticalOnly
			}
		}
	}
	types := definition.AppointmentTypes
	var required []int
	if medical {
		required = append(required, types.PostOp)
		required = append(required, ladderTypeIDs(types.Medical)...)
	}
	if optical {
		required = append(required, ladderTypeIDs(types.RoutineVision)...)
	}
	for _, typeID := range required {
		if _, ok := binding.AppointmentTypes[typeID]; !ok {
			return fmt.Errorf("books visits with appointment type %d, which is unavailable", typeID)
		}
	}
	return nil
}

func ladderTypeIDs(ladder AppointmentTypeLadder) []int {
	return []int{ladder.NewPediatric, ladder.NewAdult, ladder.EstablishedPediatric, ladder.EstablishedAdult}
}

func checkMarks[K cmp.Ordered](kind string, bound, unavailable, known []K) error {
	slices.Sort(bound)
	slices.Sort(known)
	for _, key := range bound {
		if !slices.Contains(known, key) {
			return fmt.Errorf("%s %v is bound but not in the table", kind, key)
		}
	}
	for i, key := range unavailable {
		if !slices.Contains(known, key) {
			return fmt.Errorf("%s %v is marked unavailable but not in the table", kind, key)
		}
		if slices.Contains(bound, key) {
			return fmt.Errorf("%s %v is both bound and marked unavailable", kind, key)
		}
		if slices.Contains(unavailable[:i], key) {
			return fmt.Errorf("%s %v is marked unavailable twice", kind, key)
		}
	}
	for _, key := range known {
		if !slices.Contains(bound, key) && !slices.Contains(unavailable, key) {
			return fmt.Errorf("%s %v has no binding and is not marked unavailable", kind, key)
		}
	}
	return nil
}
