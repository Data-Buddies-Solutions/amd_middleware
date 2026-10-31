package insurance

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"advancedmd-token-management/internal/domain"
)

type PlanSummary struct {
	PlanID           string   `json:"planId"`
	Label            string   `json:"label"`
	Family           string   `json:"family"`
	SelfPay          bool     `json:"selfPay"`
	Names            []string `json:"names"`
	CarrierCode      string   `json:"carrierCode"`
	CarrierID        string   `json:"carrierId"`
	CarrierName      string   `json:"carrierName"`
	Outcome          string   `json:"outcome"`
	AllowedProviders []string `json:"allowedProviders"`
	Requirements     []string `json:"requirements"`
	CallerNotice     string   `json:"callerNotice"`
	AcceptedAt       []string `json:"acceptedAt"`
}

type PlanList struct {
	OfficeID          string        `json:"officeId"`
	Coverage          string        `json:"coverage"`
	CatalogRevisionID string        `json:"catalogRevisionId"`
	Plans             []PlanSummary `json:"plans"`
}

func ListPlans(coverage string, office *domain.OfficeConfig) (PlanList, bool) {
	if coverage != "medical" && coverage != "routine_vision" {
		return PlanList{}, false
	}
	r := active.Load()
	summaries := []PlanSummary{}
	if !officeSupports(office, coverage) {
		return PlanList{OfficeID: office.ID, Coverage: coverage, CatalogRevisionID: r.id, Plans: summaries}, true
	}
	list := r.listForOffice(office.ID)
	for _, p := range list.Plans {
		if p.Coverage != coverage {
			continue
		}
		d := decidePlanAtOffice(p, office, "", time.Time{})
		s := PlanSummary{
			PlanID:           p.ID,
			Label:            p.Label,
			Family:           p.Family,
			SelfPay:          p.selfPay(),
			Names:            append([]string{}, p.Names...),
			CarrierCode:      p.CarrierCode,
			CarrierID:        p.CarrierID,
			CarrierName:      p.CarrierName,
			Outcome:          d.Outcome,
			AllowedProviders: d.AllowedProviders,
			Requirements:     []string{},
			CallerNotice:     p.CallerNotice,
			AcceptedAt:       []string{},
		}
		for _, r := range d.Requirements {
			s.Requirements = append(s.Requirements, r.Kind)
		}
		if d.Participation != "accepted" {
			s.AcceptedAt = acceptedElsewhere(r, p, list, office)
		}
		summaries = append(summaries, s)
	}
	slices.SortFunc(summaries, func(a, b PlanSummary) int {
		return cmp.Or(compareLabels(a.Label, b.Label), strings.Compare(a.PlanID, b.PlanID))
	})
	return PlanList{OfficeID: office.ID, Coverage: coverage, CatalogRevisionID: r.id, Plans: summaries}, true
}

func compareLabels(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

func acceptedElsewhere(r *revision, p plan, list planList, office *domain.OfficeConfig) []string {
	offices := []string{}
	for _, other := range r.lists {
		if other.Region != list.Region {
			continue
		}
		for _, id := range other.Offices {
			o, available := domain.LookupOfficeByID(id)
			if available && id != office.ID && decidePlan(other, p.ID, p.Coverage, o, "", time.Time{}).Participation == "accepted" {
				offices = append(offices, o.DisplayName)
			}
		}
	}
	return offices
}
