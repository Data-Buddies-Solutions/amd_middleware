package clients

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/session"
)

func newTestRestClient(t *testing.T, handler http.Handler) (*AdvancedMDRestClient, *session.TokenData, func()) {
	t.Helper()
	server := httptest.NewTLSServer(handler)

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	restBase := server.URL[8:]

	tokenData := &session.TokenData{
		Token:       "Bearer test-token",
		RestApiBase: restBase,
	}

	return NewAdvancedMDRestClient(httpClient), tokenData, server.Close
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

func TestScheduleReadLogsUpstreamStatus(t *testing.T) {
	for _, tc := range []struct {
		operation string
		status    int
		read      func(*AdvancedMDRestClient, *session.TokenData) error
	}{
		{"block holds", http.StatusServiceUnavailable, func(c *AdvancedMDRestClient, token *session.TokenData) error {
			_, err := c.GetBlockHolds(context.Background(), token, "1513", "2026-03-03")
			return err
		}},
		{"monthly appointments", http.StatusForbidden, func(c *AdvancedMDRestClient, token *session.TokenData) error {
			_, err := c.GetAppointmentsByMonth(context.Background(), token, "1513", "2026-03-01")
			return err
		}},
		{"daily appointments", http.StatusBadGateway, func(c *AdvancedMDRestClient, token *session.TokenData) error {
			_, err := c.GetAppointmentsByDay(context.Background(), token, "1513", "2026-03-03")
			return err
		}},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			var logs bytes.Buffer
			previousWriter := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previousWriter) })

			const responseBody = "synthetic-private-response-marker"
			client, token, cleanup := newTestRestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, responseBody)
			}))
			t.Cleanup(cleanup)

			if err := tc.read(client, token); err == nil {
				t.Fatal("expected upstream response to remain an error")
			}
			want := fmt.Sprintf("provider=advancedmd operation=%q upstream_http_status=%d", tc.operation, tc.status)
			if !strings.Contains(logs.String(), want) {
				t.Errorf("missing upstream diagnostic %q", want)
			}
			for _, sensitive := range []string{responseBody, token.Token, token.RestApiBase} {
				if strings.Contains(logs.String(), sensitive) {
					t.Error("upstream diagnostic included sensitive response or request data")
				}
			}
		})
	}
}

func TestGetAppointmentsByMonth_ReturnsAppointments(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("forView") != "month" {
			t.Errorf("Expected forView=month, got %s", r.URL.Query().Get("forView"))
		}
		if r.URL.Query().Get("columnId") != "1513-1550-1551" {
			t.Errorf("Expected columnId=1513-1550-1551, got %s", r.URL.Query().Get("columnId"))
		}
		if r.URL.Query().Get("startDate") != "2026-03-01" {
			t.Errorf("Expected startDate=2026-03-01, got %s", r.URL.Query().Get("startDate"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"id":1001,"startdatetime":"2026-03-12T09:00:00","duration":15,"columnid":1513,"profileid":620,"provider":"BACH, AUSTIN","heading":"BACH","facility":"ABITA EYE GROUP SPRING HILL","facilityid":1568,"appointmenttypeids":[1006],"patientid":12345,"firstname":"John","lastname":"Smith","confirmdate":"2026-03-10T14:00:00","confirmmethod":"phone"},
			{"id":1002,"startdatetime":"2026-03-12T10:00:00","duration":30,"columnid":1550,"profileid":2076,"provider":"NOEL, DON HERSHELSON","facility":"ABITA EYE GROUP SPRING HILL","facilityid":1568,"appointmenttypeids":[1007],"patientid":67890,"firstname":"Jane","lastname":"Doe","confirmdate":null}
		]`))
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

	if appts[0].PatientID != 12345 {
		t.Errorf("Expected PatientID 12345, got %d", appts[0].PatientID)
	}
	if appts[0].Provider != "BACH, AUSTIN" {
		t.Errorf("Expected provider 'BACH, AUSTIN', got %q", appts[0].Provider)
	}
	if appts[0].Facility != "ABITA EYE GROUP SPRING HILL" {
		t.Errorf("Expected facility, got %q", appts[0].Facility)
	}
	if appts[1].ID != 1002 || appts[1].PatientID != 67890 || appts[1].AppointmentTypes[0] != 1007 {
		t.Errorf("second appointment = %+v", appts[1])
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
		if payload["comments"] != "Appointment reason: blurry vision\nReferring doctor: Dr. Smith\n- AI" {
			t.Fatalf("comments = %v, want appointment comments in payload %#v", payload["comments"], payload)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(bookAppointmentResponse{ID: 98765})
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
		Comments:   "Appointment reason: blurry vision\nReferring doctor: Dr. Smith\n- AI",
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
		if r.Method != http.MethodPut {
			t.Errorf("Expected PUT method, got %s", r.Method)
		}

		if !strings.Contains(r.URL.Path, "/scheduler/appointments/9570263/cancel") {
			t.Errorf("Expected path to contain /scheduler/appointments/9570263/cancel, got %s", r.URL.Path)
		}

		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Expected Authorization 'Bearer test-token', got %q", r.Header.Get("Authorization"))
		}

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

func TestScheduleReadsRejectEmptyResponseBodies(t *testing.T) {
	client, token, cleanup := newTestRestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer cleanup()

	if _, err := readScheduledDay(client, token, "1513", "2026-03-01"); err == nil {
		t.Fatal("empty appointments body must not read as an empty schedule")
	}
	if _, err := client.GetBlockHolds(context.Background(), token, "1513", "2026-03-01"); err == nil {
		t.Fatal("empty block holds body must not read as no holds")
	}
	if _, err := client.GetAppointmentsByMonth(context.Background(), token, "1513", "2026-03-01"); err == nil {
		t.Fatal("empty monthly body must not read as no appointments")
	}
}

func TestGetAppointmentsByMonthAcceptsSingleObject(t *testing.T) {
	client, token, cleanup := newTestRestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"id": 42, "startdatetime": "2026-03-18T12:00:00", "patientid": 123}`))
	}))
	defer cleanup()

	appointments, err := client.GetAppointmentsByMonth(context.Background(), token, "1513", "2026-03-01")
	if err != nil || len(appointments) != 1 || appointments[0].ID != 42 {
		t.Fatalf("GetAppointmentsByMonth() = %+v, err = %v", appointments, err)
	}
}

