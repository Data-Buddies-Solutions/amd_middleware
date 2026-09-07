// Package patient owns patient resolution and construction of complete Acuity
// patient results.
package patient

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

type Status string

const (
	StatusVerified        Status = "verified"
	StatusCandidate       Status = "candidate"
	StatusMultipleMatches Status = "multiple_matches"
	StatusNotFound        Status = "not_found"
)

type AppointmentsStatus string

const (
	AppointmentsFound AppointmentsStatus = "found"
	AppointmentsNone  AppointmentsStatus = "none"
	AppointmentsError AppointmentsStatus = "error"
)

type CreateStatus string

const (
	CreateStatusCreated CreateStatus = "created"
	CreateStatusPartial CreateStatus = "partial"
	CreateStatusError   CreateStatus = "error"
)

type MutationOutcome string

const (
	MutationRejected           MutationOutcome = "rejected"
	MutationReconciledFailure  MutationOutcome = "reconciled_failure"
	MutationIndeterminateWrite MutationOutcome = "indeterminate_write"
	MutationValidationFailed   MutationOutcome = "validation_failed"
	MutationUnavailable        MutationOutcome = "unavailable"
	MutationFailed             MutationOutcome = "failed"
	MutationReconciledSuccess  MutationOutcome = "reconciled_success"
)

type UpdateInsuranceStatus string

const (
	UpdateInsuranceStatusUpdated UpdateInsuranceStatus = "updated"
	UpdateInsuranceStatusError   UpdateInsuranceStatus = "error"
)

// ResolveCommand is the complete patient-resolution intent accepted from the
// HTTP adapter after transport validation.
type ResolveCommand struct {
	PatientID string
	LastName  string
	DOB       string
	FirstName string
	Phone     string
	OfficeID  string
}

type Appointment struct {
	ID                int
	Date              string
	Time              string
	Provider          string
	Type              string
	AppointmentTypeID int
	Facility          string
	OfficeID          string
	Office            string
	CancellationToken string
	RescheduleToken   string
}

// Candidate contains only the private identity-selection facts available
// before one patient is fully hydrated.
type Candidate struct {
	Status    Status
	PatientID string
	FirstName string
	LastName  string
	DOB       string
}

// ResolveResult is one complete Acuity patient resolution outcome.
type ResolveResult struct {
	Status              Status
	ProviderFailure     safeerrors.Category
	PatientID           string
	FirstName           string
	LastName            string
	Name                string
	DOB                 string
	Phone               string
	InsuranceCarrier    string
	InsuranceCarrierID  string
	InsPlanID           string
	RespPartyID         string
	Routing             domain.RoutingRule
	AllowedProviders    []string
	RoutingAmbiguous    bool
	PreauthRequired     bool
	AppointmentsStatus  AppointmentsStatus
	Appointments        []Appointment
	AppointmentsMessage string
	Message             string
	Matches             []Candidate
	Observation         ResolutionObservation
}

// ResolutionObservation contains only PHI-free operational facts for one
// patient-resolution request.
type ResolutionObservation struct {
	Recorded                bool
	PatientSearchDurationMS int64
	DemographicDurationMS   int64
	AppointmentDurationMS   int64
	PatientSearchReads      int
	DemographicReads        int
	AppointmentReads        int
	OfficeGroupSize         int
	CandidateCountBucket    string
	AppointmentOutcome      string
}

// CreateCommand is the complete caller intent for creating a patient and
// attaching primary insurance.
type CreateCommand struct {
	FirstName      string
	LastName       string
	DOB            string
	Phone          string
	Email          string
	Street         string
	AptSuite       string
	City           string
	State          string
	Zip            string
	Sex            string
	SSN            string
	Insurance      string
	CoverageType   string
	SubscriberName string
	SubscriberNum  string
	Office         string
}

// CreateResult preserves the public patient-creation response contract.
type CreateResult struct {
	Status           CreateStatus
	Outcome          MutationOutcome
	PatientID        string
	Name             string
	DOB              string
	Routing          domain.RoutingRule
	AllowedProviders []string
	PreauthRequired  bool
	Message          string
}

// UpdateInsuranceCommand is the complete caller intent for replacing primary
// insurance.
type UpdateInsuranceCommand struct {
	PatientID      string
	DOB            string
	InsPlanID      string
	RespPartyID    string
	OldInsurance   string
	Insurance      string
	CoverageType   string
	SubscriberName string
	SubscriberNum  string
	Office         string
}

// UpdateInsuranceResult preserves the public insurance-update response
// contract.
type UpdateInsuranceResult struct {
	Status           UpdateInsuranceStatus
	Outcome          MutationOutcome
	PatientID        string
	OldInsurance     string
	NewInsurance     string
	Routing          domain.RoutingRule
	AllowedProviders []string
	RoutingAmbiguous bool
	PreauthRequired  bool
	Message          string
}

