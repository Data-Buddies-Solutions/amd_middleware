package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func patientVisitsHandler(t *testing.T, request *map[string]any, response string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if request != nil {
			if err := json.NewDecoder(r.Body).Decode(request); err != nil {
				t.Fatalf("decode request: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response))
	})
}

func TestGetPatientVisitsRequestsStartAndStatusForOnePatient(t *testing.T) {
	var request map[string]any
	client, token, cleanup := newTestXMLRPCClient(t, patientVisitsHandler(t, &request, `{
		"PPMDResults": {
			"Results": {
				"@visitcount": "2",
				"visitlist": {
					"visit": [
						{"@id": "9907455", "@visitstartdatetime": "2026-11-20T08:30:00", "@apptstatus": "0"},
						{"@id": "10116371", "@visitstartdatetime": "2027-01-12T10:00:00", "@apptstatus": "10"}
					]
				}
			},
			"Error": null
		}
	}`))
	defer cleanup()

	visits, err := client.GetPatientVisits(context.Background(), token, "6035620")
	if err != nil {
		t.Fatalf("GetPatientVisits() error = %v", err)
	}
	want := []PatientVisit{
		{Start: time.Date(2026, time.November, 20, 8, 30, 0, 0, time.UTC), Status: "0"},
		{Start: time.Date(2027, time.January, 12, 10, 0, 0, 0, time.UTC), Status: "10"},
	}
	if len(visits) != len(want) {
		t.Fatalf("GetPatientVisits() = %+v, want %+v", visits, want)
	}
	for i := range want {
		if visits[i] != want[i] {
			t.Fatalf("visit %d = %+v, want %+v", i, visits[i], want[i])
		}
	}

	message := request["ppmdmsg"].(map[string]any)
	if message["@action"] != "getpatientvisits" || message["@class"] != "api" || message["@patientid"] != "6035620" {
		t.Fatalf("ppmdmsg = %#v", message)
	}
	fields, ok := message["visit"].(map[string]any)
	if !ok || fields["@visitstartdatetime"] != "VisitStartDateTime" || fields["@apptstatus"] != "ApptStatus" {
		t.Fatalf("requested visit fields = %#v", message["visit"])
	}
}

func TestGetPatientVisitsAcceptsOneOrNoVisits(t *testing.T) {
	for name, tc := range map[string]struct {
		response string
		want     int
	}{
		"single object": {response: `{"PPMDResults": {"Results": {"@visitcount": "1", "visitlist": {"visit":
			{"@id": "10116256", "@visitstartdatetime": "2026-10-26T12:00:00", "@apptstatus": "0"}}}, "Error": null}}`, want: 1},
		"no visits": {response: `{"PPMDResults": {"Results": {"@visitcount": "0", "visitlist": null}, "Error": null}}`, want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			client, token, cleanup := newTestXMLRPCClient(t, patientVisitsHandler(t, nil, tc.response))
			defer cleanup()
			visits, err := client.GetPatientVisits(context.Background(), token, "6061099")
			if err != nil || len(visits) != tc.want {
				t.Fatalf("GetPatientVisits() = %+v, %v; want %d visits", visits, err, tc.want)
			}
		})
	}
}

func TestGetPatientVisitsRejectsUnprovableVisitLists(t *testing.T) {
	for name, response := range map[string]string{
		"count mismatch": `{"PPMDResults": {"Results": {"@visitcount": "2", "visitlist": {"visit":
			{"@id": "1", "@visitstartdatetime": "2026-10-26T12:00:00", "@apptstatus": "0"}}}, "Error": null}}`,
		"missing count": `{"PPMDResults": {"Results": {"visitlist": null}, "Error": null}}`,
		"unparseable start": `{"PPMDResults": {"Results": {"@visitcount": "1", "visitlist": {"visit":
			{"@id": "1", "@visitstartdatetime": "soon", "@apptstatus": "0"}}}, "Error": null}}`,
		"missing status": `{"PPMDResults": {"Results": {"@visitcount": "1", "visitlist": {"visit":
			{"@id": "1", "@visitstartdatetime": "2026-10-26T12:00:00"}}}, "Error": null}}`,
		"provider fault": `{"PPMDResults": {"Results": null, "Error": {"Fault": {"detail": {"code": "-2147220473", "description": "Bad patient"}}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			client, token, cleanup := newTestXMLRPCClient(t, patientVisitsHandler(t, nil, response))
			defer cleanup()
			if visits, err := client.GetPatientVisits(context.Background(), token, "6061099"); err == nil {
				t.Fatalf("GetPatientVisits() = %+v, want error", visits)
			}
		})
	}
}

func TestGetAppointmentsByDayReadsOneDayOfColumns(t *testing.T) {
	client, token, cleanup := newTestRestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/scheduler/appointments" || query.Get("forView") != "day" || query.Get("isLegacy") != "true" ||
			query.Get("columnId") != "1513-1593" || query.Get("startDate") != "2026-11-20" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":9907455,"startdatetime":"2026-11-20T08:30:00","columnid":1513,"patientid":6035620,"appointmenttypeids":[1007]}]`))
	}))
	defer cleanup()

	rows, err := client.GetAppointmentsByDay(context.Background(), token, "1513-1593", "2026-11-20")
	if err != nil || len(rows) != 1 || rows[0].ID != 9907455 || rows[0].ColumnID != 1513 || rows[0].PatientID != 6035620 {
		t.Fatalf("GetAppointmentsByDay() = %+v, %v", rows, err)
	}
}
