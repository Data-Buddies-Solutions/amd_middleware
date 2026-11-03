package clients

import (
	"context"
	"net/http"
	"testing"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/session"
)

func TestScheduleOccupancyRejectsLostRows(t *testing.T) {
	for _, body := range []string{`[{"startdatetime":"invalid"}]`, `{}`, `[{"startdatetime":"2026-09-15T09:00:00","duration":15},{"startdatetime":"invalid"}]`} {
		t.Run(body, func(t *testing.T) {
			client, token, close := newTestRestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer close()
			if _, err := readScheduledDay(client, token, "1513", "2026-09-15"); err == nil {
				t.Error("lost appointment rows must not prove empty occupancy")
			}
			if _, err := client.GetBlockHolds(context.Background(), token, "1513", "2026-09-15"); err == nil {
				t.Error("lost hold rows must not prove empty occupancy")
			}
		})
	}
}

func TestSchedulerSetupRequiresResults(t *testing.T) {
	for _, body := range []string{`{"PPMDResults":{"Results":{"columnlist":{"column":false}}}}`, `{"PPMDResults":{"Results":{"columnlist":{"column":[false]}}}}`, `{}`, `null`, `{"PPMDResults":{"Results":null}}`, `{"PPMDResults":{"Error":{"Fault":{"detail":"rejected"}},"Results":{}}}`} {
		t.Run(body, func(t *testing.T) {
			client, token, close := newTestXMLRPCClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer close()
			if _, err := client.GetSchedulerSetup(context.Background(), token); err == nil {
				t.Fatal("invalid setup must not be cached as an empty schedule")
			}
		})
	}
}

func TestSchedulerSetupAllowsExplicitEmptyLists(t *testing.T) {
	client, token, close := newTestXMLRPCClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"PPMDResults":{"Results":{"columnlist":{"column":[]},"profilelist":{"profile":[]},"facilitylist":{"facility":[]}}}}`))
	}))
	defer close()
	if _, err := client.GetSchedulerSetup(context.Background(), token); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerSetupRejectsMissingColumns(t *testing.T) {
	for _, columnList := range []string{`{}`, `{"column":null}`} {
		client, token, close := newTestXMLRPCClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"PPMDResults":{"Results":{"columnlist":` + columnList + `,"profilelist":{"profile":[]},"facilitylist":{"facility":[]}}}}`))
		}))
		_, err := client.GetSchedulerSetup(context.Background(), token)
		close()
		if err == nil {
			t.Fatalf("columnlist %s: GetSchedulerSetup() error = nil, want missing columns error", columnList)
		}
	}
}

func TestSchedulerSetupPreservesProviderFieldVariants(t *testing.T) {
	client, token, close := newTestXMLRPCClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"PPMDResults":{"Results":{"ColumnList":{"column":{"@id":"col1","@profile":"prof1"}}}}}`))
	}))
	defer close()
	setup, err := client.GetSchedulerSetup(context.Background(), token)
	if err != nil || len(setup.Columns) != 1 || setup.Columns[0].ID != 1 {
		t.Fatalf("setup=%+v error=%v", setup, err)
	}
}

func readScheduledDay(client *AdvancedMDRestClient, token *session.TokenData, columnID, date string) ([]domain.Appointment, error) {
	rows, err := client.GetAppointmentsByDay(context.Background(), token, columnID, date)
	if err != nil {
		return nil, err
	}
	return ScheduledAppointments(rows)
}