// Patient is the single interface used by patient-facing HTTP routes.
type Patient interface {
	Resolve(context.Context, ResolveCommand) (ResolveResult, error)
	Create(context.Context, CreateCommand) CreateResult
	UpdateInsurance(context.Context, UpdateInsuranceCommand) UpdateInsuranceResult
}

type patient struct {
	advancedMD        advancedmd.PatientRecords
	appointmentTokens AppointmentTokenIssuer
}

type AppointmentTokenIssuer interface {
	IssueCancellationToken(string, domain.PatientAppointment) (string, error)
	IssueRescheduleToken(string, domain.PatientAppointment) (string, error)
}

type MutationMetric struct {
	Operation string
	Outcome   string
	Count     uint64
}

type mutationMetricKey struct {
	operation string
	outcome   string
}

const (
	maxReadAttempts = 3
	readRetryDelay  = 5 * time.Millisecond
)

var mutationMetrics = struct {
	sync.Mutex
	counts map[mutationMetricKey]uint64
}{
	counts: make(map[mutationMetricKey]uint64),
}

func New(advancedMD advancedmd.PatientRecords) Patient {
	return &patient{advancedMD: advancedMD}
}

func NewWithAppointmentTokens(
	advancedMD advancedmd.PatientRecords,
	appointmentTokens AppointmentTokenIssuer,
) Patient {
	return &patient{
		advancedMD:        advancedMD,
		appointmentTokens: appointmentTokens,
	}
}

func (p *patient) Create(ctx context.Context, command CreateCommand) (result CreateResult) {
	defer func() {
		recordMutation("create", createOutcome(result))
	}()

	office, err := domain.ResolveOffice(command.Office)
	if err != nil {
		return CreateResult{Status: CreateStatusError, Outcome: MutationValidationFailed, Message: err.Error()}
	}
	if domain.IsSelfPayInsurance(command.Insurance) && strings.TrimSpace(command.SubscriberNum) == "" {
		command.SubscriberNum = "self pay"
	}
	if missing := createMissingFields(command); len(missing) > 0 {
		return CreateResult{
			Status:  CreateStatusError,
			Outcome: MutationValidationFailed,
			Message: fmt.Sprintf("Missing required fields: %s", strings.Join(missing, ", ")),
		}
	}
	selection, message := selectInsurance(command.Insurance, command.CoverageType, office)
	if message != "" {
		return CreateResult{Status: CreateStatusError, Outcome: MutationValidationFailed, Message: message}
	}

	created, createReconciled, outcome := p.createPatient(ctx, command, office)
	switch outcome {
	case MutationRejected:
		return CreateResult{Status: CreateStatusError, Outcome: outcome, Message: "AdvancedMD rejected patient creation. Please contact the office."}
	case MutationReconciledFailure:
		return CreateResult{Status: CreateStatusError, Outcome: outcome, Message: "AdvancedMD did not create the patient. Please try again or contact the office."}
	case MutationIndeterminateWrite:
		return CreateResult{Status: CreateStatusError, Outcome: outcome, Message: "Patient creation may have been applied, but the outcome could not be confirmed. Do not retry automatically; contact the office."}
	case MutationUnavailable, MutationFailed:
		return CreateResult{Status: CreateStatusError, Outcome: outcome, Message: "Failed to create patient in AdvancedMD. Please try again or contact the office."}
	}

	insuranceReconciled, outcome := p.addInsurance(ctx, domain.PatientInsurance{
		PatientID:     created.ID,
		RespPartyID:   created.RespPartyID,
		CarrierID:     selection.entry.CarrierID,
		SubscriberNum: command.SubscriberNum,
	})
	if outcome != "" {
		partial := CreateResult{
			Status:    CreateStatusPartial,
			Outcome:   outcome,
			PatientID: created.ID,
			Name:      created.Name,
			DOB:       domain.NormalizeDOB(command.DOB),
		}
		switch outcome {
		case MutationRejected:
			partial.Message = "Patient created, but AdvancedMD rejected the insurance attachment. Please contact the office."
		case MutationReconciledFailure:
			partial.Message = "Patient created, but AdvancedMD did not attach the insurance. Please try again or contact the office."
		case MutationIndeterminateWrite:
			partial.Message = "Patient created, but the insurance attachment may have been applied and could not be confirmed. Do not retry automatically; contact the office."
		default:
			partial.Message = "Patient created but insurance could not be attached. Please contact the office."
		}
		return partial
	}

	routing := selection.entry.Routing
	if selection.mode == domain.InsuranceModeMedical {
		routing = selection.policy.SchedulingRouting(routing, domain.NormalizeDOB(command.DOB))
	}
	result = CreateResult{
		Status:           CreateStatusCreated,
		PatientID:        created.ID,
		Name:             created.Name,
		DOB:              domain.NormalizeDOB(command.DOB),
		Routing:          routing,
		AllowedProviders: selection.policy.ProviderNames(routing, domain.NormalizeDOB(command.DOB)),
		PreauthRequired:  selection.entry.PreauthRequired,
		Message:          "Patient created and insurance attached successfully",
	}
	if createReconciled || insuranceReconciled {
		result.Outcome = MutationReconciledSuccess
	}
	return result
}

