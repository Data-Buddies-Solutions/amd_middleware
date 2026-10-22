package patient

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
	"advancedmd-token-management/internal/safeerrors"
)

const appointmentReserve = time.Second

func (p *patient) Resolve(ctx context.Context, query ResolveQuery) (ResolveResult, error) {
	office, ok := domain.LookupOfficeByID(query.OfficeID)
	if !ok {
		return ResolveResult{}, advancedmd.NewError(safeerrors.CategoryInternal)
	}
	if query.PatientID != "" {
		return p.resolvePatient(ctx, domain.Patient{ID: string(query.PatientID)}, "", office)
	}

	if query.Phone == "" && query.LastName == "" && query.FirstName != "" && query.DOB != "" {
		return p.resolveFirstNameDOB(ctx, query, office)
	}

	search := patientSearch(query)
	searchStarted := time.Now()
	read, err := p.advancedMD.SearchPatients(ctx, search)
	observation := ResolutionObservation{
		Recorded:                true,
		PatientSearchDurationMS: time.Since(searchStarted).Milliseconds(),
		PatientSearchReads:      1,
		OfficeGroupSize:         len(office.AppointmentLookupOfficeIDs),
		AppointmentOutcome:      "not_requested",
	}
	if err != nil {
		observation.CandidateCountBucket = "unknown"
		return ResolveResult{Observation: observation}, err
	}
	observation.PatientSearchComplete = new(read.Complete)
	matches := selectPatients(read.Patients, query)
	observation.CandidateCountBucket = candidateCountBucket(len(matches))
	if !read.Complete {
		return unverifiedPatient(observation, "incomplete_search", safeerrors.CategoryInvalidResponse), nil
	}
	if len(matches) == 0 {
		return ResolveResult{
			Status:       StatusNotFound,
			Appointments: []Appointment{},
			Message:      notFoundMessage(query),
			Observation:  observation,
		}, nil
	}
	if len(matches) > 1 {
		results := make([]Candidate, 0, len(matches))
		for _, candidate := range matches {
			name := candidate.Name()
			results = append(results, Candidate{
				Status:    statusCandidate,
				PatientID: candidate.ID,
				FirstName: name.Given,
				LastName:  name.Last,
				DOB:       candidate.DOB,
			})
		}
		observation.AppointmentOutcome = "deferred"
		return ResolveResult{
			Status:       StatusMultipleMatches,
			Appointments: []Appointment{},
			Matches:      results,
			Message:      multipleMatchesMessage(query, len(results)),
			Observation:  observation,
		}, nil
	}

	result, err := p.resolvePatient(ctx, matches[0], query.Phone, office)
	result.Observation.PatientSearchDurationMS = observation.PatientSearchDurationMS
	result.Observation.PatientSearchReads = observation.PatientSearchReads
	result.Observation.CandidateCountBucket = observation.CandidateCountBucket
	result.Observation.PatientSearchComplete = observation.PatientSearchComplete
	return result, err
}

func patientSearch(query ResolveQuery) domain.PatientSearch {
	if query.Phone != "" {
		return domain.PatientSearch{Phone: string(query.Phone)}
	}
	return domain.PatientSearch{
		FirstName: string(query.FirstName),
		LastName:  string(query.LastName),
	}
}

func selectPatients(patients []domain.Patient, query ResolveQuery) []domain.Patient {
	matches := patients
	if query.DOB != "" {
		matches = nil
		for _, candidate := range patients {
			if query.DOB.Matches(candidate.DOB) {
				matches = append(matches, candidate)
			}
		}
	}

	if query.FirstName == "" {
		return matches
	}
	firstNameMatches := make([]domain.Patient, 0, len(matches))
	for _, candidate := range matches {
		if candidate.Name().GivenStartsWith(query.FirstName) {
			firstNameMatches = append(firstNameMatches, candidate)
		}
	}
	return firstNameMatches
}

func (p *patient) resolvePatient(ctx context.Context, candidate domain.Patient, lookupPhone domain.Phone, office *domain.OfficeConfig) (ResolveResult, error) {
	return p.resolvePatientWithDemographics(ctx, candidate, lookupPhone, office, nil)
}

