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

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	apphttp "advancedmd-token-management/internal/http"
	"advancedmd-token-management/internal/patient"
	"advancedmd-token-management/internal/safeerrors"
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

// Validate actual middleware responses through the strict s2s patient parser,
// model-facing tool text, and a subsequent signed booking.
func TestPythonPatientReadContract(t *testing.T) {
	python := os.Getenv("PYTHON_SCHEDULING_WORKTREE")
	if python == "" {
		t.Skip("set PYTHON_SCHEDULING_WORKTREE to run the cross-repository contract")
	}
	script, err := filepath.Abs("testdata/s2s_patient_contract.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"found", "none", "incomplete", "partial", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			records, _, _ := rescheduleFixture(t)
			demographics := records.Demographics["12345"]
			demographics.FullName = "DOE,JANE"
			records.Demographics["12345"] = demographics
			records.CandidateReads["Jane"] = domain.PatientCandidateRead{Complete: true, Patients: []domain.Patient{{ID: "12345", FullName: "DOE,JANE", FirstName: "Jane", LastName: "Doe", DOB: "01/15/1980"}}}
			expected := scenario
			read := records.AppointmentResults["12345"]
			switch scenario {
			case "none":
				read.Read.Appointments = nil
			case "incomplete":
				read.Read.Appointments = nil
				read.Read.Complete = false
				expected = "error"
			case "partial":
				read.Read.Complete = false
				expected = "error"
			case "failure":
				read.Err = advancedmd.NewError(safeerrors.CategoryUnavailable)
				expected = "error"
			}
			records.AppointmentResults["12345"] = read
			for i := 1; i <= 16; i++ {
				day := mutationTestNow().AddDate(0, 0, i).Format("2006-01-02")
				records.ScheduleReads[day] = completeRead("1513", nil, nil)
			}
			scheduler := scheduling.New(records, "test-booking-secret", mutationTestNow)
			tokens := scheduling.NewAppointmentTokens("test-booking-secret", mutationTestNow)
			server := httptest.NewServer(apphttp.NewRouter(apphttp.NewHandlers(nil, patient.NewWithAppointmentTokens(records, tokens), scheduler), "test-auth", nil))
			defer server.Close()
			cmd := exec.Command("uv", "run", "--no-sync", "python", script, server.URL, expected)
			cmd.Dir = python
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Python patient contract: %v\n%s", err, output)
			}
			t.Log(string(output))
			if len(records.Bookings) != 1 || len(records.Cancellations) != 0 {
				t.Fatalf("provider writes=%d/%d", len(records.Bookings), len(records.Cancellations))
			}
		})
	}
}

