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

	"advancedmd-token-management/internal/domain"
)

//go:embed data
var dataFiles embed.FS

const (
	carriersFile     = "carriers.json"
	officeTablesFile = "office_tables.json"
	plansFile        = "plans.csv"
)

var planColumns = []string{"id", "label", "coverage", "carrier_code", "carrier_id", "self_pay", "aliases"}

var tableColumns = []string{"requires", "only_offices", "notice", "note"}

func mustLoadCatalog() []planList {
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
	officeTables := map[string][]string{}
	if err := decodeStrictJSON(files[officeTablesFile], &officeTables); err != nil {
		return nil, fmt.Errorf("%s: %w", officeTablesFile, err)
	}
	if err := validateOfficeTables(officeTables); err != nil {
		return nil, err
	}
	if err := checkEveryFileIsListed(files, officeTables); err != nil {
		return nil, err
	}
	tables := slices.Sorted(maps.Keys(officeTables))
	plansByDir := map[string]map[string]plan{}
	usedByDir := map[string]map[string]bool{}
	lists := make([]planList, 0, len(tables))
	for _, table := range tables {
		dir := path.Dir(table)
		if plansByDir[dir] == nil {
			plans, err := parsePlans(files[path.Join(dir, plansFile)], carriers)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path.Join(dir, plansFile), err)
			}
			plansByDir[dir] = plans
			usedByDir[dir] = map[string]bool{}
		}
		list, err := parseTable(files[table], officeTables[table], plansByDir[dir], usedByDir[dir])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", table, err)
		}
		list.Table = table
		list.Offices = officeTables[table]
		indexNames(&list)
		lists = append(lists, list)
	}
	for dir, plans := range plansByDir {
		for id := range plans {
			if !usedByDir[dir][id] {
				return nil, fmt.Errorf("%s: plan %q is in no office table", path.Join(dir, plansFile), id)
			}
		}
	}
	if err := validateCatalog(lists, carriers); err != nil {
		return nil, err
	}
	return lists, nil
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

func parsePlans(b []byte, carriers map[string]string) (map[string]plan, error) {
	rows, err := readCSV(b, planColumns)
	if err != nil {
		return nil, err
	}
	plans := map[string]plan{}
	for _, row := range rows {
		p := plan{ID: row[0], Label: row[1], Coverage: row[2], CarrierCode: row[3], CarrierID: row[4], CarrierName: carriers[row[4]]}
		switch row[5] {
		case "yes":
			p.SelfPay = true
		case "":
		default:
			return nil, fmt.Errorf("plan %q: self_pay must be yes or empty", p.ID)
		}
		p.Names = splitList(row[6], "|")
		if _, ok := plans[p.ID]; ok {
			return nil, fmt.Errorf("duplicate plan id %q", p.ID)
		}
		plans[p.ID] = p
	}
	return plans, nil
}

func parseTable(b []byte, offices []string, plans map[string]plan, used map[string]bool) (planList, error) {
	header, rows, err := readTable(b)
	if err != nil {
		return planList{}, err
	}
	doctors := header[1 : len(header)-len(tableColumns)]
	if err := checkDoctorColumns(doctors, offices); err != nil {
		return planList{}, err
	}
	list := planList{}
	seen := map[string]bool{}
	for _, row := range rows {
		p, ok := plans[row[0]]
		if !ok {
			return planList{}, fmt.Errorf("plan %q is not in %s", row[0], plansFile)
		}
		if seen[p.ID] {
			return planList{}, fmt.Errorf("plan %q has two rows", p.ID)
		}
		seen[p.ID] = true
		used[p.ID] = true
		p.Doctors = map[string]string{}
		for i, doctor := range doctors {
			if value := row[i+1]; value != "" {
				p.Doctors[doctor] = value
			}
		}
		rest := row[len(row)-len(tableColumns):]
		for _, r := range splitList(rest[0], ";") {
			kind, channel, _ := strings.Cut(r, ":")
			p.Requirements = append(p.Requirements, planRequirement{Kind: kind, Channel: channel})
		}
		p.OnlyOffices = splitList(rest[1], ";")
		p.CallerNotice = rest[2]
		p.Note = rest[3]
		list.Plans = append(list.Plans, p)
	}
	return list, nil
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

func checkDoctorColumns(columns, offices []string) error {
	want := []string{}
	for _, id := range offices {
		office, _ := domain.LookupOfficeByID(id)
		for _, column := range office.Columns {
			if !slices.Contains(want, column.DisplayName) {
				want = append(want, column.DisplayName)
			}
		}
	}
	got := slices.Clone(columns)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		return fmt.Errorf("doctor columns must be exactly %s", strings.Join(want, ", "))
	}
	return nil
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