type insuranceSelection struct {
	entry  domain.InsuranceEntry
	mode   domain.InsuranceMode
	policy domain.SchedulingPolicy
}

func selectInsurance(name, coverageType string, office *domain.OfficeConfig) (insuranceSelection, string) {
	mode := domain.InsuranceModeForCoverage(coverageType)
	entry, ok := domain.LookupInsuranceForCoverageAtOffice(name, mode, office)
	policy := domain.NewSchedulingPolicy(office)

	switch {
	case mode == domain.InsuranceModeVision && !policy.SupportsRouting(domain.RoutingOpticalOnly):
		return insuranceSelection{}, fmt.Sprintf("Routine vision coverage is not supported at %s. Route the patient to Spring Hill routine vision scheduling.", office.DisplayName)
	case mode == domain.InsuranceModeMedical && !policy.SupportsMedical():
		return insuranceSelection{}, fmt.Sprintf("Medical coverage is not supported at %s. Use routine vision coverage for this office or route medical visits to a medical office.", office.DisplayName)
	case !ok:
		return insuranceSelection{}, fmt.Sprintf("Insurance not recognized: %q. Please use an insurance name from the accepted list.", name)
	case entry.Routing == domain.RoutingNotAccepted:
		return insuranceSelection{}, fmt.Sprintf("%s is not accepted at %s.", name, office.DisplayName)
	default:
		return insuranceSelection{entry: entry, mode: mode, policy: policy}, ""
	}
}

func createMissingFields(command CreateCommand) []string {
	fields := []struct {
		name  string
		value string
	}{
		{"firstName", command.FirstName},
		{"lastName", command.LastName},
		{"dob", command.DOB},
		{"phone", command.Phone},
		{"street", command.Street},
		{"city", command.City},
		{"state", command.State},
		{"zip", command.Zip},
		{"sex", command.Sex},
		{"insurance", command.Insurance},
		{"subscriberName", command.SubscriberName},
		{"subscriberNum", command.SubscriberNum},
	}
	missing := make([]string, 0)
	for _, field := range fields {
		if field.value == "" {
			missing = append(missing, field.name)
		}
	}
	return missing
}

func (p *patient) createPatient(ctx context.Context, command CreateCommand, office *domain.OfficeConfig) (domain.CreatedPatient, bool, MutationOutcome) {
	baseline, err := p.creationBaseline(ctx, command)
	if err != nil {
		return domain.CreatedPatient{}, false, failureOutcome(err)
	}

	created, err := p.advancedMD.CreatePatient(ctx, domain.PatientCreate{
		FirstName: domain.StripDiacritics(command.FirstName),
		LastName:  domain.StripDiacritics(command.LastName),
		DOB:       domain.NormalizeDOB(command.DOB),
		Phone:     domain.FormatPhone(command.Phone),
		Email:     strings.TrimSpace(command.Email),
		Street:    command.Street,
		AptSuite:  command.AptSuite,
		City:      command.City,
		State:     strings.ToUpper(command.State),
		Zip:       command.Zip,
		Sex:       domain.NormalizeSex(command.Sex),
		SSN:       strings.TrimSpace(command.SSN),
		OfficeID:  office.ID,
	})
	if err == nil {
		return created, false, ""
	}
	switch advancedmd.MutationFailureOf(err) {
	case advancedmd.MutationRejected:
		return domain.CreatedPatient{}, false, MutationRejected
	case advancedmd.MutationAmbiguous:
		created, outcome := p.reconcileCreatedPatient(ctx, command, baseline)
		return created, outcome == "", outcome
	default:
		return domain.CreatedPatient{}, false, failureOutcome(err)
	}
}

