package clients

import (
	"context"
	"net/http"
	"testing"
)

func TestScheduleOccupancyRejectsLostRows(t *testing.T) {
	for _, body := range []string{`[{"startdatetime":"invalid"}]`, `{}`, `[{"startdatetime":"2026-09-15T09:00:00","duration":15},{"startdatetime":"invalid"}]`} {
		t.Run(body, func(t *testing.T) {
			client, token, close := newTestRestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer close()
			if _, err := client.GetAppointments(context.Background(), token, "1513", "2026-09-15"); err == nil {
				t.Error("lost appointment rows must not prove empty occupancy")
			}
			if _, err := client.GetBlockHolds(context.Background(), token, "1513", "2026-09-15"); err == nil {
				t.Error("lost hold rows must not prove empty occupancy")
			}
		})
	}
}

func TestSchedulerSetupRequiresResults(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"PPMDResults":{"Results":null}}`, `{"PPMDResults":{"Error":{"Fault":{"detail":"rejected"}},"Results":{}}}`} {
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

// Encoding/json accepts provider field capitalization variants; envelope
// validation must preserve that behavior and tolerate inactive column settings.
func TestSchedulerSetupPreservesProviderFieldVariants(t *testing.T) {
	client, token, close := newTestXMLRPCClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"PPMDResults":{"Results":{"ColumnList":{"column":{"@id":"col1","@profile":"prof1"}}}}}`))
	}))
	defer close()
	setup, err := client.GetSchedulerSetup(context.Background(), token)
	if err != nil || len(setup.Columns) != 1 || setup.Columns[0].ID != "1" {
		t.Fatalf("setup=%+v error=%v", setup, err)
	}
}
