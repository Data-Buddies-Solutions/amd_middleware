package clients

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
)

// newTestRestClient creates a TLS test server and REST client wired together.
// The handler receives all requests. Returns the client, tokenData pointing at the server, and a cleanup func.
func newTestRestClient(t *testing.T, handler http.Handler) (*AdvancedMDRestClient, *domain.TokenData, func()) {
	t.Helper()
	server := httptest.NewTLSServer(handler)

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// Strip "https://" from server.URL to match RestApiBase format
	restBase := server.URL[8:]

	tokenData := &domain.TokenData{
		Token:       "Bearer test-token",
		RestApiBase: restBase,
	}

	return NewAdvancedMDRestClient(httpClient), tokenData, server.Close
}

func TestGetAppointmentsForColumns_Concurrent(t *testing.T) {
	var callCount int64

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&callCount, 1)
		// Simulate AMD latency
		time.Sleep(50 * time.Millisecond)

		colID := r.URL.Query().Get("columnId")
		appts := []AMDAppointmentResponse{
			{
				ID:            1,
				StartDateTime: fmt.Sprintf("2026-03-03T09:00:00"),
				Duration:      15,
				ColumnID:      0,
				PatientID:     100,
			},
		}
		// Tag the response with the column ID so we can verify correct mapping
		if colID == "1513" {
			appts[0].ColumnID = 1513
		} else if colID == "1551" {
			appts[0].ColumnID = 1551
		} else if colID == "1550" {
			appts[0].ColumnID = 1550
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(appts)
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	columnIDs := []string{"1513", "1551", "1550"}

	start := time.Now()
	result := client.GetAppointmentsForColumns(context.Background(), tokenData, columnIDs, "2026-03-03")
	elapsed := time.Since(start)

	// Verify all 3 columns returned
	if len(result) != 3 {
		t.Fatalf("Expected 3 columns in result, got %d", len(result))
	}

	// Verify correct mapping (each column got its own data)
	for _, colID := range columnIDs {
		appts, ok := result[colID]
		if !ok {
			t.Errorf("Missing results for column %s", colID)
			continue
		}
		if len(appts) != 1 {
			t.Errorf("Column %s: expected 1 appointment, got %d", colID, len(appts))
		}
	}

	// Verify all 3 calls were made
	if atomic.LoadInt64(&callCount) != 3 {
		t.Errorf("Expected 3 HTTP calls, got %d", callCount)
	}

	// Verify concurrency: 3 calls x 50ms each should take ~50-100ms concurrent, not ~150ms+ sequential
	if elapsed > 140*time.Millisecond {
		t.Errorf("Expected concurrent execution (~50-100ms), but took %v (likely sequential)", elapsed)
	}
}

func TestGetBlockHoldsForColumns_Concurrent(t *testing.T) {
	var callCount int64

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&callCount, 1)
		time.Sleep(50 * time.Millisecond)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]AMDBlockHoldResponse{})
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	columnIDs := []string{"1513", "1551", "1550"}

	start := time.Now()
	result := client.GetBlockHoldsForColumns(context.Background(), tokenData, columnIDs, "2026-03-03")
	elapsed := time.Since(start)

	if len(result) != 3 {
		t.Fatalf("Expected 3 columns in result, got %d", len(result))
	}

	if atomic.LoadInt64(&callCount) != 3 {
		t.Errorf("Expected 3 HTTP calls, got %d", callCount)
	}

	if elapsed > 140*time.Millisecond {
		t.Errorf("Expected concurrent execution (~50-100ms), but took %v (likely sequential)", elapsed)
	}
}

