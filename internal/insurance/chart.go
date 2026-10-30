package insurance

import (
	"cmp"
	"regexp"
	"slices"
	"time"

	"advancedmd-token-management/internal/domain"
)

func DecideChartInsurance(chart domain.PatientDemographics, coverage string, office *domain.OfficeConfig, dob string, asOf time.Time) InsuranceDecision {
	d, list, ok := startDecision(coverage, office)
	if !ok {
		return d
	}
	var plans []plan
	for _, p := range list.Plans {
		if p.Coverage == coverage && chart.CarrierID != "" && p.CarrierID == chart.CarrierID {
			plans = append(plans, p)
		}
	}
	groups := distinctOutcomes(plans, office, dob, asOf)
	if len(groups) != 1 || groups[0].Participation != "accepted" {
		d.Outcome = "needs_staff_task"
		d.Reason, d.Answer = "chart_unverified", answerChartStaff
		return d
	}
	return groups[0]
}

var noteBreaks = regexp.MustCompile(`[.,;:/\n]`)

type noteRun struct {
	planID            string
	piece, start, end int
}

func NotePlanIDs(note, carrierID string, office *domain.OfficeConfig) []string {
	if carrierID == "" {
		return nil
	}
	var pieces [][]string
	for _, piece := range noteBreaks.Split(note, -1) {
		pieces = append(pieces, tokens(piece))
	}
	list := listForOffice(office.ID)
	var ids []string
	for _, coverage := range []string{"medical", "routine_vision"} {
		if !officeSupports(office, coverage) {
			continue
		}
		var runs []noteRun
		for _, p := range list.Plans {
			if p.Coverage == coverage && p.CarrierID == carrierID {
				runs = append(runs, notedRuns(pieces, p)...)
			}
		}
		if id, ok := onlyNamedPlan(runs); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func notedRuns(pieces [][]string, p plan) []noteRun {
	carrier := tokens(p.CarrierName)
	var runs []noteRun
	for _, name := range p.nameTokens {
		specific := slices.ContainsFunc(name, func(w string) bool { return !genericWords[w] })
		if len(name) < 2 || !specific || onlyCarrierWords(name, carrier) {
			continue
		}
		for i, words := range pieces {
			for start := 0; start+len(name) <= len(words); start++ {
				if slices.Equal(words[start:start+len(name)], name) {
					runs = append(runs, noteRun{p.ID, i, start, start + len(name)})
				}
			}
		}
	}
	return runs
}

func onlyCarrierWords(name, carrier []string) bool {
	for _, w := range name {
		if !slices.Contains(carrier, w) {
			return false
		}
	}
	return true
}

func onlyNamedPlan(runs []noteRun) (string, bool) {
	if len(runs) == 0 {
		return "", false
	}
	widest := slices.MaxFunc(runs, func(a, b noteRun) int { return cmp.Compare(a.end-a.start, b.end-b.start) })
	for _, r := range runs {
		within := r.piece == widest.piece && r.start >= widest.start && r.end <= widest.end && r.end-r.start < widest.end-widest.start
		if r.planID != widest.planID && !within {
			return "", false
		}
	}
	return widest.planID, true
}
