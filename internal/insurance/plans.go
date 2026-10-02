package insurance

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

//go:embed data/*.json
var dataFiles embed.FS

const carriersFile = "carriers.json"

type planList struct {
	Source     string   `json:"source"`
	Offices    []string `json:"offices"`
	Plans      []plan   `json:"plans"`
	vocabulary map[string]map[string]bool
}

type plan struct {
	ID           string            `json:"id"`
	Label        string            `json:"label"`
	Coverage     string            `json:"coverage"`
	Names        []string          `json:"names"`
	CarrierCode  string            `json:"carrierCode"`
	CarrierID    string            `json:"carrierId"`
	Doctors      map[string]string `json:"doctors"`
	OnlyOffices  []string          `json:"onlyOffices"`
	Requirements []planRequirement `json:"requirements"`
	CallerNotice string            `json:"callerNotice"`
	Note         string            `json:"note"`
	SelfPay      bool              `json:"selfPay"`
	nameTokens   [][]string
}

type planRequirement struct {
	Kind    string `json:"kind"`
	Channel string `json:"channel,omitempty"`
}

var catalog = mustLoadCatalog()

func mustLoadCatalog() []planList {
	files := map[string][]byte{}
	entries, err := dataFiles.ReadDir("data")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		b, err := dataFiles.ReadFile("data/" + entry.Name())
		if err != nil {
			panic(err)
		}
		files[entry.Name()] = b
	}
	lists, err := parseCatalog(files)
	if err != nil {
		panic("invalid insurance plan data: " + err.Error())
	}
	return lists
}

func parseCatalog(files map[string][]byte) ([]planList, error) {
	carriers := map[string]string{}
	if err := decodeStrictJSON(files[carriersFile], &carriers); err != nil {
		return nil, fmt.Errorf("%s: %w", carriersFile, err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		if name != carriersFile {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	lists := make([]planList, 0, len(names))
	for _, name := range names {
		var list planList
		if err := decodeStrictJSON(files[name], &list); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		list.vocabulary = map[string]map[string]bool{}
		for i := range list.Plans {
			p := &list.Plans[i]
			if list.vocabulary[p.Coverage] == nil {
				list.vocabulary[p.Coverage] = map[string]bool{}
			}
			for _, name := range planNames(*p) {
				words := tokens(name)
				p.nameTokens = append(p.nameTokens, words)
				for _, word := range words {
					list.vocabulary[p.Coverage][word] = true
				}
			}
		}
		lists = append(lists, list)
	}
	if err := validateCatalog(names, lists, carriers); err != nil {
		return nil, err
	}
	return lists, nil
}

func decodeStrictJSON(b []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}

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

func (p plan) requirementKinds() string {
	kinds := make([]string, 0, len(p.Requirements))
	for _, r := range p.Requirements {
		kinds = append(kinds, r.Kind)
	}
	slices.Sort(kinds)
	return strings.Join(kinds, ",")
}
