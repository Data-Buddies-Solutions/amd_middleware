package insurance

import (
	"encoding/json"
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
		{ID: "self-pay", Label: "Self Pay", Coverage: "medical", Names: []string{"Self-pay", "Cash", "Cash pay"}, CarrierID: "car301672", Doctors: bach("yes"), SelfPay: true},
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
	lists := map[string]planList{
		"south_florida.json": {Offices: []string{"hollywood", "sweetwater", "north_miami_beach_optical"}, Plans: southFlorida},
		"spring_hill.json":   {Offices: []string{"spring_hill"}, Plans: springHill},
		"crystal_river.json": {Offices: []string{"crystal_river"}, Plans: crystalRiver},
	}
	for name, list := range lists {
		b, err := json.Marshal(list)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = b
	}
	return files
}

func office(t *testing.T, id string) *domain.OfficeConfig {
	t.Helper()
	o, ok := domain.LookupOfficeByID(id)
	if !ok {
		t.Fatalf("unknown office %s", id)
	}
	return o
}
