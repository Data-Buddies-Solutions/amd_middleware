package advancedmd

import (
	"os"
	"testing"

	"advancedmd-token-management/internal/domain"
)

func TestMain(m *testing.M) {
	domain.InitRegistry(domain.Prod)
	os.Exit(m.Run())
}

func testOffice(officeID string) *domain.OfficeConfig {
	office, _ := domain.LookupOfficeByID(officeID)
	return office
}

func testOffices(officeIDs ...string) []*domain.OfficeConfig {
	offices := make([]*domain.OfficeConfig, len(officeIDs))
	for i, officeID := range officeIDs {
		offices[i] = testOffice(officeID)
	}
	return offices
}
