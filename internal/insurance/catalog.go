package insurance

type InsuranceCatalog struct {
	Carriers []CatalogCarrier `json:"carriers"`
	Regions  []CatalogRegion  `json:"regions"`
}

type CatalogCarrier struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CatalogRegion struct {
	ID     string         `json:"id"`
	Plans  []CatalogPlan  `json:"plans"`
	Tables []CatalogTable `json:"tables"`
}

type CatalogPlan struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Family      string   `json:"family"`
	Coverage    string   `json:"coverage"`
	CarrierCode string   `json:"carrierCode"`
	CarrierID   string   `json:"carrierId"`
	SelfPay     bool     `json:"selfPay"`
	Aliases     []string `json:"aliases"`
}

type CatalogTable struct {
	ID      string       `json:"id"`
	Offices []string     `json:"offices"`
	Doctors []string     `json:"doctors"`
	Rows    []CatalogRow `json:"rows"`
}

type CatalogRow struct {
	PlanID      string               `json:"planId"`
	Doctors     map[string]string    `json:"doctors"`
	Requires    []CatalogRequirement `json:"requires"`
	OnlyOffices []string             `json:"onlyOffices"`
	Notice      string               `json:"notice"`
	Note        string               `json:"note"`
}

type CatalogRequirement struct {
	Kind    string `json:"kind"`
	Channel string `json:"channel"`
}

type CatalogError struct {
	Region  string      `json:"region"`
	Table   string      `json:"table"`
	PlanID  string      `json:"planId"`
	Rule    CatalogRule `json:"rule"`
	Message string      `json:"message"`
}

type CatalogRule string

func RunningCatalog() InsuranceCatalog {
	return active.Load().catalog
}

func ValidateCatalog(candidate InsuranceCatalog) []CatalogError {
	_, errs, drift := parseCatalog(candidate)
	return append(errs, drift...)
}
