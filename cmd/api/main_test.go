package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance/insurancetest"
)

const runMainEnv = "ABITA_MIDDLEWARE_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		main()
		return
	}
	domain.InitRegistry(domain.Prod)
	os.Exit(m.Run())
}

func middleware(t *testing.T, productURL string) (*exec.Cmd, func() string, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	logPath := filepath.Join(t.TempDir(), "middleware.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logFile.Close() })
	output := func() string {
		b, _ := os.ReadFile(logPath)
		return string(b)
	}
	cmd := exec.Command(os.Args[0])
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.Env = append(os.Environ(),
		runMainEnv+"=1",
		"PORT="+strconv.Itoa(port),
		"AMD_ENV=prod",
		"ADVANCEDMD_USERNAME=user",
		"ADVANCEDMD_PASSWORD=password",
		"ADVANCEDMD_OFFICE_KEY=991TEST",
		"ADVANCEDMD_APP_NAME=app",
		"API_SECRET=test-secret",
		"MAINTENANCE_OIDC_AUDIENCE=https://middleware.example.test",
		"MAINTENANCE_OIDC_SERVICE_ACCOUNT=middleware-maintenance@example.iam.gserviceaccount.com",
		"ACUITY_PRODUCT_URL="+productURL,
		"ACUITY_PRODUCT_INSURANCE_TOKEN=product-insurance-token",
	)
	return cmd, output, "http://127.0.0.1:" + strconv.Itoa(port)
}

func TestStartupExitsBeforeListeningWhenProductIsUnreachable(t *testing.T) {
	product := httptest.NewServer(http.NotFoundHandler())
	product.Close()
	cmd, output, base := middleware(t, product.URL)

	err := cmd.Run()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("process exit = %v, want a non-zero exit; output:\n%s", err, output())
	}
	logs := output()
	if !strings.Contains(logs, "insurance_catalog_refresh_failed current_revision=none category=network") ||
		!strings.Contains(logs, "Failed to install the insurance catalog from Product") ||
		strings.Contains(logs, "Server listening") {
		t.Fatalf("output:\n%s", logs)
	}
	if response, err := http.Get(base + "/ready"); err == nil {
		response.Body.Close()
		t.Fatalf("/ready answered %d after a failed startup", response.StatusCode)
	}
}

func TestStartupInstallsTheProductCatalogBeforeItIsReady(t *testing.T) {
	catalog, err := json.Marshal(insurancetest.Catalog())
	if err != nil {
		t.Fatal(err)
	}
	product := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent/insurance/read" || r.Header.Get("Authorization") != "Bearer product-insurance-token" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"revisionId":"rev-a","catalog":` + string(catalog) + `}`))
	}))
	defer product.Close()
	cmd, output, base := middleware(t, product.URL)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Signal(syscall.SIGTERM)
		cmd.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := http.Get(base + "/ready")
		if err == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("/ready = %d", response.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("middleware never listened; output:\n%s", output())
		}
		time.Sleep(20 * time.Millisecond)
	}
	request, _ := http.NewRequest(http.MethodGet, base+"/api/insurance/plans?office=sweetwater&coverage=medical", nil)
	request.Header.Set("Authorization", "Bearer test-secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var plans struct {
		CatalogRevisionID string `json:"catalogRevisionId"`
		Plans             []any  `json:"plans"`
	}
	if err := json.NewDecoder(response.Body).Decode(&plans); err != nil || response.StatusCode != http.StatusOK || plans.CatalogRevisionID != "rev-a" || len(plans.Plans) == 0 {
		t.Fatalf("plans are not decided from Product's revision: status=%d revision=%q plans=%d err=%v", response.StatusCode, plans.CatalogRevisionID, len(plans.Plans), err)
	}
	logs := output()
	installed := strings.Index(logs, "insurance_catalog_installed revision=rev-a previous_revision=none")
	listening := strings.Index(logs, "Server listening")
	if installed < 0 || listening < 0 || installed > listening {
		t.Fatalf("catalog was not installed before listening; output:\n%s", logs)
	}
}
