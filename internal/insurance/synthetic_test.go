package insurance

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"slices"
	"strings"
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
		{ID: "aetna-better-health", Label: "Aetna Better Health", Coverage: "medical", Names: []string{"Aetna Better Health Medicaid", "Aetna Better Health MMA"}, CarrierCode: "ICA01", CarrierID: "car40907", Doctors: bach("yes")},
		{ID: "aetna-better-health-kids", Label: "Aetna Better Health Kids", Coverage: "medical", Names: []string{"Aetna Healthy Kids"}, CarrierCode: "ICA01", CarrierID: "car40907", Doctors: bach("yes")},
		{ID: "aetna-medicare", Label: "Aetna Medicare", Coverage: "medical", Names: []string{"Aetna Medicare HMO", "Aetna Medicare PPO"}, CarrierCode: "AET07", CarrierID: "car40887", Doctors: bach("yes")},
		{ID: "aetna-commercial", Label: "Aetna Commercial", Coverage: "medical", Names: []string{"Aetna PPO", "Aetna Open Access"}, CarrierCode: "AET07", CarrierID: "car40887", Doctors: bach("no")},
		{ID: "self-pay", Label: "Self Pay", Coverage: "medical", Names: []string{"Cash", "Cash pay"}, CarrierID: "car301672", Doctors: bach("yes"), SelfPay: true},
		{ID: "vsp", Label: "VSP", Coverage: "routine_vision", Names: []string{"Vision Service Plan"}, CarrierID: "car280695", Doctors: map[string]string{"Dr. Kyler Farnan": "yes", "Dr. Lisbet Vidal": "yes", "Dr. Gisselle Calero": "pending", "Dr. Maria Casas": "yes"}},
	}
}

func useSyntheticCatalog(t *testing.T, southFlorida, springHill, crystalRiver []plan) {
	t.Helper()
	lists, err := parseCatalog(syntheticFiles(t, southFlorida, springHill, crystalRiver))
	if err != nil {
		t.Fatal(err)
	}
	previous := catalog
	catalog = lists
	t.Cleanup(func() { catalog = previous })
}

func syntheticFiles(t *testing.T, southFlorida, springHill, crystalRiver []plan) map[string][]byte {
	t.Helper()
	files := map[string][]byte{carriersFile: []byte(syntheticCarriers)}
	officeTables := map[string][]string{
		"south_florida/doctors.csv": {"hollywood", "sweetwater", "north_miami_beach_optical"},
		"spring_hill/doctors.csv":   {"spring_hill"},
		"crystal_river/doctors.csv": {"crystal_river"},
	}
	b, err := json.Marshal(officeTables)
	if err != nil {
		t.Fatal(err)
	}
	files[officeTablesFile] = b
	for dir, plans := range map[string][]plan{"south_florida": southFlorida, "spring_hill": springHill, "crystal_river": crystalRiver} {
		files[dir+"/"+plansFile], files[dir+"/doctors.csv"] = syntheticCSV(t, plans)
	}
	return files
}

func syntheticCSV(t *testing.T, plans []plan) ([]byte, []byte) {
	t.Helper()
	planRows := [][]string{planColumns}
	doctors := []string{}
	for _, p := range plans {
		for doctor := range p.Doctors {
			if !slices.Contains(doctors, doctor) {
				doctors = append(doctors, doctor)
			}
		}
	}
	slices.Sort(doctors)
	tableRows := [][]string{append(append([]string{"plan"}, doctors...), tableColumns...)}
	for _, p := range plans {
		selfPay := ""
		if p.SelfPay {
			selfPay = "yes"
		}
		planRows = append(planRows, []string{p.ID, p.Label, p.Coverage, p.CarrierCode, p.CarrierID, selfPay, strings.Join(p.Names, " | ")})
		row := []string{p.ID}
		for _, doctor := range doctors {
			row = append(row, p.Doctors[doctor])
		}
		requirements := []string{}
		for _, r := range p.Requirements {
			requirements = append(requirements, strings.TrimSuffix(r.Kind+":"+r.Channel, ":"))
		}
		row = append(row, strings.Join(requirements, ";"), strings.Join(p.OnlyOffices, ";"), p.CallerNotice, p.Note)
		tableRows = append(tableRows, row)
	}
	return writeCSV(t, planRows), writeCSV(t, tableRows)
}

func writeCSV(t *testing.T, rows [][]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	if err := w.WriteAll(rows); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func office(t *testing.T, id string) *domain.OfficeConfig {
	t.Helper()
	o, ok := domain.LookupOfficeByID(id)
	if !ok {
		t.Fatalf("unknown office %s", id)
	}
	return o
}
