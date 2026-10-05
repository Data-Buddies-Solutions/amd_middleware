package insurance

import "slices"

type planList struct {
	Table      string
	Offices    []string
	Plans      []plan
	vocabulary map[string]map[string]bool
}

type plan struct {
	ID           string
	Label        string
	Coverage     string
	Names        []string
	CarrierCode  string
	CarrierID    string
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

var catalog = mustLoadCatalog()

func planNames(p plan) []string {
	return append([]string{p.Label}, p.Names...)
}

func listForOffice(officeID string) planList {
	for _, list := range catalog {
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
