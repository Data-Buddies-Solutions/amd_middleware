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
	var observation CreateObservation
	defer func() {
		result.Observation = observation
		recordMutation("create", mutationLabel(result.Outcome, result.Status == CreateStatusCreated))
	}()

	office, err := domain.ResolveOffice(command.Office)
	if err != nil {
		return CreateResult{Status: CreateStatusError, Outcome: MutationValidationFailed, Message: err.Error()}
	}
	now := p.now()
	dob, dobErr := domain.ParseDOB(command.DOB, now)
	decision := decideInsurance(command.Insurance, command.InsurancePlanID, command.CoverageType, office, dob, now)
	command.SubscriberNum = subscriberNumber(decision.SelfPay, command.SubscriberNum)
	if missing := createMissingFields(command); len(missing) > 0 {
		return CreateResult{
			Status:  CreateStatusError,
			Outcome: MutationValidationFailed,
			Message: fmt.Sprintf("Missing required fields: %s", strings.Join(missing, ", ")),
		}
	}
	if dobErr != nil {
		return CreateResult{Status: CreateStatusError, Outcome: MutationValidationFailed, Message: dobErr.Error()}
	}
	phone, err := domain.ParsePhone(command.Phone)
	if err != nil {
		return CreateResult{Status: CreateStatusError, Outcome: MutationValidationFailed, Message: err.Error()}
	}
	if decision.Participation != "accepted" {
		return CreateResult{Status: CreateStatusError, Outcome: MutationValidationFailed, Message: decision.Answer}
	}

	identity := chartIdentity{
		firstName: domain.NewPersonName(command.FirstName),
		lastName:  domain.NewPersonName(command.LastName),
		dob:       dob,
		phone:     phone,
	}
	baseline, observation, err := p.creationBaseline(ctx, identity)
	switch {
	case errors.Is(err, errExistingPatient):
		return CreateResult{Status: CreateStatusError, Outcome: MutationValidationFailed, Message: "An existing patient chart matches this first name and date of birth for this phone number. Do not create a new chart; look up the existing patient instead."}
	case errors.Is(err, errIncompleteBaseline):
		return CreateResult{Status: CreateStatusError, Outcome: MutationFailed, Message: "No chart was created. AdvancedMD returned an incomplete patient search for this phone number, so an existing chart could not be ruled out. Ask office staff to check for an existing chart before registering this patient."}
	case err != nil:
		return CreateResult{Status: CreateStatusError, Outcome: failureOutcome(err), Message: createFailedMessage}
	}

	created, createReconciled, outcome := p.createPatient(ctx, command, identity, office, baseline)
	switch outcome {
	case MutationRejected:
		return CreateResult{Status: CreateStatusError, Outcome: outcome, Message: "AdvancedMD rejected patient creation. Please contact the office."}
	case MutationReconciledFailure:
		return CreateResult{Status: CreateStatusError, Outcome: outcome, Message: "AdvancedMD did not create the patient. Please try again or contact the office."}
	case MutationIndeterminateWrite:
		return CreateResult{Status: CreateStatusError, Outcome: outcome, Message: "Patient creation may have been applied, but the outcome could not be confirmed. Do not retry automatically; contact the office."}
	case MutationUnavailable, MutationFailed:
		return CreateResult{Status: CreateStatusError, Outcome: outcome, Message: createFailedMessage}
	}

	insuranceReconciled, outcome := p.addInsurance(ctx, domain.PatientInsurance{
		PatientID:     created.ID,
		RespPartyID:   created.RespPartyID,
		CarrierID:     decision.CarrierID,
		SubscriberNum: command.SubscriberNum,
	})
	if outcome != "" {
		partial := CreateResult{
			Status:    createStatusPartial,
			Outcome:   outcome,
			PatientID: created.ID,
			Name:      created.Name,
			DOB:       string(dob),
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
		DOB:               string(dob),
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
		{"insurance", firstNonEmpty(command.Insurance, command.InsurancePlanID)},
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

type chartIdentity struct {
	firstName domain.PersonName
	lastName  domain.PersonName
	dob       domain.DOB
	phone     domain.Phone
}

func (p *patient) createPatient(ctx context.Context, command CreateCommand, identity chartIdentity, office *domain.OfficeConfig, baseline map[string]struct{}) (domain.CreatedPatient, bool, MutationOutcome) {
	created, err := p.advancedMD.CreatePatient(ctx, domain.PatientCreate{
		FirstName: string(identity.firstName),
		LastName:  string(identity.lastName),
		DOB:       string(identity.dob),
		Phone:     formatPhone(identity.phone),
		Email:     strings.TrimSpace(command.Email),
		Street:    command.Street,
		AptSuite:  command.AptSuite,
		City:      command.City,
		State:     strings.ToUpper(command.State),
		Zip:       command.Zip,
		Sex:       normalizeSex(command.Sex),
		OfficeID:  office.ID,
	})
	if err == nil {
		return created, false, ""
	}
	switch advancedmd.MutationFailureOf(err) {
	case advancedmd.MutationRejected:
		return domain.CreatedPatient{}, false, MutationRejected
	case advancedmd.MutationAmbiguous:
		created, outcome := p.reconcileCreatedPatient(ctx, identity, baseline)
		return created, outcome == "", outcome
	default:
		return domain.CreatedPatient{}, false, failureOutcome(err)
	}
}

const createFailedMessage = "Failed to create patient in AdvancedMD. Please try again or contact the office."

var (
	errExistingPatient    = errors.New("existing patient matches first name and DOB")
	errIncompleteBaseline = errors.New("patient baseline search was incomplete")
)

func (p *patient) creationBaseline(ctx context.Context, identity chartIdentity) (map[string]struct{}, CreateObservation, error) {
	search := domain.PatientSearch{Phone: string(identity.phone)}
	read, err := retryRead(ctx, func() (domain.PatientCandidateRead, error) {
		return p.advancedMD.SearchPatients(ctx, search)
	})
	if err != nil {
		return nil, CreateObservation{}, err
	}
	complete := read.Complete
	observation := CreateObservation{BaselineSearchComplete: &complete}

	baseline := make(map[string]struct{})
	for _, candidate := range read.Patients {
		if candidate.ID == "" {
			return nil, observation, errors.New("patient reconciliation baseline contains a record without an ID")
		}
		if candidate.Name().MatchesFirstName(identity.firstName) && identity.dob.Matches(candidate.DOB) {
			return nil, observation, errExistingPatient
		}
		baseline[candidate.ID] = struct{}{}
	}
	if !complete {
		return nil, observation, errIncompleteBaseline
	}
	return baseline, observation, nil
}

func (p *patient) reconcileCreatedPatient(
	ctx context.Context,
	identity chartIdentity,
	baseline map[string]struct{},
) (domain.CreatedPatient, MutationOutcome) {
	search := domain.PatientSearch{Phone: string(identity.phone)}
	for attempt := 1; attempt <= maxReadAttempts; attempt++ {
		read, err := p.advancedMD.SearchPatients(ctx, search)
		if err == nil {
			matches, identifiable := newCreationMatches(read.Patients, identity, baseline)
			if !identifiable {
				return domain.CreatedPatient{}, MutationIndeterminateWrite
			}
			if len(matches) == 1 && read.Complete {
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
	identity chartIdentity,
	baseline map[string]struct{},
) ([]domain.Patient, bool) {
	matches := make([]domain.Patient, 0, 1)
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		if candidate.ID == "" {
			return nil, false
		}
		if !creationMatch(candidate, identity) {
			continue
		}
		if _, existed := baseline[candidate.ID]; existed {
			continue
		}
		if _, duplicate := seen[candidate.ID]; duplicate {
			continue
		}
		seen[candidate.ID] = struct{}{}
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
		ID:          match.ID,
		RespPartyID: demographics.RespPartyID,
		Name:        match.FullName,
	}, ""
}

func creationMatch(candidate domain.Patient, identity chartIdentity) bool {
	return candidate.Name().WrittenAs(identity.firstName, identity.lastName) &&
		identity.dob.Matches(candidate.DOB) &&
		(candidate.Phone == "" || identity.phone.Matches(candidate.Phone))
}

func formatPhone(phone domain.Phone) string {
	if len(phone) != 10 {
		return string(phone)
	}
	return fmt.Sprintf("(%s)%s-%s", phone[:3], phone[3:6], phone[6:])
}

func normalizeSex(sex string) string {
	switch strings.ToUpper(strings.TrimSpace(sex)) {
	case "M", "MALE":
		return "M"
	case "F", "FEMALE":
		return "F"
	default:
		return "U"
	}
}
