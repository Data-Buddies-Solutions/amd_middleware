package insurancetest

import (
	"os"
	"testing"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
)

func TestMain(m *testing.M) {
	domain.InitRegistry(domain.Prod)
	os.Exit(m.Run())
}

func TestFixtureIsAValidCatalog(t *testing.T) {
	if errs := insurance.ValidateCatalog(Catalog()); len(errs) > 0 {
		t.Fatalf("testdata/catalog.json breaks %d catalog rules: %+v", len(errs), errs)
	}
}