func (p *patient) UpdateInsurance(ctx context.Context, command UpdateInsuranceCommand) (result UpdateInsuranceResult) {
	defer func() {
		recordMutation("update_insurance", updateInsuranceOutcome(result))
	}()

	if domain.IsSelfPayInsurance(command.Insurance) && strings.TrimSpace(command.SubscriberNum) == "" {
		command.SubscriberNum = "self pay"
	}
	if command.PatientID == "" || command.Insurance == "" || command.SubscriberNum == "" {
		return UpdateInsuranceResult{
			Status:  UpdateInsuranceStatusError,
			Outcome: MutationValidationFailed,
			Message: "patientId, insurance, and subscriberNum are required",
		}
	}
	if err := domain.ValidateOptionalDOB(command.DOB); err != nil {
		return UpdateInsuranceResult{
			Status:  UpdateInsuranceStatusError,
			Outcome: MutationValidationFailed,
			Message: err.Error(),
		}
	}
	office, err := domain.ResolveOffice(command.Office)
	if err != nil {
		return UpdateInsuranceResult{Status: UpdateInsuranceStatusError, Outcome: MutationValidationFailed, Message: err.Error()}
	}
	selection, message := selectInsurance(command.Insurance, command.CoverageType, office)
	if message != "" {
		return UpdateInsuranceResult{Status: UpdateInsuranceStatusError, Outcome: MutationValidationFailed, Message: message}
	}

	reconciled, replacementAlreadyActive, outcome := p.endInsurance(ctx, command, selection.entry.CarrierID)
	if outcome != "" {
		return updateInsuranceFailure(outcome, "Failed to update existing insurance in AdvancedMD. Please try again or contact the office.")
	}

	if !replacementAlreadyActive {
		addReconciled, outcome := p.addInsurance(ctx, domain.PatientInsurance{
			PatientID:     command.PatientID,
			RespPartyID:   command.RespPartyID,
			CarrierID:     selection.entry.CarrierID,
			SubscriberNum: command.SubscriberNum,
		})
		reconciled = reconciled || addReconciled
		if outcome != "" {
			return updateInsuranceFailure(outcome, "Failed to attach new insurance in AdvancedMD. Please try again or contact the office.")
		}
	}

	routing := selection.policy.SchedulingRouting(selection.entry.Routing, command.DOB)
	_, ambiguous := domain.RoutingForDemographicInsurance(selection.entry.CarrierID, command.Insurance, office)
	result = UpdateInsuranceResult{
		Status:           UpdateInsuranceStatusUpdated,
		PatientID:        command.PatientID,
		OldInsurance:     command.OldInsurance,
		NewInsurance:     command.Insurance,
		Routing:          routing,
		AllowedProviders: selection.policy.ProviderNames(routing, command.DOB),
		RoutingAmbiguous: ambiguous,
		PreauthRequired:  selection.entry.PreauthRequired,
		Message:          "Insurance updated successfully",
	}
	if reconciled {
		result.Outcome = MutationReconciledSuccess
	}
	return result
}

func updateInsuranceFailure(outcome MutationOutcome, message string) UpdateInsuranceResult {
	switch outcome {
	case MutationRejected:
		message = "AdvancedMD rejected the insurance update. Please contact the office."
	case MutationReconciledFailure:
		message = "AdvancedMD did not apply the insurance update. Please try again or contact the office."
	case MutationIndeterminateWrite:
		message = "The insurance update may have been applied, but the outcome could not be confirmed. Do not retry automatically; contact the office."
	}
	return UpdateInsuranceResult{Status: UpdateInsuranceStatusError, Outcome: outcome, Message: message}
}

func (p *patient) endInsurance(ctx context.Context, command UpdateInsuranceCommand, replacementCarrierID string) (bool, bool, MutationOutcome) {
	if command.InsPlanID == "" {
		return false, false, ""
	}
	err := p.advancedMD.EndDatePatientInsurance(ctx, domain.PatientInsuranceEnd{
		PatientID: command.PatientID,
		InsPlanID: command.InsPlanID,
	})
	if err == nil {
		return false, false, ""
	}
	switch advancedmd.MutationFailureOf(err) {
	case advancedmd.MutationRejected:
		return false, false, MutationRejected
	case advancedmd.MutationAmbiguous:
		demographics, known := p.reconcileInsurance(ctx, command.PatientID)
		if !known {
			return false, false, MutationIndeterminateWrite
		}
		if demographics.InsPlanID == command.InsPlanID {
			return false, false, MutationReconciledFailure
		}
		replacement := domain.PatientInsurance{
			RespPartyID:   command.RespPartyID,
			CarrierID:     replacementCarrierID,
			SubscriberNum: command.SubscriberNum,
		}
		return true, insuranceMatches(demographics, replacement), ""
	default:
		return false, false, failureOutcome(err)
	}
}

func (p *patient) addInsurance(ctx context.Context, command domain.PatientInsurance) (bool, MutationOutcome) {
	err := p.advancedMD.AddPatientInsurance(ctx, command)
	if err == nil {
		return false, ""
	}
	switch advancedmd.MutationFailureOf(err) {
	case advancedmd.MutationRejected:
		return false, MutationRejected
	case advancedmd.MutationAmbiguous:
		demographics, known := p.reconcileInsurance(ctx, command.PatientID)
		if !known {
			return false, MutationIndeterminateWrite
		}
		if !insuranceMatches(demographics, command) {
			return false, MutationReconciledFailure
		}
		return true, ""
	default:
		return false, failureOutcome(err)
	}
}

