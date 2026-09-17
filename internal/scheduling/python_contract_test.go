package scheduling_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	apphttp "advancedmd-token-management/internal/http"
	"advancedmd-token-management/internal/patient"
	"advancedmd-token-management/internal/scheduling"
)

// This explicitly opted-in test runs the real Python call owner against Go HTTP
// handlers with only AdvancedMD replaced by a test adapter.
func TestPythonSchedulingContract(t *testing.T) {
	python := os.Getenv("PYTHON_SCHEDULING_WORKTREE")
	if python == "" {
		t.Skip("set PYTHON_SCHEDULING_WORKTREE to run the cross-repository contract")
	}
	for _, scenario := range []string{"success", "partial", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			records, _, _ := rescheduleFixture(t)
			if scenario == "partial" {
				records.CancelAppointmentErr = context.DeadlineExceeded
			}
			if scenario == "failure" {
				records.BookAppointmentErr = context.DeadlineExceeded
			}
			for i := 1; i <= 16; i++ {
				day := mutationTestNow().AddDate(0, 0, i).Format("2006-01-02")
				records.ScheduleReads[day] = completeRead("1513", nil, nil)
			}
			scheduler := scheduling.New(records, "test-booking-secret", mutationTestNow)
			tokens := scheduling.NewAppointmentTokens("test-booking-secret", mutationTestNow)
			router := apphttp.NewRouter(apphttp.NewHandlers(nil, patient.NewWithAppointmentTokens(records, tokens), scheduler), "test-auth", nil)
			mux := http.NewServeMux()
			mux.Handle("/", router)
			// Only scenario and clock control are fixtures. Patient appointments
			// and their action tokens must come through the real HTTP response.
			mux.HandleFunc("/fixture", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{
					"now": mutationTestNow().Format(time.RFC3339), "scenario": scenario,
				})
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			cmd := exec.Command("uv", "run", "python", filepath.Join("tests", "middleware_contract.py"), server.URL)
			cmd.Dir = python
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Python contract: %v\n%s", err, output)
			}
			t.Log(string(output))
			wantCancel := 1
			if scenario == "failure" {
				wantCancel = 0
			}
			if len(records.Bookings) != 1 || len(records.Cancellations) != wantCancel {
				t.Fatalf("provider writes=%d/%d", len(records.Bookings), len(records.Cancellations))
			}
		})
	}
}
