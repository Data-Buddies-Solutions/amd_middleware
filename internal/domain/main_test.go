package domain

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	InitRegistry(Prod)
	os.Exit(m.Run())
}