func insuranceMatches(demographics domain.PatientDemographics, intended domain.PatientInsurance) bool {
	return demographics.CarrierID == intended.CarrierID &&
		demographics.RespPartyID == intended.RespPartyID &&
		strings.EqualFold(strings.TrimSpace(demographics.SubscriberNum), strings.TrimSpace(intended.SubscriberNum))
}

func (p *patient) reconcileInsurance(ctx context.Context, patientID string) (domain.PatientDemographics, bool) {
	demographics, err := retryRead(ctx, func() (domain.PatientDemographics, error) {
		return p.advancedMD.GetPatientDemographics(ctx, patientID)
	})
	if err != nil || !demographics.InsuranceStateKnown {
		return domain.PatientDemographics{}, false
	}
	return demographics, true
}

func (p *patient) creationBaseline(ctx context.Context, command CreateCommand) (map[string]struct{}, error) {
	search := domain.PatientSearch{Phone: domain.NormalizePhoneDigits(command.Phone)}
	candidates, err := retryRead(ctx, func() ([]domain.Patient, error) {
		return p.advancedMD.SearchPatients(ctx, search)
	})
	if err != nil {
		return nil, err
	}

	baseline := make(map[string]struct{})
	for _, candidate := range candidates {
		id := domain.StripPatientPrefix(candidate.ID)
		if id == "" {
			return nil, errors.New("patient reconciliation baseline contains a record without an ID")
		}
		baseline[id] = struct{}{}
	}
	return baseline, nil
}

func (p *patient) reconcileCreatedPatient(
	ctx context.Context,
	command CreateCommand,
	baseline map[string]struct{},
) (domain.CreatedPatient, MutationOutcome) {
	search := domain.PatientSearch{Phone: domain.NormalizePhoneDigits(command.Phone)}
	for attempt := 1; attempt <= maxReadAttempts; attempt++ {
		candidates, err := p.advancedMD.SearchPatients(ctx, search)
		if err == nil {
			matches, identifiable := newCreationMatches(candidates, command, baseline)
			if !identifiable {
				return domain.CreatedPatient{}, MutationIndeterminateWrite
			}
			if len(matches) == 1 {
				return p.loadCreatedPatient(ctx, matches[0])
			}
			if len(matches) > 1 {
				return domain.CreatedPatient{}, MutationIndeterminateWrite
			}
		} else if !isTransientReadError(err) {
			return domain.CreatedPatient{}, MutationIndeterminateWrite
		}

		if attempt == maxReadAttempts {
			return domain.CreatedPatient{}, MutationIndeterminateWrite
		}
		if err := waitReadRetry(ctx, attempt); err != nil {
			return domain.CreatedPatient{}, MutationIndeterminateWrite
		}
	}
	return domain.CreatedPatient{}, MutationIndeterminateWrite
}

func newCreationMatches(
	candidates []domain.Patient,
	command CreateCommand,
	baseline map[string]struct{},
) ([]domain.Patient, bool) {
	matches := make([]domain.Patient, 0, 1)
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		id := domain.StripPatientPrefix(candidate.ID)
		if id == "" {
			return nil, false
		}
		if !creationMatch(candidate, command) {
			continue
		}
		if _, existed := baseline[id]; existed {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		matches = append(matches, candidate)
	}
	return matches, true
}

func (p *patient) loadCreatedPatient(ctx context.Context, match domain.Patient) (domain.CreatedPatient, MutationOutcome) {
	demographics, err := retryRead(ctx, func() (domain.PatientDemographics, error) {
		return p.advancedMD.GetPatientDemographics(ctx, match.ID)
	})
	if err != nil || demographics.RespPartyID == "" {
		return domain.CreatedPatient{}, MutationIndeterminateWrite
	}
	return domain.CreatedPatient{
		ID:          domain.StripPatientPrefix(match.ID),
		RespPartyID: demographics.RespPartyID,
		Name:        match.FullName,
	}, ""
}

func creationMatch(candidate domain.Patient, command CreateCommand) bool {
	firstName := candidate.FirstName
	if firstName == "" {
		firstName = domain.ParseFirstName(candidate.FullName)
	}
	lastName := candidate.LastName
	if lastName == "" {
		lastName = strings.TrimSpace(strings.SplitN(candidate.FullName, ",", 2)[0])
	}
	return strings.EqualFold(domain.StripDiacritics(firstName), domain.StripDiacritics(command.FirstName)) &&
		strings.EqualFold(domain.StripDiacritics(lastName), domain.StripDiacritics(command.LastName)) &&
		domain.NormalizeDOB(candidate.DOB) == domain.NormalizeDOB(command.DOB) &&
		(candidate.Phone == "" || domain.NormalizePhoneDigits(candidate.Phone) == domain.NormalizePhoneDigits(command.Phone))
}

