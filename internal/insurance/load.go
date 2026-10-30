package insurance

import (
	"bytes"
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
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

func mustLoadCatalog() (InsuranceCatalog, []planList) {
	files := map[string][]byte{}
	err := fs.WalkDir(dataFiles, "data", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || path.Ext(name) == ".md" {
			return err
		}
		b, err := dataFiles.ReadFile(name)
		files[strings.TrimPrefix(name, "data/")] = b
		return err
	})
	if err != nil {
		panic(err)
	}
	source, err := readCatalogFiles(files)
	if err != nil {
		panic("invalid insurance plan data: " + err.Error())
	}
	lists, errs := parseCatalog(source)
	if len(errs) > 0 {
		panic(fmt.Sprintf("invalid insurance plan data: %+v", errs))
	}
	return source, lists
}

func readCatalogFiles(files map[string][]byte) (InsuranceCatalog, error) {
	carriers := map[string]string{}
	if err := decodeStrictJSON(files[carriersFile], &carriers); err != nil {
		return InsuranceCatalog{}, fmt.Errorf("%s: %w", carriersFile, err)
	}
	officeTables := map[string][]string{}
	if err := decodeStrictJSON(files[officeTablesFile], &officeTables); err != nil {
		return InsuranceCatalog{}, fmt.Errorf("%s: %w", officeTablesFile, err)
	}
	if err := checkEveryFileIsListed(files, officeTables); err != nil {
		return InsuranceCatalog{}, err
	}
	c := InsuranceCatalog{Carriers: []CatalogCarrier{}, Regions: []CatalogRegion{}}
	for _, id := range slices.Sorted(maps.Keys(carriers)) {
		c.Carriers = append(c.Carriers, CatalogCarrier{ID: id, Name: carriers[id]})
	}
	for _, name := range slices.Sorted(maps.Keys(officeTables)) {
		dir := path.Dir(name)
		if len(c.Regions) == 0 || c.Regions[len(c.Regions)-1].ID != dir {
			plans, err := readPlans(files[path.Join(dir, plansFile)])
			if err != nil {
				return InsuranceCatalog{}, fmt.Errorf("%s: %w", path.Join(dir, plansFile), err)
			}
			c.Regions = append(c.Regions, CatalogRegion{ID: dir, Plans: plans, Tables: []CatalogTable{}})
		}
		table, err := readOfficeTable(files[name])
		if err != nil {
			return InsuranceCatalog{}, fmt.Errorf("%s: %w", name, err)
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

func readPlans(b []byte) ([]CatalogPlan, error) {
	rows, err := readCSV(b, planColumns)
	if err != nil {
		return nil, err
	}
	plans := []CatalogPlan{}
	for _, row := range rows {
		p := CatalogPlan{ID: row[0], Label: row[1], Family: row[2], Coverage: row[3], CarrierCode: row[4], CarrierID: row[5], Aliases: splitList(row[7], "|")}
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

func readOfficeTable(b []byte) (CatalogTable, error) {
	header, rows, err := readTable(b)
	if err != nil {
		return CatalogTable{}, err
	}
	table := CatalogTable{Doctors: append([]string{}, header[1:len(header)-len(tableColumns)]...), Rows: []CatalogRow{}}
	for _, row := range rows {
		r := CatalogRow{PlanID: row[0], Doctors: map[string]string{}, Requires: []CatalogRequirement{}}
		for i, doctor := range table.Doctors {
			if value := row[i+1]; value != "" {
				r.Doctors[doctor] = value
			}
		}
		rest := row[len(row)-len(tableColumns):]
		for _, requirement := range splitList(rest[0], ";") {
			kind, channel, _ := strings.Cut(requirement, ":")
			r.Requires = append(r.Requires, CatalogRequirement{Kind: kind, Channel: channel})
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

func indexNames(list *planList) {
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
				if planTypes[word] {
					if p.planTypes == nil {
						p.planTypes = map[string]bool{}
					}
					p.planTypes[word] = true
				}
			}
		}
	}
}

func decodeStrictJSON(b []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}
