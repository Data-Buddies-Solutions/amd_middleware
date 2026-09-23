package eligibility

import "advancedmd-token-management/internal/domain"

// CheckedProvider ties payer evidence to the scheduling provider, not a network claim.
type CheckedProvider struct {
	ProfileID string `json:"profileId"`
	Name      string `json:"name"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	NPI       string `json:"npi"`
}

// Type 1 identities verified against CMS NPPES on 2026-09-23:
// https://npiregistry.cms.hhs.gov/api/?version=2.1&number=<npi>
// Profile IDs come from the Spring Hill medical scheduling registry.
var lichtProvider = CheckedProvider{Name: "Dr. Joseph Licht", FirstName: "Joseph", LastName: "Licht", NPI: "1497147680"}

var springHillMedicalProviders = []CheckedProvider{
	{Name: "Dr. Austin Bach", FirstName: "Austin", LastName: "Bach", NPI: "1659706588"},
	lichtProvider,
	{Name: "Dr. Noel", FirstName: "Don", LastName: "Noel", NPI: "1659998482"},
}

// The sandbox and production scheduling registries use different profile IDs.
func medicalProviders() ([]CheckedProvider, bool) {
	return checkedProviders("spring_hill", springHillMedicalProviders)
}

func checkedProviders(officeID string, identities []CheckedProvider) ([]CheckedProvider, bool) {
	office, ok := domain.LookupOfficeByID(officeID)
	if !ok {
		return nil, false
	}
	providers := append([]CheckedProvider(nil), identities...)
	for i := range providers {
		providers[i].ProfileID = ""
		for _, column := range office.Columns {
			if column.MatchKey != name(providers[i].LastName) {
				continue
			}
			if providers[i].ProfileID != "" && providers[i].ProfileID != column.ProfileID {
				return nil, false
			}
			providers[i].ProfileID = column.ProfileID
		}
		if providers[i].ProfileID == "" {
			return nil, false
		}
	}
	return providers, true
}

// Only unanimous, trusted results supply an actionable intake name and status.
// Every individual result remains available, including partial failures.
func providerConsensus(out Result) Result {
	out.Status, out.ReviewReason = "review", "provider_results_need_review"
	first := out.ProviderResults[0]
	for _, result := range out.ProviderResults {
		if (result.Status != "active" && result.Status != "inactive") || result.Status != first.Status ||
			result.Match == nil || result.Match.ReviewRequired || result.MatchedPatient == nil ||
			first.Match == nil || result.Match.Status != first.Match.Status || first.MatchedPatient == nil {
			return out
		}
		a, b := result.MatchedPatient, first.MatchedPatient
		if name(a.FirstName) != name(b.FirstName) || name(a.LastName) != name(b.LastName) ||
			a.DateOfBirth != b.DateOfBirth || identifier(a.MemberID) != identifier(b.MemberID) {
			return out
		}
	}
	out.Status, out.ReviewReason = first.Status, ""
	out.Match, out.MatchedPatient = first.Match, first.MatchedPatient
	return out
}