type restCall struct {
	name    string
	method  string
	write   bool
	success string
	send    func(context.Context, *AdvancedMDRestClient, *session.TokenData) error
}

var restCalls = []restCall{
	{"read", http.MethodGet, false, `[{"id":1,"startdatetime":"2026-03-03T09:00:00"}]`, func(ctx context.Context, c *AdvancedMDRestClient, token *session.TokenData) error {
		_, err := c.GetAppointmentsByDay(ctx, token, "1513", "2026-03-03")
		return err
	}},
	{"book", http.MethodPost, true, `{"id":7}`, func(ctx context.Context, c *AdvancedMDRestClient, token *session.TokenData) error {
		_, err := c.BookAppointment(ctx, token, BookAppointmentParams{PatientID: 1})
		return err
	}},
	{"cancel", http.MethodPut, true, ``, func(ctx context.Context, c *AdvancedMDRestClient, token *session.TokenData) error {
		return c.CancelAppointment(ctx, token, 9)
	}},
}

func TestRestRequestsClassifyEveryProviderStatus(t *testing.T) {
	const (
		none     = safeerrors.CategoryNone
		auth     = safeerrors.CategoryAuthentication
		conflict = safeerrors.CategoryConflict
		rejected = safeerrors.CategoryRejected
		upstream = safeerrors.CategoryUpstreamStatus
		invalid  = safeerrors.CategoryInvalidResponse
	)
	for _, tc := range []struct {
		status int
		want   map[string]safeerrors.Category
	}{
		{http.StatusOK, map[string]safeerrors.Category{"read": none, "book": none, "cancel": none}},
		{http.StatusCreated, map[string]safeerrors.Category{"read": upstream, "book": none, "cancel": none}},
		{http.StatusNoContent, map[string]safeerrors.Category{"read": upstream, "book": invalid, "cancel": none}},
		{http.StatusBadRequest, map[string]safeerrors.Category{"read": upstream, "book": rejected, "cancel": rejected}},
		{http.StatusUnauthorized, map[string]safeerrors.Category{"read": auth, "book": auth, "cancel": auth}},
		{http.StatusForbidden, map[string]safeerrors.Category{"read": auth, "book": auth, "cancel": auth}},
		{http.StatusNotFound, map[string]safeerrors.Category{"read": upstream, "book": rejected, "cancel": rejected}},
		{http.StatusRequestTimeout, map[string]safeerrors.Category{"read": upstream, "book": upstream, "cancel": upstream}},
		{http.StatusConflict, map[string]safeerrors.Category{"read": upstream, "book": conflict, "cancel": conflict}},
		{http.StatusUnprocessableEntity, map[string]safeerrors.Category{"read": upstream, "book": rejected, "cancel": rejected}},
		{http.StatusTooManyRequests, map[string]safeerrors.Category{"read": upstream, "book": rejected, "cancel": rejected}},
		{http.StatusInternalServerError, map[string]safeerrors.Category{"read": upstream, "book": upstream, "cancel": upstream}},
		{http.StatusBadGateway, map[string]safeerrors.Category{"read": upstream, "book": upstream, "cancel": upstream}},
		{http.StatusServiceUnavailable, map[string]safeerrors.Category{"read": upstream, "book": upstream, "cancel": upstream}},
	} {
		for _, call := range restCalls {
			t.Run(fmt.Sprintf("%s %d", call.name, tc.status), func(t *testing.T) {
				var logs bytes.Buffer
				previousWriter := log.Writer()
				log.SetOutput(&logs)
				t.Cleanup(func() { log.SetOutput(previousWriter) })

				client, token, cleanup := newTestRestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					wantContentType := ""
					if call.write {
						wantContentType = "application/json"
					}
					if r.Method != call.method || r.Header.Get("Authorization") != "Bearer test-token" ||
						r.Header.Get("Accept") != "application/json" || r.Header.Get("Content-Type") != wantContentType {
						t.Errorf("request = %s Authorization=%q Accept=%q Content-Type=%q", r.Method,
							r.Header.Get("Authorization"), r.Header.Get("Accept"), r.Header.Get("Content-Type"))
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, call.success)
				}))
				t.Cleanup(cleanup)

				err := call.send(context.Background(), client, token)
				want := tc.want[call.name]
				if got := safeerrors.Classify(err); got != want || safeerrors.IsNotSent(err) {
					t.Fatalf("error = %v, category = %q, not sent = %t; want a sent %q", err, got, safeerrors.IsNotSent(err), want)
				}
				var status *httpStatusError
				if errors.As(err, &status) != (want != none && want != invalid) {
					t.Fatalf("error = %v, want an HTTP status error only for a refused status", err)
				}
				if status != nil && status.statusCode != tc.status {
					t.Fatalf("status error code = %d, want %d", status.statusCode, tc.status)
				}
				logged := strings.Contains(logs.String(), "upstream_http_status=")
				if wantLog := !call.write && tc.status != http.StatusOK; logged != wantLog {
					t.Fatalf("logged upstream status = %t, want %t", logged, wantLog)
				}
			})
		}
	}
}