func retryRead[T any](ctx context.Context, read func() (T, error)) (T, error) {
	var zero T
	for attempt := 1; attempt <= maxReadAttempts; attempt++ {
		result, err := read()
		if err == nil {
			return result, nil
		}
		if attempt == maxReadAttempts || !isTransientReadError(err) {
			return zero, err
		}

		if err := waitReadRetry(ctx, attempt); err != nil {
			return zero, err
		}
	}
	return zero, errors.New("patient read retry exhausted")
}

func waitReadRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(readRetryDelay * time.Duration(attempt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isTransientReadError(err error) bool {
	switch advancedmd.CategoryOf(err) {
	case safeerrors.CategoryTimeout, safeerrors.CategoryNetwork, safeerrors.CategoryUnavailable:
		return true
	default:
		return false
	}
}

func failureOutcome(err error) MutationOutcome {
	switch advancedmd.CategoryOf(err) {
	case safeerrors.CategoryUnavailable, safeerrors.CategoryAuthentication:
		return MutationUnavailable
	default:
		return MutationFailed
	}
}

func createOutcome(result CreateResult) string {
	if result.Outcome != "" {
		return string(result.Outcome)
	}
	if result.Status == CreateStatusCreated {
		return "success"
	}
	return "failed"
}

func updateInsuranceOutcome(result UpdateInsuranceResult) string {
	if result.Outcome != "" {
		return string(result.Outcome)
	}
	if result.Status == UpdateInsuranceStatusUpdated {
		return "success"
	}
	return "failed"
}

func recordMutation(operation, outcome string) {
	log.Printf("patient-mutation operation=%s category=%s", operation, outcome)

	mutationMetrics.Lock()
	defer mutationMetrics.Unlock()
	mutationMetrics.counts[mutationMetricKey{operation: operation, outcome: outcome}]++
}

func MutationMetricSnapshot() []MutationMetric {
	mutationMetrics.Lock()
	defer mutationMetrics.Unlock()

	snapshot := make([]MutationMetric, 0, len(mutationMetrics.counts))
	for key, count := range mutationMetrics.counts {
		snapshot = append(snapshot, MutationMetric{
			Operation: key.operation,
			Outcome:   key.outcome,
			Count:     count,
		})
	}
	sort.Slice(snapshot, func(i, j int) bool {
		if snapshot[i].Operation == snapshot[j].Operation {
			return snapshot[i].Outcome < snapshot[j].Outcome
		}
		return snapshot[i].Operation < snapshot[j].Operation
	})
	return snapshot
}

func (p *patient) Resolve(ctx context.Context, command ResolveCommand) (ResolveResult, error) {
	office, ok := domain.LookupOfficeByID(command.OfficeID)
	if !ok {
		return ResolveResult{}, advancedmd.NewError(safeerrors.CategoryInternal)
	}
	if command.PatientID != "" {
		return p.resolvePatient(ctx, domain.Patient{ID: command.PatientID}, "", office)
	}

	search := patientSearch(command)
	searchStarted := time.Now()
	patients, err := p.advancedMD.SearchPatients(ctx, search)
	observation := ResolutionObservation{
		Recorded:                true,
		PatientSearchDurationMS: time.Since(searchStarted).Milliseconds(),
		PatientSearchReads:      1,
		OfficeGroupSize:         len(domain.AppointmentLookupOfficeIDs(office)),
		AppointmentOutcome:      "not_requested",
	}
	if err != nil {
		observation.CandidateCountBucket = "unknown"
		return ResolveResult{Observation: observation}, err
	}
	matches := selectPatients(patients, command)
	observation.CandidateCountBucket = candidateCountBucket(len(matches))
	if len(matches) == 0 {
		return ResolveResult{
			Status:       StatusNotFound,
			Appointments: []Appointment{},
			Message:      notFoundMessage(command),
			Observation:  observation,
		}, nil
	}
	if len(matches) > 1 {
		results := make([]Candidate, 0, len(matches))
		for _, candidate := range matches {
			results = append(results, Candidate{
				Status:    StatusCandidate,
				PatientID: candidate.ID,
				FirstName: patientFirstName(candidate),
				LastName:  patientLastName(candidate),
				DOB:       candidate.DOB,
			})
		}
		observation.AppointmentOutcome = "deferred"
		return ResolveResult{
			Status:       StatusMultipleMatches,
			Appointments: []Appointment{},
			Matches:      results,
			Message:      multipleMatchesMessage(command, len(results)),
			Observation:  observation,
		}, nil
	}

	result, err := p.resolvePatient(ctx, matches[0], command.Phone, office)
	result.Observation.PatientSearchDurationMS = observation.PatientSearchDurationMS
	result.Observation.PatientSearchReads = observation.PatientSearchReads
	result.Observation.CandidateCountBucket = observation.CandidateCountBucket
	return result, err
}

func patientSearch(command ResolveCommand) domain.PatientSearch {
	if command.Phone != "" {
		return domain.PatientSearch{Phone: domain.NormalizePhoneDigits(command.Phone)}
	}
	return domain.PatientSearch{
		FirstName: domain.StripDiacritics(command.FirstName),
		LastName:  domain.StripDiacritics(command.LastName),
	}
}

func selectPatients(patients []domain.Patient, command ResolveCommand) []domain.Patient {
	matches := patients
	if command.DOB != "" {
		normalizedDOB := domain.NormalizeDOB(command.DOB)
		matches = nil
		for _, candidate := range patients {
			if domain.NormalizeDOB(candidate.DOB) == normalizedDOB {
				matches = append(matches, candidate)
			}
		}
	}

	if command.FirstName == "" {
		return matches
	}
	firstNameMatches := make([]domain.Patient, 0, len(matches))
	for _, candidate := range matches {
		candidateFirstName := strings.ToUpper(domain.StripDiacritics(candidate.FirstName))
		requestFirstName := strings.ToUpper(domain.StripDiacritics(command.FirstName))
		if strings.HasPrefix(candidateFirstName, requestFirstName) {
			firstNameMatches = append(firstNameMatches, candidate)
		}
	}
	return firstNameMatches
}

func (p *patient) resolvePatient(ctx context.Context, candidate domain.Patient, lookupPhone string, office *domain.OfficeConfig) (ResolveResult, error) {
	officeIDs := domain.AppointmentLookupOfficeIDs(office)
	result := ResolveResult{
		Status:       StatusVerified,
		PatientID:    candidate.ID,
		Name:         candidate.FullName,
		DOB:          candidate.DOB,
		Phone:        firstNonEmpty(candidate.Phone, lookupPhone),
		Appointments: []Appointment{},
		Observation: ResolutionObservation{
			Recorded:             true,
			DemographicReads:     1,
			OfficeGroupSize:      len(officeIDs),
			CandidateCountBucket: "1",
			AppointmentOutcome:   "not_requested",
		},
	}

	type demographicsResult struct {
		demographics domain.PatientDemographics
		err          error
		durationMS   int64
	}
	type appointmentsResult struct {
		read       advancedmd.AppointmentRead
		err        error
		durationMS int64
	}
	demographicsResults := make(chan demographicsResult, 1)
	appointmentsResults := make(chan appointmentsResult, 1)
	go func() {
		started := time.Now()
		demographics, err := p.advancedMD.GetPatientDemographics(ctx, candidate.ID)
		demographicsResults <- demographicsResult{
			demographics: demographics,
			err:          err,
			durationMS:   time.Since(started).Milliseconds(),
		}
	}()
	go func() {
		started := time.Now()
		read, err := p.advancedMD.ReadPatientAppointments(ctx, domain.PatientAppointmentsQuery{
			PatientID: candidate.ID,
			OfficeIDs: officeIDs,
		})
		appointmentsResults <- appointmentsResult{
			read:       read,
			err:        err,
			durationMS: time.Since(started).Milliseconds(),
		}
	}()

	demographicsRead := <-demographicsResults
	appointmentsRead := <-appointmentsResults
	result.Observation.DemographicDurationMS = demographicsRead.durationMS
	result.Observation.AppointmentDurationMS = appointmentsRead.durationMS
	result.Observation.AppointmentReads = appointmentsRead.read.ProviderReads
	result.Observation.AppointmentOutcome = appointmentLoadOutcome(
		appointmentsRead.read.Appointments,
		appointmentsRead.err,
	)
	if demographicsRead.err != nil {
		category := advancedmd.CategoryOf(demographicsRead.err)
		result.ProviderFailure = category
		log.Printf("patient-resolve: failed to get demographics category=%s", category)
		if category == safeerrors.CategoryUnavailable {
			return result, demographicsRead.err
		}
	} else {
		if result.DOB == "" {
			result.DOB = demographicsRead.demographics.DOB
		}
		if result.Name == "" {
			result.Name = demographicsRead.demographics.FullName
		}
		applyDemographics(&result, demographicsRead.demographics, office, result.DOB)
	}

	if appointmentsRead.err != nil {
		result.ProviderFailure = advancedmd.CategoryOf(appointmentsRead.err)
		log.Printf("patient-resolve: failed to get appointments category=%s", result.ProviderFailure)
		result.AppointmentsStatus = AppointmentsError
		result.AppointmentsMessage = "Failed to retrieve appointments from AdvancedMD. Please try again."
		result.Message = "Patient verified, appointment lookup unavailable"
		return result, nil
	}

	for _, appointment := range appointmentsRead.read.Appointments {
		cancellationToken := ""
		rescheduleToken := ""
		if p.appointmentTokens != nil {
			var tokenErr error
			cancellationToken, tokenErr = p.appointmentTokens.IssueCancellationToken(candidate.ID, appointment)
			if tokenErr == nil {
				rescheduleToken, tokenErr = p.appointmentTokens.IssueRescheduleToken(candidate.ID, appointment)
			}
			if tokenErr != nil {
				result.Appointments = []Appointment{}
				result.AppointmentsStatus = AppointmentsError
				result.AppointmentsMessage = "Failed to prepare appointments for cancellation. Please try again."
				result.Message = "Patient verified, appointment lookup unavailable"
				result.Observation.AppointmentOutcome = "error"
				return result, nil
			}
		}
		result.Appointments = append(result.Appointments, Appointment{
			ID:                appointment.ID,
			Date:              appointment.Start.Format("Monday, January 2, 2006"),
			Time:              appointment.Start.Format("3:04 PM"),
			Provider:          appointment.Provider,
			Type:              appointment.Type,
			AppointmentTypeID: appointment.AppointmentTypeID,
			Facility:          appointment.Facility,
			OfficeID:          appointment.OfficeID,
			Office:            appointment.Office,
			CancellationToken: cancellationToken,
			RescheduleToken:   rescheduleToken,
		})
	}
	if len(result.Appointments) == 0 {
		result.AppointmentsStatus = AppointmentsNone
		result.Message = "Patient verified, no appointments found"
		return result, nil
	}

	result.AppointmentsStatus = AppointmentsFound
	result.Message = fmt.Sprintf("Patient verified with %d appointment(s)", len(result.Appointments))
	return result, nil
}

func candidateCountBucket(count int) string {
	switch {
	case count == 0:
		return "0"
	case count == 1:
		return "1"
	case count <= 5:
		return "2-5"
	default:
		return "6+"
	}
}

func appointmentLoadOutcome(appointments []domain.PatientAppointment, err error) string {
	if err != nil {
		return "error"
	}
	if len(appointments) == 0 {
		return "none"
	}
	return "found"
}

func patientFirstName(candidate domain.Patient) string {
	if candidate.FirstName != "" {
		return candidate.FirstName
	}
	return domain.ParseFirstName(candidate.FullName)
}

func patientLastName(candidate domain.Patient) string {
	if candidate.LastName != "" {
		return candidate.LastName
	}
	return strings.TrimSpace(strings.SplitN(candidate.FullName, ",", 2)[0])
}

func applyDemographics(result *ResolveResult, demographics domain.PatientDemographics, office *domain.OfficeConfig, patientDOB string) {
	policy := domain.NewSchedulingPolicy(office)
	result.InsuranceCarrier = demographics.CarrierName
	result.InsPlanID = demographics.InsPlanID
	result.RespPartyID = demographics.RespPartyID

	if demographics.CarrierID == "" {
		return
	}

	result.InsuranceCarrierID = demographics.CarrierID
	routing, ambiguous := domain.RoutingForDemographicInsurance(demographics.CarrierID, demographics.CarrierName, office)
	routing = policy.PatientRouting(routing, patientDOB)
	result.Routing = routing
	result.AllowedProviders = policy.ProviderNames(routing, patientDOB)
	result.RoutingAmbiguous = ambiguous
	if entry, ok := domain.LookupInsuranceForCoverageAtOffice(demographics.CarrierName, domain.InsuranceModeMedical, office); ok {
		result.PreauthRequired = entry.PreauthRequired
	}
	if domain.IsMinor(patientDOB) && routing != domain.RoutingNotAccepted {
		result.RoutingAmbiguous = false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func notFoundMessage(command ResolveCommand) string {
	if command.Phone != "" && command.FirstName == "" && command.DOB == "" {
		return "No patient found for that phone number"
	}
	if command.FirstName != "" {
		return "No patient found matching that first name"
	}
	return "No patient found matching the provided information"
}

func multipleMatchesMessage(command ResolveCommand, count int) string {
	if command.Phone != "" && command.FirstName != "" && command.DOB == "" {
		return fmt.Sprintf("Found %d patients with that name and phone number. Please provide date of birth.", count)
	}
	if command.Phone != "" {
		return fmt.Sprintf("Found %d patients for this phone number. Ask the caller to confirm their name.", count)
	}
	return fmt.Sprintf("Found %d patients with that last name and DOB. Please provide first name.", count)
}
