package patient

import (
	"context"
	"strings"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

func usableDOB(dob string, asOf time.Time) bool {
	return strings.TrimSpace(dob) != "" && domain.ValidateOptionalDOB(dob, asOf) == nil
}

func (p *patient) resolveFirstNameDOB(ctx context.Context, query ResolveQuery, office *domain.OfficeConfig) (ResolveResult, error) {
	started := time.Now()
	read, err := p.advancedMD.ReadPatientCandidates(ctx, string(query.FirstName), domain.NormalizeDOB(query.DOB))
	observation := ResolutionObservation{
		Recorded: true, PatientSearchReads: 1,
		PatientSearchDurationMS: time.Since(started).Milliseconds(),
		OfficeGroupSize:         len(office.AppointmentLookupOfficeIDs),
		CandidateCountBucket:    candidateCountBucket(len(read.Patients)),
		AppointmentOutcome:      "not_requested",
	}
	unresolved := func(reason string, failure safeerrors.Category) (ResolveResult, error) {
		return ResolveResult{
			Status: StatusUnresolved, Reason: reason, ProviderFailure: failure,
			Message:      "Patient identity could not be verified. Ask office staff for help.",
			Appointments: []Appointment{}, Observation: observation,
		}, nil
	}
	if err != nil {
		return unresolved("search_unavailable", advancedmd.CategoryOf(err))
	}
	observation.PatientSearchComplete = new(read.Complete)
	if !read.Complete {
		return unresolved("incomplete_search", safeerrors.CategoryInvalidResponse)
	}
	dob := domain.NormalizeDOB(query.DOB)
	now := p.now()
	if query.FirstName.Key() == "" {
		return unresolved("invalid_identity", safeerrors.CategoryRejected)
	}
	matches := []domain.Patient{}
	seen := map[string]bool{}
	for _, row := range read.Patients {
		if !row.Name().MatchesFirstName(query.FirstName) || !usableDOB(row.DOB, now) || domain.NormalizeDOB(row.DOB) != dob {
			continue
		}
		if row.ID == "" || seen[row.ID] {
			return unresolved("invalid_candidate_set", safeerrors.CategoryInvalidResponse)
		}
		seen[row.ID] = true
		matches = append(matches, row)
	}
	observation.CandidateCountBucket = candidateCountBucket(len(matches))
	if len(matches) == 0 {
		return ResolveResult{Status: StatusNotFound, Appointments: []Appointment{}, Message: "No patient matches this first name and date of birth.", Observation: observation}, nil
	}
	if len(matches) > 1 {
		candidates := make([]Candidate, 0, len(matches))
		for _, row := range matches {
			name := row.Name()
			candidates = append(candidates, Candidate{Status: statusCandidate, PatientID: row.ID, FirstName: name.First(), LastName: name.Last, DOB: domain.NormalizeDOB(row.DOB)})
		}
		return ResolveResult{Status: StatusMultipleMatches, Matches: candidates, Appointments: []Appointment{}, Message: "More than one patient matches this first name and date of birth. Ask office staff to resolve the identity.", Observation: observation}, nil
	}
	selected := matches[0]
	demographicsStarted := time.Now()
	demographics, err := p.advancedMD.GetPatientDemographics(ctx, selected.ID)
	observation.DemographicReads = 1
	observation.DemographicDurationMS = time.Since(demographicsStarted).Milliseconds()
	if err != nil {
		return unresolved("demographics_unavailable", advancedmd.CategoryOf(err))
	}
	if demographics.FullName != "" {
		if !domain.ParseRecordName(demographics.FullName).MatchesFirstName(query.FirstName) {
			return unresolved("identity_not_verified", safeerrors.CategoryInvalidResponse)
		}
		selected.FullName = demographics.FullName
	}
	if demographics.DOB != "" && (!usableDOB(demographics.DOB, now) || domain.NormalizeDOB(demographics.DOB) != dob) {
		return unresolved("identity_not_verified", safeerrors.CategoryInvalidResponse)
	}
	result, err := p.resolvePatientWithDemographics(ctx, selected, "", office, &demographics)
	result.Observation.PatientSearchReads = observation.PatientSearchReads
	result.Observation.PatientSearchDurationMS = observation.PatientSearchDurationMS
	result.Observation.DemographicReads += observation.DemographicReads
	result.Observation.DemographicDurationMS += observation.DemographicDurationMS
	result.Observation.PatientSearchComplete = observation.PatientSearchComplete
	return result, err
}
