package insurance

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"advancedmd-token-management/internal/domain"
)

type PlanSummary struct {
	PlanID           string   `json:"planId"`
	Label            string   `json:"label"`
	Names            []string `json:"names"`
	CarrierCode      string   `json:"carrierCode"`
	CarrierID        string   `json:"carrierId"`
	CarrierName      string   `json:"carrierName"`
	Outcome          string   `json:"outcome"`
	AllowedProviders []string `json:"allowedProviders"`
	Requirements     []string `json:"requirements"`
	CallerNotice     string   `json:"callerNotice"`
	Note             string   `json:"note"`
	AcceptedAt       []string `json:"acceptedAt"`
}

func ListPlans(coverage string, office *domain.OfficeConfig) ([]PlanSummary, bool) {
	if coverage != "medical" && coverage != "routine_vision" {
		return nil, false
	}
	summaries := []PlanSummary{}
	if !officeSupports(office, coverage) {
		return summaries, true
	}
	list := listForOffice(office.ID)
	for _, p := range list.Plans {
		if p.Coverage != coverage {
			continue
		}
		d := decidePlanAtOffice(p, office, "")
		s := PlanSummary{
			PlanID:           p.ID,
			Label:            p.Label,
			Names:            append([]string{}, p.Names...),
			CarrierCode:      p.CarrierCode,
			CarrierID:        p.CarrierID,
			CarrierName:      p.CarrierName,
			Outcome:          d.Outcome,
			AllowedProviders: d.AllowedProviders,
			Requirements:     []string{},
			CallerNotice:     p.CallerNotice,
			Note:             p.Note,
			AcceptedAt:       []string{},
		}
		for _, r := range d.Requirements {
			s.Requirements = append(s.Requirements, r.Kind)
		}
		if d.Participation != "accepted" {
			s.AcceptedAt = acceptedElsewhere(p, list, office)
		}
		summaries = append(summaries, s)
	}
	slices.SortFunc(summaries, func(a, b PlanSummary) int {
		return cmp.Or(compareLabels(a.Label, b.Label), strings.Compare(a.PlanID, b.PlanID))
	})
	return summaries, true
}

func compareLabels(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

func acceptedElsewhere(p plan, list planList, office *domain.OfficeConfig) []string {
	offices := []string{}
	for _, other := range catalog {
		if path.Dir(other.Table) != path.Dir(list.Table) {
			continue
		}
		for _, id := range other.Offices {
			o, _ := domain.LookupOfficeByID(id)
			if id != office.ID && DecidePlan(p.ID, p.Coverage, o, "").Participation == "accepted" {
				offices = append(offices, o.DisplayName)
			}
		}
	}
	return offices
}
