package insurancetest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"advancedmd-token-management/internal/insurance"
)

const RevisionID = "fixture"

func Install() {
	body := fixture()
	published := func(context.Context) (string, []byte, error) { return RevisionID, body, nil }
	if err := insurance.Refresh(context.Background(), published); err != nil {
		panic("insurance fixture did not install: " + err.Error())
	}
}

func Catalog() insurance.InsuranceCatalog {
	var c insurance.InsuranceCatalog
	if err := json.Unmarshal(fixture(), &c); err != nil {
		panic("insurance fixture is not an InsuranceCatalog: " + err.Error())
	}
	return c
}

func fixture() []byte {
	_, here, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(here), "..", "testdata", "catalog.json"))
	if err != nil {
		panic(err)
	}
	return b
}