func (p *patient) resolvePatientWithDemographics(ctx context.Context, candidate domain.Patient, lookupPhone domain.Phone, office *domain.OfficeConfig, loaded *domain.PatientDemographics) (ResolveResult, error) {
	officeIDs := office.AppointmentLookupOfficeIDs
	result := ResolveResult{
		Status:       StatusVerified,
		PatientID:    candidate.ID,
		Name:         candidate.FullName,
		DOB:          candidate.DOB,
		Phone:        firstNonEmpty(candidate.Phone, string(lookupPhone)),
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
	appointmentsCtx, cancelAppointments := withAppointmentBudget(ctx)
	defer cancelAppointments()
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
		read, err := p.advancedMD.ReadPatientAppointments(appointmentsCtx, domain.PatientAppointmentsQuery{
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
		log.Printf("patient-resolve: failed to get demographics category=%s", category)
		if category == safeerrors.CategoryUnavailable {
			result.ProviderFailure = category
			return result, demographicsRead.err
		}
		return unverifiedPatient(result.Observation, "demographics_failed", category), nil
	}
	if result.DOB == "" {
		result.DOB = demographicsRead.demographics.DOB
	}
	if result.Name == "" {
		result.Name = demographicsRead.demographics.FullName
	}
	if result.Name == "" || result.DOB == "" {
		return unverifiedPatient(result.Observation, "incomplete_identity", safeerrors.CategoryInvalidResponse), nil
	}
	if !demographicsRead.demographics.InsuranceStateKnown {
		return unverifiedPatient(result.Observation, "insurance_unknown", safeerrors.CategoryInvalidResponse), nil
	}
	applyDemographics(&result, demographicsRead.demographics, office, result.DOB, p.now())

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
		cancellationToken, tokenErr := p.appointmentTokens.IssueCancellationToken(candidate.ID, appointment)
		rescheduleToken := ""
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
		result.AppointmentsStatus = appointmentsNone
		result.Message = "Patient verified, no appointments found"
		return result, nil
	}

	result.AppointmentsStatus = AppointmentsFound
	result.Message = fmt.Sprintf("Patient verified with %d appointment(s)", len(result.Appointments))
	return result, nil
}

func withAppointmentBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, bounded := ctx.Deadline()
	if !bounded {
		return context.WithCancel(ctx)
	}
	return context.WithDeadline(ctx, deadline.Add(-appointmentReserve))
}

func unverifiedPatient(observation ResolutionObservation, reason string, failure safeerrors.Category) ResolveResult {
	return ResolveResult{
		Status:          StatusUnresolved,
		Reason:          reason,
		ProviderFailure: failure,
		Message:         "Patient identity could not be verified. Ask office staff for help.",
		Appointments:    []Appointment{},
		Observation:     observation,
	}
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

func applyDemographics(result *ResolveResult, demographics domain.PatientDemographics, office *domain.OfficeConfig, patientDOB string, asOf time.Time) {
	result.InsuranceCarrier = demographics.CarrierName
	result.InsPlanID = demographics.InsPlanID
	result.RespPartyID = demographics.RespPartyID
	result.InsuranceCarrierID = demographics.CarrierID
	if demographics.CarrierID == "" {
		return
	}
	decision := insurance.DecideChartInsurance(demographics, "medical", office, patientDOB, asOf)
	result.InsuranceDecision = &decision
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

func notFoundMessage(query ResolveQuery) string {
	if query.Phone != "" && query.FirstName == "" && query.DOB == "" {
		return "No patient found for that phone number"
	}
	if query.FirstName != "" {
		return "No patient found matching that first name"
	}
	return "No patient found matching the provided information"
}

func multipleMatchesMessage(query ResolveQuery, count int) string {
	if query.Phone != "" && query.FirstName != "" && query.DOB == "" {
		return fmt.Sprintf("Found %d patients with that name and phone number. Please provide date of birth.", count)
	}
	if query.Phone != "" {
		return fmt.Sprintf("Found %d patients for this phone number. Ask the caller to confirm their name.", count)
	}
	return fmt.Sprintf("Found %d patients with that last name and DOB. Please provide first name.", count)
}

func (c ResolveCommand) Parse(officeID string, asOf time.Time) (ResolveQuery, string) {
	hasLookupFields := c.Phone != "" || c.FirstName != "" || c.LastName != "" || c.DOB != ""
	if c.PatientID != "" {
		id, err := domain.ParsePatientID(c.PatientID)
		if err != nil || id == "" {
			return ResolveQuery{}, "patientId must be numeric"
		}
		if hasLookupFields {
			return ResolveQuery{}, "Provide either patientId or lookup fields, not both"
		}
		return ResolveQuery{PatientID: id, OfficeID: officeID}, ""
	}
	phone, err := domain.ParsePhone(c.Phone)
	if err != nil {
		return ResolveQuery{}, err.Error()
	}
	if phone == "" && (c.FirstName == "" && c.LastName == "" || c.DOB == "") {
		return ResolveQuery{}, "Provide patientId, phone, phone + firstName, phone + dob, firstName + dob, or lastName + dob"
	}
	dob, err := domain.ParseDOB(c.DOB, asOf)
	if err != nil {
		return ResolveQuery{}, err.Error()
	}
	return ResolveQuery{
		Phone:     phone,
		FirstName: domain.NewPersonName(c.FirstName),
		LastName:  domain.NewPersonName(c.LastName),
		DOB:       dob,
		OfficeID:  officeID,
	}, ""
}
