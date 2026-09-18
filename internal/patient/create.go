package patient

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
)

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
	coverage := command.CoverageType
	if coverage == "" {
		coverage = "medical"
	}
	decision := domain.DecideInsurance(command.Insurance, coverage, office, command.DOB)
	if decision.Participation != "accepted" {
		return CreateResult{Status: CreateStatusError, Outcome: MutationValidationFailed, Message: decision.Answer}
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
		CarrierID:     decision.CarrierID,
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

	result = CreateResult{
		Status:            CreateStatusCreated,
		PatientID:         created.ID,
		Name:              created.Name,
		DOB:               domain.NormalizeDOB(command.DOB),
		Routing:           decision.Routing,
		AllowedProviders:  decision.AllowedProviders,
		PreauthRequired:   len(decision.Requirements) > 0,
		InsuranceDecision: &decision,
		Message:           "Patient created and insurance attached successfully",
	}
	if createReconciled || insuranceReconciled {
		result.Outcome = MutationReconciledSuccess
	}
	return result
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
