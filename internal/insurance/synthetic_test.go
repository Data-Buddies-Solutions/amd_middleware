package insurance

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"advancedmd-token-management/internal/domain"
)

const adultDOB = "01/15/1980"

const syntheticCarriers = `{"car40907":"ICARE HEALTH OPTIONS TPA","car40887":"AETNA","car280695":"VSP","car301672":"SELF PAY","car1":"ONE","car2":"TWO","car3":"THREE","car4":"FOUR","car5":"FIVE"}`

func bach(value string) map[string]string {
	return map[string]string{"Dr. Austin Bach": value}
}

func syntheticSouthFlorida() []plan {
	return []plan{
		{ID: "aetna-better-health", Label: "Aetna Better Health", Family: "Aetna", Coverage: "medical", Names: []string{"Aetna Better Health Medicaid", "Aetna Better Health MMA"}, CarrierCode: "ICA01", CarrierID: "car40907", Doctors: bach("yes")},
		{ID: "aetna-better-health-kids", Label: "Aetna Better Health Kids", Family: "Aetna", Coverage: "medical", Names: []string{"Aetna Healthy Kids"}, CarrierCode: "ICA01", CarrierID: "car40907", Doctors: bach("yes")},
		{ID: "aetna-medicare", Label: "Aetna Medicare", Family: "Aetna", Coverage: "medical", Names: []string{"Aetna Medicare HMO", "Aetna Medicare PPO"}, CarrierCode: "AET07", CarrierID: "car40887", Doctors: bach("yes")},
		{ID: "aetna-commercial", Label: "Aetna Commercial", Family: "Aetna", Coverage: "medical", Names: []string{"Aetna PPO", "Aetna Open Access"}, CarrierCode: "AET07", CarrierID: "car40887", Doctors: bach("no")},
		{ID: "self-pay", Label: "Self Pay", Family: "Self Pay", Coverage: "medical", Names: []string{"Cash", "Cash pay"}, CarrierID: "car301672", Doctors: bach("yes"), SelfPay: true},
		{ID: "vsp", Label: "VSP", Family: "VSP", Coverage: "routine_vision", Names: []string{"Vision Service Plan"}, CarrierID: "car280695", Doctors: map[string]string{"Dr. Kyler Farnan": "yes", "Dr. Lisbet Vidal": "yes", "Dr. Gisselle Calero": "pending", "Dr. Maria Casas": "yes"}},
	}
}

func useSyntheticCatalog(t *testing.T, southFlorida, springHill, crystalRiver []plan) {
	t.Helper()
	lists, errs := readPlanLists(syntheticCatalog(t, southFlorida, springHill, crystalRiver))
	if len(errs) > 0 {
		t.Fatalf("%+v", errs)
	}
	installForTest(t, lists)
}

func installForTest(t *testing.T, lists []planList) {
	t.Helper()
	previous := active.Load()
	active.Store(&revision{id: "test", lists: lists})
	t.Cleanup(func() { active.Store(previous) })
}

func syntheticCatalog(t *testing.T, southFlorida, springHill, crystalRiver []plan) InsuranceCatalog {
	t.Helper()
	carriers := map[string]string{}
	if err := json.Unmarshal([]byte(syntheticCarriers), &carriers); err != nil {
		t.Fatal(err)
	}
	c := InsuranceCatalog{}
	for _, id := range slices.Sorted(maps.Keys(carriers)) {
		c.Carriers = append(c.Carriers, CatalogCarrier{ID: id, Name: carriers[id]})
	}
	c.Regions = []CatalogRegion{
		syntheticRegion(t, "crystal_river", []string{"crystal_river"}, crystalRiver),
		syntheticRegion(t, "south_florida", []string{"hollywood", "sweetwater", "north_miami_beach_optical"}, southFlorida),
		syntheticRegion(t, "spring_hill", []string{"spring_hill"}, springHill),
	}
	return c
}

func syntheticRegion(t *testing.T, id string, offices []string, plans []plan) CatalogRegion {
	t.Helper()
	table := CatalogTable{ID: "doctors", Offices: offices, Rows: []CatalogRow{}}
	for _, officeID := range offices {
		for _, column := range office(t, officeID).Columns {
			if !slices.Contains(table.Doctors, column.DisplayName) {
				table.Doctors = append(table.Doctors, column.DisplayName)
			}
		}
	}
	slices.Sort(table.Doctors)
	region := CatalogRegion{ID: id, Plans: []CatalogPlan{}}
	for _, p := range plans {
		region.Plans = append(region.Plans, CatalogPlan{ID: p.ID, Label: p.Label, Family: p.Family, Coverage: p.Coverage, CarrierCode: p.CarrierCode, CarrierID: p.CarrierID, SelfPay: p.SelfPay, Aliases: p.Names})
		row := CatalogRow{PlanID: p.ID, Doctors: p.Doctors, Requires: []CatalogRequirement{}, OnlyOffices: p.OnlyOffices, Notice: p.CallerNotice, Note: p.Note}
		for _, r := range p.Requirements {
			row.Requires = append(row.Requires, CatalogRequirement{Kind: r.Kind, Channel: r.Channel})
		}
		table.Rows = append(table.Rows, row)
	}
	region.Tables = []CatalogTable{table}
	return region
}

func office(t *testing.T, id string) *domain.OfficeConfig {
	t.Helper()
	o, ok := domain.LookupOfficeByID(id)
	if !ok {
		t.Fatalf("unknown office %s", id)
	}
	return o
}
