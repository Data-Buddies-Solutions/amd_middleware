package scheduling

import (
	"os"
	"testing"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance/insurancetest"
)

func TestMain(m *testing.M) {
	domain.InitRegistry(domain.Prod)
	insurancetest.Install()
	os.Exit(m.Run())
}
