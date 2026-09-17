package patient

import (
	"context"
	"strings"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

func exactFirstName(name string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		return -1
	}, strings.ToLower(domain.StripDiacritics(name)))
}

func candidateName(row domain.Patient) string {
	if row.FirstName != "" {
		return row.FirstName
	}
	parts := strings.Fields(domain.ParseFirstName(row.FullName))
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func usableDOB(dob string) bool {
	return strings.TrimSpace(dob) != "" && domain.ValidateOptionalDOB(dob) == nil
}

func (p *patient) resolveFirstNameDOB(ctx context.Context, command ResolveCommand, office *domain.OfficeConfig) (ResolveResult, error) {
	started := time.Now()
	read, err := p.advancedMD.ReadPatientCandidates(ctx, domain.StripDiacritics(command.FirstName))
	observation := ResolutionObservation{
		Recorded: true, PatientSearchReads: 1,
		PatientSearchDurationMS: time.Since(started).Milliseconds(),
		OfficeGroupSize:         len(domain.AppointmentLookupOfficeIDs(office)),
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
	if !read.Complete {
		return unresolved("incomplete_search", safeerrors.CategoryInvalidResponse)
	}
	name, dob := exactFirstName(command.FirstName), domain.NormalizeDOB(command.DOB)
	if name == "" {
		return unresolved("invalid_identity", safeerrors.CategoryRejected)
	}
	matches := []domain.Patient{}
	seen := map[string]bool{}
	for _, row := range read.Patients {
		// Missing or invalid DOB is not a match, regardless of the first name.
		if exactFirstName(candidateName(row)) != name || !usableDOB(row.DOB) || domain.NormalizeDOB(row.DOB) != dob {
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
			candidates = append(candidates, Candidate{Status: StatusCandidate, PatientID: row.ID, FirstName: candidateName(row), LastName: patientLastName(row), DOB: domain.NormalizeDOB(row.DOB)})
		}
		return ResolveResult{Status: StatusMultipleMatches, Matches: candidates, Appointments: []Appointment{}, Message: "More than one patient matches this first name and date of birth. Ask office staff to resolve the identity.", Observation: observation}, nil
	}
	selected := matches[0]
	started = time.Now()
	demographics, err := p.advancedMD.GetPatientDemographics(ctx, selected.ID)
	observation.DemographicReads = 1
	observation.DemographicDurationMS = time.Since(started).Milliseconds()
	if err != nil {
		return unresolved("demographics_unavailable", advancedmd.CategoryOf(err))
	}
	// Keep the matched lookup identity when optional demographic fields are absent;
	// an authoritative conflicting identity cannot replace it silently.
	if demographics.FullName != "" {
		if exactFirstName(candidateName(domain.Patient{FullName: demographics.FullName})) != name {
			return unresolved("identity_not_verified", safeerrors.CategoryInvalidResponse)
		}
		selected.FullName = demographics.FullName
	}
	if demographics.DOB != "" && (!usableDOB(demographics.DOB) || domain.NormalizeDOB(demographics.DOB) != dob) {
		return unresolved("identity_not_verified", safeerrors.CategoryInvalidResponse)
	}
	result, err := p.resolvePatientWithDemographics(ctx, selected, "", office, &demographics)
	result.Observation.PatientSearchReads = observation.PatientSearchReads
	result.Observation.PatientSearchDurationMS = observation.PatientSearchDurationMS
	result.Observation.DemographicReads += observation.DemographicReads
	result.Observation.DemographicDurationMS += observation.DemographicDurationMS
	return result, err
}
