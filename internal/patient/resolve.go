package patient

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

func (p *patient) Resolve(ctx context.Context, command ResolveCommand) (ResolveResult, error) {
	office, ok := domain.LookupOfficeByID(command.OfficeID)
	if !ok {
		return ResolveResult{}, advancedmd.NewError(safeerrors.CategoryInternal)
	}
	if command.PatientID != "" {
		return p.resolvePatient(ctx, domain.Patient{ID: command.PatientID}, "", office)
	}

	if command.Phone == "" && command.LastName == "" && command.FirstName != "" && command.DOB != "" {
		if err := domain.ValidateOptionalDOB(command.DOB); err != nil {
			return ResolveResult{}, advancedmd.NewError(safeerrors.CategoryRejected)
		}
		return p.resolveFirstNameDOB(ctx, command, office)
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
	return p.resolvePatientWithDemographics(ctx, candidate, lookupPhone, office, nil)
}

func (p *patient) resolvePatientWithDemographics(ctx context.Context, candidate domain.Patient, lookupPhone string, office *domain.OfficeConfig, loaded *domain.PatientDemographics) (ResolveResult, error) {
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
		var demographics domain.PatientDemographics
		var err error
		if loaded != nil {
			demographics = *loaded
		} else {
			demographics, err = p.advancedMD.GetPatientDemographics(ctx, candidate.ID)
		}
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

	if loaded != nil {
		result.Observation.DemographicReads = 0
	}
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

	if !appointmentsRead.read.Complete {
		result.ProviderFailure = safeerrors.CategoryInvalidResponse
		result.AppointmentsStatus = AppointmentsError
		result.AppointmentsMessage = "Appointment data was incomplete. Please load appointments again."
		result.Message = "Patient verified, appointment lookup unavailable"
		result.Observation.AppointmentOutcome = "incomplete"
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
			VisitType:         domain.AppointmentVisitType(appointment.AppointmentTypeID),
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
	result.InsuranceCarrier = demographics.CarrierName
	result.InsPlanID = demographics.InsPlanID
	result.RespPartyID = demographics.RespPartyID
	result.InsuranceCarrierID = demographics.CarrierID
	if demographics.CarrierID == "" {
		return
	}
	decision := domain.DecideChartInsurance(demographics, "", "medical", office, patientDOB)
	result.InsuranceDecision = &decision
	result.Routing = decision.Routing
	result.AllowedProviders = decision.AllowedProviders
	result.RoutingAmbiguous = decision.Participation == "unknown"
	result.PreauthRequired = len(decision.Requirements) > 0
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
