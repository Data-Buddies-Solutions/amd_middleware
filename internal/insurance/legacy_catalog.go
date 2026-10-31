package insurance

import (
	"maps"
	"slices"
)

type legacyCatalog struct {
	Carriers []legacyCarrier `json:"carriers"`
	Regions  []legacyRegion  `json:"regions"`
}

type legacyCarrier struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type legacyRegion struct {
	ID     string        `json:"id"`
	Plans  []legacyPlan  `json:"plans"`
	Tables []legacyTable `json:"tables"`
}

type legacyPlan struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Family      string   `json:"family"`
	Coverage    string   `json:"coverage"`
	CarrierCode string   `json:"carrierCode"`
	CarrierID   string   `json:"carrierId"`
	SelfPay     bool     `json:"selfPay"`
	Aliases     []string `json:"aliases"`
}

type legacyTable struct {
	ID      string      `json:"id"`
	Offices []string    `json:"offices"`
	Doctors []string    `json:"doctors"`
	Rows    []legacyRow `json:"rows"`
}

type legacyRow struct {
	PlanID      string              `json:"planId"`
	Doctors     map[string]string   `json:"doctors"`
	Requires    []legacyRequirement `json:"requires"`
	OnlyOffices []string            `json:"onlyOffices"`
	Notice      string              `json:"notice"`
	Note        string              `json:"note"`
}

type legacyRequirement struct {
	Kind    string `json:"kind"`
	Channel string `json:"channel"`
}

func decodeLegacyCatalog(body []byte) (InsuranceCatalog, error) {
	var legacy legacyCatalog
	if err := decodeStrictJSON(body, &legacy); err != nil {
		return InsuranceCatalog{}, err
	}
	return convertLegacyCatalog(legacy), nil
}

func convertLegacyCatalog(legacy legacyCatalog) InsuranceCatalog {
	codeCounts := map[string]map[string]int{}
	for _, region := range legacy.Regions {
		for _, p := range region.Plans {
			if p.CarrierCode == "" {
				continue
			}
			if codeCounts[p.CarrierID] == nil {
				codeCounts[p.CarrierID] = map[string]int{}
			}
			codeCounts[p.CarrierID][p.CarrierCode]++
		}
	}
	catalog := InsuranceCatalog{Carriers: []CatalogCarrier{}, Regions: []CatalogRegion{}}
	for _, carrier := range legacy.Carriers {
		catalog.Carriers = append(catalog.Carriers, CatalogCarrier{ID: carrier.ID, Code: mostCommonCode(codeCounts[carrier.ID]), Name: carrier.Name})
	}
	for _, region := range legacy.Regions {
		converted := CatalogRegion{ID: region.ID, Plans: []CatalogPlan{}, Tables: []CatalogTable{}}
		for _, p := range region.Plans {
			converted.Plans = append(converted.Plans, CatalogPlan{ID: p.ID, Label: p.Label, Family: p.Family, Coverage: p.Coverage, CarrierID: p.CarrierID, Aliases: p.Aliases})
		}
		for _, table := range region.Tables {
			rows := []CatalogRow{}
			for _, row := range table.Rows {
				rows = append(rows, CatalogRow{PlanID: row.PlanID, Doctors: row.Doctors, BeforeBooking: legacyBeforeBooking(row.Requires), OnlyOffices: row.OnlyOffices, Notice: row.Notice})
			}
			converted.Tables = append(converted.Tables, CatalogTable{ID: table.ID, Offices: table.Offices, Doctors: table.Doctors, Rows: rows})
		}
		catalog.Regions = append(catalog.Regions, converted)
	}
	return catalog
}

func mostCommonCode(counts map[string]int) string {
	best := ""
	for _, code := range slices.Sorted(maps.Keys(counts)) {
		if counts[code] > counts[best] {
			best = code
		}
	}
	return best
}

func legacyBeforeBooking(requires []legacyRequirement) BeforeBooking {
	if len(requires) == 0 {
		return BeforeBookingNone
	}
	if slices.ContainsFunc(requires, func(r legacyRequirement) bool { return BeforeBooking(r.Kind) == BeforeBookingStaffVerify }) {
		return BeforeBookingStaffVerify
	}
	return BeforeBooking(requires[0].Kind)
}