func TestGetBlockHolds_RecurringUsesOccurrenceDuration(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"id": 655774,
			"startdatetime": "2026-05-05T12:15:00",
			"enddatetime": "2027-12-31T13:15:00",
			"duration": 60,
			"columnid": null,
			"reason": "LUNCH",
			"recurrence": {
				"recurrencetype": 1,
				"interval": 0,
				"daysofweek": null
			}
		}`))
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	holds, err := client.GetBlockHolds(context.Background(), tokenData, "1600", "2026-05-05")
	if err != nil {
		t.Fatalf("GetBlockHolds failed: %v", err)
	}
	if len(holds) != 1 {
		t.Fatalf("Expected 1 hold, got %d", len(holds))
	}

	wantEnd := time.Date(2026, 5, 5, 13, 15, 0, 0, time.UTC)
	if !holds[0].EndDateTime.Equal(wantEnd) {
		t.Fatalf("Expected recurring hold end %s, got %s", wantEnd, holds[0].EndDateTime)
	}
}

func TestGetBlockHolds_NonRecurringUsesEndDateTime(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"id": 655775,
			"startdatetime": "2026-05-05T08:00:00",
			"enddatetime": "2026-05-08T17:00:00",
			"duration": 540,
			"columnid": 1600,
			"reason": "OUT OF THE OFFICE"
		}`))
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	holds, err := client.GetBlockHolds(context.Background(), tokenData, "1600", "2026-05-05")
	if err != nil {
		t.Fatalf("GetBlockHolds failed: %v", err)
	}
	if len(holds) != 1 {
		t.Fatalf("Expected 1 hold, got %d", len(holds))
	}

	wantEnd := time.Date(2026, 5, 8, 17, 0, 0, 0, time.UTC)
	if !holds[0].EndDateTime.Equal(wantEnd) {
		t.Fatalf("Expected non-recurring hold end %s, got %s", wantEnd, holds[0].EndDateTime)
	}
}

func TestGetAppointmentsForColumns_PartialFailure(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		colID := r.URL.Query().Get("columnId")
		if colID == "1551" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("AMD is down"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]AMDAppointmentResponse{})
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	result := client.GetAppointmentsForColumns(context.Background(), tokenData, []string{"1513", "1551", "1550"}, "2026-03-03")

	// Successful columns should be present
	if _, ok := result["1513"]; !ok {
		t.Error("Expected column 1513 in results (succeeded)")
	}
	if _, ok := result["1550"]; !ok {
		t.Error("Expected column 1550 in results (succeeded)")
	}

	// Failed column should be absent
	if _, ok := result["1551"]; ok {
		t.Error("Expected column 1551 to be absent from results (failed)")
	}
}

func TestGetAppointmentsForColumns_EmptyColumns(t *testing.T) {
	client, tokenData, cleanup := newTestRestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("No HTTP calls should be made for empty column list")
	}))
	defer cleanup()

	result := client.GetAppointmentsForColumns(context.Background(), tokenData, []string{}, "2026-03-03")
	if len(result) != 0 {
		t.Errorf("Expected empty result, got %d entries", len(result))
	}
}

func TestGetAppointmentsForColumns_SingleColumn(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]AMDAppointmentResponse{
			{ID: 1, StartDateTime: "2026-03-03T10:00:00", Duration: 15, ColumnID: 1513},
		})
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	result := client.GetAppointmentsForColumns(context.Background(), tokenData, []string{"1513"}, "2026-03-03")
	if len(result) != 1 {
		t.Fatalf("Expected 1 column, got %d", len(result))
	}
	if len(result["1513"]) != 1 {
		t.Errorf("Expected 1 appointment for column 1513, got %d", len(result["1513"]))
	}
}

func TestGetAppointmentsByMonth_ReturnsAppointments(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify correct query params
		if r.URL.Query().Get("forView") != "month" {
			t.Errorf("Expected forView=month, got %s", r.URL.Query().Get("forView"))
		}
		if r.URL.Query().Get("columnId") != "1513-1550-1551" {
			t.Errorf("Expected columnId=1513-1550-1551, got %s", r.URL.Query().Get("columnId"))
		}
		if r.URL.Query().Get("startDate") != "2026-03-01" {
			t.Errorf("Expected startDate=2026-03-01, got %s", r.URL.Query().Get("startDate"))
		}

		confirm := "2026-03-10T14:00:00"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]AMDAppointmentResponse{
			{
				ID:               1001,
				StartDateTime:    "2026-03-12T09:00:00",
				Duration:         15,
				ColumnID:         1513,
				ProfileID:        620,
				Provider:         "BACH, AUSTIN",
				Facility:         "ABITA EYE GROUP SPRING HILL",
				FacilityID:       1568,
				AppointmentTypes: []int{1006},
				PatientID:        12345,
				FirstName:        "John",
				LastName:         "Smith",
				ConfirmDate:      &confirm,
			},
			{
				ID:               1002,
				StartDateTime:    "2026-03-12T10:00:00",
				Duration:         30,
				ColumnID:         1550,
				ProfileID:        2076,
				Provider:         "NOEL, DON HERSHELSON",
				Facility:         "ABITA EYE GROUP SPRING HILL",
				FacilityID:       1568,
				AppointmentTypes: []int{1007},
				PatientID:        67890,
				FirstName:        "Jane",
				LastName:         "Doe",
				ConfirmDate:      nil,
			},
		})
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	appts, err := client.GetAppointmentsByMonth(context.Background(), tokenData, "1513-1550-1551", "2026-03-01")
	if err != nil {
		t.Fatalf("GetAppointmentsByMonth failed: %v", err)
	}

	if len(appts) != 2 {
		t.Fatalf("Expected 2 appointments, got %d", len(appts))
	}

	// Verify first appointment fields
	if appts[0].PatientID != 12345 {
		t.Errorf("Expected PatientID 12345, got %d", appts[0].PatientID)
	}
	if appts[0].Provider != "BACH, AUSTIN" {
		t.Errorf("Expected provider 'BACH, AUSTIN', got %q", appts[0].Provider)
	}
	if appts[0].Facility != "ABITA EYE GROUP SPRING HILL" {
		t.Errorf("Expected facility, got %q", appts[0].Facility)
	}
	if appts[0].ConfirmDate == nil {
		t.Error("Expected ConfirmDate to be non-nil for first appointment")
	}

	// Verify second appointment has nil ConfirmDate
	if appts[1].ConfirmDate != nil {
		t.Error("Expected ConfirmDate to be nil for second appointment")
	}
}