func TestRestRequestsClassifyResponseBodies(t *testing.T) {
	const (
		none    = safeerrors.CategoryNone
		invalid = safeerrors.CategoryInvalidResponse
	)
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    map[string]safeerrors.Category
	}{
		{"empty", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
			map[string]safeerrors.Category{"read": invalid, "book": invalid, "cancel": none}},
		{"whitespace", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, " \n") },
			map[string]safeerrors.Category{"read": invalid, "book": invalid, "cancel": none}},
		{"malformed", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"id":`) },
			map[string]safeerrors.Category{"read": invalid, "book": invalid, "cancel": none}},
		{"zero booking ID", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"id":0}`) },
			map[string]safeerrors.Category{"read": none, "book": invalid, "cancel": none}},
		{"truncated", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"id"`)
		}, map[string]safeerrors.Category{"read": invalid, "book": invalid, "cancel": none}},
	} {
		for _, call := range restCalls {
			t.Run(tc.name+" "+call.name, func(t *testing.T) {
				client, token, cleanup := newTestRestClient(t, tc.handler)
				t.Cleanup(cleanup)

				err := call.send(context.Background(), client, token)
				want := tc.want[call.name]
				if got := safeerrors.Classify(err); got != want || safeerrors.IsNotSent(err) {
					t.Fatalf("error = %v, category = %q, not sent = %t; want a sent %q", err, got, safeerrors.IsNotSent(err), want)
				}
			})
		}
	}
}

func TestRestRequestsSeparateUnsentRequestsFromTransportFailures(t *testing.T) {
	for _, call := range restCalls {
		t.Run(call.name, func(t *testing.T) {
			var requests atomic.Int32
			client, token, cleanup := newTestRestClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requests.Add(1)
			}))
			base := token.RestApiBase

			token.RestApiBase = "provider host/api"
			err := call.send(context.Background(), client, token)
			if !safeerrors.IsNotSent(err) || safeerrors.Classify(err) != safeerrors.CategoryInternal {
				t.Fatalf("unbuildable request error = %v, category = %q; want a not-sent internal failure", err, safeerrors.Classify(err))
			}

			token.RestApiBase = base
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			err = call.send(canceled, client, token)
			if safeerrors.IsNotSent(err) || safeerrors.Classify(err) != "canceled" {
				t.Fatalf("canceled request error = %v, category = %q; want a sent canceled failure", err, safeerrors.Classify(err))
			}

			cleanup()
			err = call.send(context.Background(), client, token)
			if safeerrors.IsNotSent(err) || safeerrors.Classify(err) != safeerrors.CategoryNetwork {
				t.Fatalf("unreachable provider error = %v, category = %q; want a sent network failure", err, safeerrors.Classify(err))
			}
			if got := requests.Load(); got != 0 {
				t.Fatalf("provider requests = %d, want none", got)
			}
		})
	}
}
