package patient

import (
	"os"
	"testing"

	"advancedmd-token-management/internal/domain"
)

func TestMain(m *testing.M) {
	domain.InitRegistry(domain.Prod)
	os.Exit(m.Run())
}