func TestGetAppointmentsByMonth_EmptyResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]AMDAppointmentResponse{})
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	appts, err := client.GetAppointmentsByMonth(context.Background(), tokenData, "1513", "2026-03-01")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(appts) != 0 {
		t.Errorf("Expected 0 appointments, got %d", len(appts))
	}
}

func TestGetAppointmentsByMonth_ServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("AMD is down"))
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	_, err := client.GetAppointmentsByMonth(context.Background(), tokenData, "1513", "2026-03-01")
	if err == nil {
		t.Fatal("Expected error on server 500, got nil")
	}
}

func TestBookAppointment_IncludesForceWhenSet(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/scheduler/Appointments") {
			t.Errorf("Expected scheduler appointments path, got %s", r.URL.Path)
		}

		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if payload["force"] != float64(1) {
			t.Fatalf("force = %v, want 1 in payload %#v", payload["force"], payload)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(BookAppointmentResponse{ID: 98765})
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	apptID, err := client.BookAppointment(context.Background(), tokenData, BookAppointmentParams{
		PatientID:     12345,
		ColumnID:      1513,
		ProfileID:     620,
		StartDatetime: "2026-06-01T09:00",
		Duration:      15,
		AppointmentType: []struct {
			ID int `json:"id"`
		}{{ID: 1007}},
		EpisodeID:  1,
		FacilityID: 1568,
		Color:      "#FF0000",
		Force:      1,
	})
	if err != nil {
		t.Fatalf("BookAppointment failed: %v", err)
	}
	if apptID != 98765 {
		t.Fatalf("appointment ID = %d, want 98765", apptID)
	}
}

func TestCancelAppointment_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify PUT method
		if r.Method != http.MethodPut {
			t.Errorf("Expected PUT method, got %s", r.Method)
		}

		// Verify URL path
		if !strings.Contains(r.URL.Path, "/scheduler/appointments/9570263/cancel") {
			t.Errorf("Expected path to contain /scheduler/appointments/9570263/cancel, got %s", r.URL.Path)
		}

		// Verify Authorization header
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Expected Authorization 'Bearer test-token', got %q", r.Header.Get("Authorization"))
		}

		// Verify request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("Failed to read request body: %v", err)
		}
		bodyStr := string(body)
		if !strings.Contains(bodyStr, `"id":9570263`) {
			t.Errorf("Expected body to contain '\"id\":9570263', got %s", bodyStr)
		}

		w.WriteHeader(http.StatusOK)
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	err := client.CancelAppointment(context.Background(), tokenData, 9570263)
	if err != nil {
		t.Fatalf("CancelAppointment failed: %v", err)
	}
}

func TestCancelAppointment_ServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("AMD is down"))
	})

	client, tokenData, cleanup := newTestRestClient(t, handler)
	defer cleanup()

	err := client.CancelAppointment(context.Background(), tokenData, 9570263)
	if err == nil {
		t.Fatal("Expected error on server 500, got nil")
	}
}
