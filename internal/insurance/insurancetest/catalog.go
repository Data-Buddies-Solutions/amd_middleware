package insurancetest

import (
	"bytes"
	"context"
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"advancedmd-token-management/internal/insurance"
)

//go:embed data
var dataFiles embed.FS

const (
	carriersFile     = "carriers.json"
	officeTablesFile = "office_tables.json"
	plansFile        = "plans.csv"
)

var planColumns = []string{"id", "label", "family", "coverage", "carrier_code", "carrier_id", "self_pay", "aliases"}

var tableColumns = []string{"requires", "only_offices", "notice", "note"}

const RevisionID = "fixture"

func Install() {
	body, err := json.Marshal(Catalog())
	if err != nil {
		panic(err)
	}
	published := func(context.Context) (string, []byte, error) { return RevisionID, body, nil }
	if err := insurance.Refresh(context.Background(), published); err != nil {
		panic("insurance fixture did not install: " + err.Error())
	}
}

func Catalog() insurance.InsuranceCatalog {
	c, err := read(files())
	if err != nil {
		panic("invalid insurance fixture files: " + err.Error())
	}
	return c
}

func files() map[string][]byte {
	found := map[string][]byte{}
	err := fs.WalkDir(dataFiles, "data", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || path.Ext(name) == ".md" {
			return err
		}
		b, err := dataFiles.ReadFile(name)
		found[strings.TrimPrefix(name, "data/")] = b
		return err
	})
	if err != nil {
		panic(err)
	}
	return found
}

func read(files map[string][]byte) (insurance.InsuranceCatalog, error) {
	carriers := map[string]string{}
	if err := decodeStrictJSON(files[carriersFile], &carriers); err != nil {
		return insurance.InsuranceCatalog{}, fmt.Errorf("%s: %w", carriersFile, err)
	}
	officeTables := map[string][]string{}
	if err := decodeStrictJSON(files[officeTablesFile], &officeTables); err != nil {
		return insurance.InsuranceCatalog{}, fmt.Errorf("%s: %w", officeTablesFile, err)
	}
	if err := checkEveryFileIsListed(files, officeTables); err != nil {
		return insurance.InsuranceCatalog{}, err
	}
	c := insurance.InsuranceCatalog{Carriers: []insurance.CatalogCarrier{}, Regions: []insurance.CatalogRegion{}}
	for _, id := range slices.Sorted(maps.Keys(carriers)) {
		c.Carriers = append(c.Carriers, insurance.CatalogCarrier{ID: id, Name: carriers[id]})
	}
	for _, name := range slices.Sorted(maps.Keys(officeTables)) {
		dir := path.Dir(name)
		if len(c.Regions) == 0 || c.Regions[len(c.Regions)-1].ID != dir {
			plans, err := readPlans(files[path.Join(dir, plansFile)])
			if err != nil {
				return insurance.InsuranceCatalog{}, fmt.Errorf("%s: %w", path.Join(dir, plansFile), err)
			}
			c.Regions = append(c.Regions, insurance.CatalogRegion{ID: dir, Plans: plans, Tables: []insurance.CatalogTable{}})
		}
		table, err := readOfficeTable(files[name])
		if err != nil {
			return insurance.InsuranceCatalog{}, fmt.Errorf("%s: %w", name, err)
		}
		table.ID = strings.TrimSuffix(path.Base(name), path.Ext(name))
		table.Offices = officeTables[name]
		region := &c.Regions[len(c.Regions)-1]
		region.Tables = append(region.Tables, table)
	}
	return c, nil
}

func checkEveryFileIsListed(files map[string][]byte, officeTables map[string][]string) error {
	tableDirs := map[string]bool{}
	for table := range officeTables {
		tableDirs[path.Dir(table)] = true
	}
	for name := range files {
		_, isTable := officeTables[name]
		isPlans := path.Base(name) == plansFile && tableDirs[path.Dir(name)]
		if name != carriersFile && name != officeTablesFile && !isTable && !isPlans {
			return fmt.Errorf("%s is not used by %s", name, officeTablesFile)
		}
	}
	return nil
}

func readPlans(b []byte) ([]insurance.CatalogPlan, error) {
	rows, err := readCSV(b, planColumns)
	if err != nil {
		return nil, err
	}
	plans := []insurance.CatalogPlan{}
	for _, row := range rows {
		p := insurance.CatalogPlan{ID: row[0], Label: row[1], Family: row[2], Coverage: row[3], CarrierCode: row[4], CarrierID: row[5], Aliases: splitList(row[7], "|")}
		switch row[6] {
		case "yes":
			p.SelfPay = true
		case "":
		default:
			return nil, fmt.Errorf("plan %q: self_pay must be yes or empty", p.ID)
		}
		plans = append(plans, p)
	}
	return plans, nil
}

func readOfficeTable(b []byte) (insurance.CatalogTable, error) {
	header, rows, err := readTable(b)
	if err != nil {
		return insurance.CatalogTable{}, err
	}
	table := insurance.CatalogTable{Doctors: append([]string{}, header[1:len(header)-len(tableColumns)]...), Rows: []insurance.CatalogRow{}}
	for _, row := range rows {
		r := insurance.CatalogRow{PlanID: row[0], Doctors: map[string]string{}, Requires: []insurance.CatalogRequirement{}}
		for i, doctor := range table.Doctors {
			if value := row[i+1]; value != "" {
				r.Doctors[doctor] = value
			}
		}
		rest := row[len(row)-len(tableColumns):]
		for _, requirement := range splitList(rest[0], ";") {
			kind, channel, _ := strings.Cut(requirement, ":")
			r.Requires = append(r.Requires, insurance.CatalogRequirement{Kind: kind, Channel: channel})
		}
		r.OnlyOffices = splitList(rest[1], ";")
		r.Notice = rest[2]
		r.Note = rest[3]
		table.Rows = append(table.Rows, r)
	}
	return table, nil
}

func readTable(b []byte) ([]string, [][]string, error) {
	records, err := readRecords(b)
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("empty table")
	}
	header := records[0]
	if len(header) < len(tableColumns)+1 || header[0] != "plan" || !slices.Equal(header[len(header)-len(tableColumns):], tableColumns) {
		return nil, nil, fmt.Errorf("header must be plan, doctor columns, then %s", strings.Join(tableColumns, ", "))
	}
	return header, records[1:], nil
}

func readCSV(b []byte, columns []string) ([][]string, error) {
	records, err := readRecords(b)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 || !slices.Equal(records[0], columns) {
		return nil, fmt.Errorf("header must be %s", strings.Join(columns, ","))
	}
	return records[1:], nil
}

func readRecords(b []byte) ([][]string, error) {
	return csv.NewReader(bytes.NewReader(bytes.TrimPrefix(b, []byte("\ufeff")))).ReadAll()
}

func splitList(s, separator string) []string {
	items := []string{}
	for _, item := range strings.Split(s, separator) {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func decodeStrictJSON(b []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}
