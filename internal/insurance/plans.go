package insurance

import (
	"slices"
	"sync/atomic"
)

type planList struct {
	Region     string
	Table      string
	Offices    []string
	Plans      []plan
	vocabulary map[string]map[string]bool
}

type plan struct {
	ID           string
	Label        string
	Family       string
	Coverage     string
	Names        []string
	CarrierCode  string
	CarrierID    string
	CarrierName  string
	Doctors      map[string]string
	OnlyOffices  []string
	Requirements []planRequirement
	CallerNotice string
	Note         string
	SelfPay      bool
	nameTokens   [][]string
	planTypes    map[string]bool
}

type planRequirement struct {
	Kind    string
	Channel string
}

type revision struct {
	id      string
	catalog InsuranceCatalog
	lists   []planList
}

var active atomic.Pointer[revision]

func planNames(p plan) []string {
	return append([]string{p.Label}, p.Names...)
}

func (r *revision) listForOffice(officeID string) planList {
	for _, list := range r.lists {
		if slices.Contains(list.Offices, officeID) {
			return list
		}
	}
	return planList{}
}

func (p plan) hasName(heard []string) bool {
	if len(heard) == 0 {
		return false
	}
	for _, name := range p.nameTokens {
		if slices.Equal(name, heard) {
			return true
		}
	}
	return false
}
