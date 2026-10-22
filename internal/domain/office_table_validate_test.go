package domain

import (
	"strings"
	"testing"
)

func TestOfficeTableIsValid(t *testing.T) {
	if err := validateOfficeTable(newOfficeTable()); err != nil {
		t.Fatal(err)
	}
}

func officeDefinitionFor(t *testing.T, table *officeTable, id string) *officeDefinition {
	t.Helper()
	for i := range table.Offices {
		if table.Offices[i].ID == id {
			return &table.Offices[i]
		}
	}
	t.Fatalf("office %s is not in the table", id)
	return nil
}

func TestOfficeTableValidationRejectsBrokenTables(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		change func(t *testing.T, table *officeTable)
	}{
		{"missing column binding", "sandbox: office spring_hill column licht has no binding and is not marked unavailable", func(t *testing.T, table *officeTable) {
			delete(table.Environments[Sandbox].Offices["spring_hill"].Columns, "licht")
		}},
		{"duplicate column ID", "prod: column ID 1513 is bound to spring_hill/bach and hollywood/bach", func(t *testing.T, table *officeTable) {
			table.Environments[Prod].Offices["hollywood"].Columns["bach"] = columnBinding{ColumnID: "1513", ProfileID: "620"}
		}},
		{"missing office binding", "prod: office crystal_river has no binding and is not marked unavailable", func(t *testing.T, table *officeTable) {
			delete(table.Environments[Prod].Offices, "crystal_river")
		}},
		{"office bound and unavailable", "prod: office crystal_river is both bound and marked unavailable", func(t *testing.T, table *officeTable) {
			binding := table.Environments[Prod]
			binding.UnavailableOffices = append(binding.UnavailableOffices, "crystal_river")
			table.Environments[Prod] = binding
		}},
		{"column marked unavailable that the office lacks", "sandbox: office spring_hill column vidal is marked unavailable but not in the table", func(t *testing.T, table *officeTable) {
			office := table.Environments[Sandbox].Offices["spring_hill"]
			office.UnavailableColumns = append(office.UnavailableColumns, "vidal")
			table.Environments[Sandbox].Offices["spring_hill"] = office
		}},
		{"missing appointment type binding", "sandbox: appointment type 6169 has no binding and is not marked unavailable", func(t *testing.T, table *officeTable) {
			binding := table.Environments[Sandbox]
			binding.UnavailableAppointmentTypes = binding.UnavailableAppointmentTypes[:len(binding.UnavailableAppointmentTypes)-1]
			table.Environments[Sandbox] = binding
		}},
		{"duplicate AdvancedMD appointment type", "sandbox: AdvancedMD appointment type 20 is bound to 1004 and 1005", func(t *testing.T, table *officeTable) {
			table.Environments[Sandbox].AppointmentTypes[1005] = 20
		}},
		{"bookable lane with an unavailable type", "sandbox: office spring_hill: books visits with appointment type 1008, which is unavailable", func(t *testing.T, table *officeTable) {
			binding := table.Environments[Sandbox]
			delete(binding.AppointmentTypes, 1008)
			binding.UnavailableAppointmentTypes = append(binding.UnavailableAppointmentTypes, 1008)
			table.Environments[Sandbox] = binding
		}},
		{"duplicate phone", "prod: phone +17275919997 is bound to spring_hill and hollywood", func(t *testing.T, table *officeTable) {
			office := table.Environments[Prod].Offices["hollywood"]
			office.Phones = append(office.Phones, "+17275919997")
			table.Environments[Prod].Offices["hollywood"] = office
		}},
		{"default office unavailable", "sandbox: default office spring_hill is unavailable", func(t *testing.T, table *officeTable) {
			binding := table.Environments[Sandbox]
			delete(binding.Offices, "spring_hill")
			binding.UnavailableOffices = append(binding.UnavailableOffices, "spring_hill")
			table.Environments[Sandbox] = binding
		}},
		{"routing tier names an unknown column", "office spring_hill: routing tier bach_only names unknown column bach_3", func(t *testing.T, table *officeTable) {
			office := officeDefinitionFor(t, table, "spring_hill")
			office.RoutingTiers[RoutingBachOnly] = append(office.RoutingTiers[RoutingBachOnly], "bach_3")
		}},
		{"lookup group names an unknown office", "office hollywood: appointment lookup group names unknown office atlantis", func(t *testing.T, table *officeTable) {
			office := officeDefinitionFor(t, table, "hollywood")
			office.AppointmentLookupOfficeIDs = append(office.AppointmentLookupOfficeIDs, "atlantis")
		}},
		{"lookup group omits the office", "office sweetwater: appointment lookup group must include the office itself", func(t *testing.T, table *officeTable) {
			officeDefinitionFor(t, table, "sweetwater").AppointmentLookupOfficeIDs = []string{"hollywood"}
		}},
		{"office rule names an unknown appointment type", "office crystal_river: medical appointment types: unknown appointment type 7777", func(t *testing.T, table *officeTable) {
			officeDefinitionFor(t, table, "crystal_river").AppointmentTypes.Medical.NewAdult = 7777
		}},
		{"office rule names a type from the wrong lane", "office hollywood: post-op appointment type: appointment type 1010 is routine_vision, want medical", func(t *testing.T, table *officeTable) {
			officeDefinitionFor(t, table, "hollywood").AppointmentTypes.PostOp = 1010
		}},
		{"provider without a display name", "office north_miami_beach_optical: column miriam_bach needs a display name", func(t *testing.T, table *officeTable) {
			column := officeDefinitionFor(t, table, "north_miami_beach_optical").Columns["miriam_bach"]
			column.Provider.DisplayName = ""
			officeDefinitionFor(t, table, "north_miami_beach_optical").Columns["miriam_bach"] = column
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			table := newOfficeTable()
			tc.change(t, &table)
			err := validateOfficeTable(table)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateOfficeTable() = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestAnInvalidOfficeTableStopsTheRegistryBuild(t *testing.T) {
	table := newOfficeTable()
	delete(table.Environments[Sandbox].Offices["spring_hill"].Columns, "licht")
	defer func() {
		message, _ := recover().(string)
		if !strings.Contains(message, "invalid office table: sandbox: office spring_hill column licht has no binding") {
			t.Fatalf("mustBuildRegistries panic = %q, want the validation error", message)
		}
	}()
	mustBuildRegistries(table)
}