// Only the external provider is replaced. Python must exercise the real HTTP
// envelopes and call-owner recovery decisions against this fixture.
func TestPythonOwnershipContract(t *testing.T) {
	python := os.Getenv("PYTHON_SCHEDULING_WORKTREE")
	if python == "" {
		t.Skip("set PYTHON_SCHEDULING_WORKTREE to run the cross-repository contract")
	}
	for _, scenario := range []string{
		"availability_invalid_input", "availability_policy_blocked", "availability_read_failure",
		"insurance_completed", "insurance_no_current_plan", "insurance_no_effect", "insurance_partial", "insurance_uncertain", "insurance_missing_refs",
		"cancellation_conflict", "cancellation_rejected", "cancellation_invalid_token", "cancellation_uncertain", "cancellation_completed",
	} {
		t.Run(scenario, func(t *testing.T) {
			records, _, _ := rescheduleFixture(t)
			chart := records.Demographics["12345"]
			chart.FullName = "DOE,JANE"
			chart.InsuranceStateKnown = true
			chart.RespPartyID = "fresh-party"
			chart.InsPlanID = "fresh-plan"
			chart.SubscriberNum = "OLD"
			records.CandidateReads["Jane"] = domain.PatientCandidateRead{Complete: true, Patients: []domain.Patient{{ID: "12345", FullName: "DOE,JANE", FirstName: "Jane", LastName: "Doe", DOB: "01/15/1980"}}}
			wantEnd, wantAdd, wantCancel := 0, 0, 0
			provider := &ownershipContractRecords{Adapter: records}
			switch scenario {
			case "availability_policy_blocked":
				chart.CarrierName = "Unrecognized chart insurance"
				chart.CarrierID = "unknown-carrier"
			case "availability_read_failure":
				records.SchedulerSetupError = advancedmd.NewError(safeerrors.CategoryUnavailable)
			case "insurance_completed":
				wantEnd, wantAdd = 1, 1
			case "insurance_no_current_plan":
				chart.InsPlanID = ""
				chart.CarrierName = ""
				chart.CarrierID = ""
				chart.SubscriberNum = ""
				wantAdd = 1
			case "insurance_no_effect":
				records.EndInsuranceError = advancedmd.NewError(safeerrors.CategoryRejected)
				wantEnd = 1
			case "insurance_partial":
				records.AddInsuranceError = advancedmd.NewError(safeerrors.CategoryRejected)
				wantEnd, wantAdd = 1, 1
			case "insurance_uncertain":
				records.AddInsuranceError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
				provider.failAfterAdd = true
				wantEnd, wantAdd = 1, 1
			case "insurance_missing_refs":
				chart.RespPartyID = ""
			case "cancellation_conflict":
				records.CancelAppointmentErr = advancedmd.NewError(safeerrors.CategoryConflict)
				wantCancel = 1
			case "cancellation_rejected":
				records.CancelAppointmentErr = advancedmd.NewError(safeerrors.CategoryRejected)
				wantCancel = 1
			case "cancellation_uncertain":
				records.AppointmentStateResults[54321] = advancedmdtest.AppointmentStateResult{State: advancedmd.AppointmentState{Complete: false}}
				records.CancelAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
				wantCancel = 1
			case "cancellation_completed":
				wantCancel = 1
			}
			records.Demographics["12345"] = chart
			tokens := scheduling.NewAppointmentTokens("test-booking-secret", mutationTestNow)
			router := apphttp.NewRouter(apphttp.NewHandlers(nil, patient.NewWithAppointmentTokens(provider, tokens), scheduling.New(provider, "test-booking-secret", mutationTestNow)), "test-auth", nil)
			mux := http.NewServeMux()
			mux.Handle("/", router)
			mux.HandleFunc("/fixture", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"scenario": scenario, "now": mutationTestNow().Format(time.RFC3339)})
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			cmd := exec.Command("uv", "run", "--no-sync", "python", filepath.Join("tests", "ownership_contract.py"), server.URL)
			cmd.Dir = python
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Python ownership contract: %v\n%s", err, output)
			}
			t.Log(string(output))
			if records.EndInsuranceCalls != wantEnd || records.AddInsuranceCalls != wantAdd || len(records.Cancellations) != wantCancel || len(records.Bookings) != 0 {
				t.Fatalf("provider writes end/add/cancel/book = %d/%d/%d/%d; want %d/%d/%d/0", records.EndInsuranceCalls, records.AddInsuranceCalls, len(records.Cancellations), len(records.Bookings), wantEnd, wantAdd, wantCancel)
			}
			for _, write := range records.InsuranceEnds {
				if write.InsPlanID != "fresh-plan" {
					t.Fatalf("untrusted plan: %+v", write)
				}
			}
			for _, write := range records.Insurances {
				if write.RespPartyID != "fresh-party" || write.CarrierID != "car301578" {
					t.Fatalf("untrusted replacement: %+v", write)
				}
			}
		})
	}
}

type ownershipContractRecords struct {
	*advancedmdtest.Adapter
	failAfterAdd bool
}

func (r *ownershipContractRecords) GetPatientDemographics(ctx context.Context, id string) (domain.PatientDemographics, error) {
	if r.failAfterAdd && r.AddInsuranceCalls > 0 {
		return domain.PatientDemographics{}, advancedmd.NewError(safeerrors.CategoryUnavailable)
	}
	return r.Adapter.GetPatientDemographics(ctx, id)
}
