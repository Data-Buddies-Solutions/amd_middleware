package insurance

import (
	"strings"
	"testing"
)

func TestReadCatalogFilesRejectsMalformedFiles(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		change func(files map[string][]byte)
	}{
		{"stray file", "is not used by", func(files map[string][]byte) { files["notes/extra.csv"] = []byte("plan\n") }},
		{"byte order mark", "", func(files map[string][]byte) {
			files["south_florida/plans.csv"] = append([]byte("\ufeff"), files["south_florida/plans.csv"]...)
		}},
		{"missing table column", "header must be", func(files map[string][]byte) {
			files["south_florida/doctors.csv"] = []byte("plan,Dr. Austin Bach,requires,notice,note\n")
		}},
		{"plan columns", "header must be", func(files map[string][]byte) {
			files["south_florida/plans.csv"] = []byte("id,label\n")
		}},
		{"bad self pay", "self_pay", func(files map[string][]byte) {
			files["south_florida/plans.csv"] = []byte(strings.Replace(string(files["south_florida/plans.csv"]), ",car301672,yes,", ",car301672,maybe,", 1))
		}},
		{"unknown carriers field", "carriers.json", func(files map[string][]byte) { files[carriersFile] = []byte(`["car1"]`) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := syntheticFiles(t, syntheticSouthFlorida(), nil, nil)
			tc.change(files)
			_, err := readCatalogFiles(files)
			if tc.want == "" && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestReadCatalogFilesMapsEachFileToTheCatalog(t *testing.T) {
	c := RunningCatalog()
	if len(c.Regions) != 2 || c.Regions[0].ID != "south_florida" || c.Regions[1].ID != "spring_hill_crystal_river" {
		t.Fatalf("regions = %+v", c.Regions)
	}
	tables := []string{}
	for _, region := range c.Regions {
		for _, table := range region.Tables {
			tables = append(tables, region.ID+"/"+table.ID+"="+strings.Join(table.Offices, ","))
		}
	}
	if got := strings.Join(tables, " "); got != "south_florida/doctors=hollywood,sweetwater,north_miami_beach_optical spring_hill_crystal_river/crystal_river=crystal_river spring_hill_crystal_river/spring_hill=spring_hill" {
		t.Fatalf("tables = %s", got)
	}
	first := c.Regions[0].Plans[0]
	if first.ID != "aetna-medicare-medical" || first.CarrierCode != "ICA01" || first.CarrierID != "car40907" || first.SelfPay || len(first.Aliases) != 6 || first.Aliases[0] != "Aetna Medicare HMO & PPO (Medical)" {
		t.Fatalf("first plan = %+v", first)
	}
	doctors := c.Regions[0].Tables[0].Doctors
	if strings.Join(doctors, ",") != "Dr. Austin Bach,Dr. Miriam Bach,Dr. Gisselle Calero,Dr. Maria Casas,Dr. Lisbet Vidal,Dr. Kyler Farnan" {
		t.Fatalf("doctor columns = %v", doctors)
	}
	for i := 1; i < len(c.Carriers); i++ {
		if c.Carriers[i-1].ID >= c.Carriers[i].ID {
			t.Fatalf("carriers not sorted by id at %d: %v", i, c.Carriers[i-1:i+1])
		}
	}
}
