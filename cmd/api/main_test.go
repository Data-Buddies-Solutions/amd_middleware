package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance/insurancetest"
	"advancedmd-token-management/internal/safeerrors"
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

func serveCatalog(t *testing.T, failFirst int) *httptest.Server {
	t.Helper()
	catalog, err := json.Marshal(insurancetest.Catalog())
	if err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	product := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent/insurance/read" || r.Header.Get("Authorization") != "Bearer product-insurance-token" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if int(reads.Add(1)) <= failFirst {
			http.Error(w, "deploying", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"revisionId":"rev-a","catalog":` + string(catalog) + `}`))
	}))
	t.Cleanup(product.Close)
	return product
}

func start(t *testing.T, cmd *exec.Cmd, output func() string, base string) {
	t.Helper()
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
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("middleware never listened; output:\n%s", output())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStartupRetriesAProductReadThatFailsBeforeListening(t *testing.T) {
	product := serveCatalog(t, 1)
	cmd, output, base := middleware(t, product.URL)

	start(t, cmd, output, base)

	logs := output()
	failed := strings.Index(logs, "insurance_catalog_refresh_failed current_revision=none category=upstream_status")
	installed := strings.Index(logs, "insurance_catalog_installed revision=rev-a previous_revision=none")
	listening := strings.Index(logs, "Server listening")
	if failed < 0 || installed < failed || listening < installed {
		t.Fatalf("want a failed read, then the install, then listening; output:\n%s", logs)
	}
}

func TestInstallInsuranceCatalogGivesUpWhenTheStartupWindowCloses(t *testing.T) {
	reads := 0
	unreachable := func(context.Context) (string, []byte, error) {
		reads++
		return "", nil, safeerrors.New(safeerrors.CategoryNetwork, "POST /v1/agent/insurance/read: network")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := installInsuranceCatalog(ctx, unreachable, 10*time.Millisecond)

	if safeerrors.Classify(err) != safeerrors.CategoryNetwork || reads < 2 {
		t.Fatalf("err = %v after %d reads, want the network failure after retrying", err, reads)
	}
}

func TestStartupInstallsTheProductCatalogBeforeItIsReady(t *testing.T) {
	catalog, err := json.Marshal(insurancetest.Catalog())
	if err != nil {
		t.Fatal(err)
	}
	product := serveCatalog(t, 0)
	cmd, output, base := middleware(t, product.URL)

	start(t, cmd, output, base)

	request, _ := http.NewRequest(http.MethodGet, base+"/api/insurance/catalog", nil)
	request.Header.Set("Authorization", "Bearer test-secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	exported, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || strings.TrimSpace(string(exported)) != string(catalog) {
		t.Fatalf("served catalog is not Product's revision: status=%d", response.StatusCode)
	}
	logs := output()
	installed := strings.Index(logs, "insurance_catalog_installed revision=rev-a previous_revision=none")
	listening := strings.Index(logs, "Server listening")
	if installed < 0 || listening < 0 || installed > listening {
		t.Fatalf("catalog was not installed before listening; output:\n%s", logs)
	}
}
