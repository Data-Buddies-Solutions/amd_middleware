package advancedmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/session"
)

func TestAdapterSearchPatientsByNameUsesControlledXMLRPCServer(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"patientlist": {"@itemcount": "0"}
				}
			}
		}`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &session.TokenData{
			CookieToken: "token=test-cookie",
			XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
		}},
		clients.NewAdvancedMDClient(server.Client(), time.Now),
		nil,
		time.Now,
	)
	got, err := adapter.SearchPatients(context.Background(), domain.PatientSearch{
		LastName:  "Doe",
		FirstName: "Jane",
	})
	if err != nil {
		t.Fatalf("SearchPatients() error = %v", err)
	}
	if len(got.Patients) != 0 {
		t.Fatalf("SearchPatients() = %+v, want no matches", got)
	}
	message := requestBody["ppmdmsg"].(map[string]any)
	if message["@action"] != "lookuppatient" || message["@name"] != "Doe,Jane" {
		t.Fatalf("ppmdmsg = %#v", message)
	}
}

func TestAdapterDemographicsUsesControlledXMLRPCServer(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"patientlist": {
						"patient": {
							"@id": "pat123",
							"@name": "DOE,JANE",
							"@respparty": "resp456",
							"@dob": "01/15/1980",
							"insplanlist": {
								"insplan": {
									"@id": "ins789",
								"@carrier": "car40906",
								"@subscriber": "resp456",
								"@subscribernum": "H123",
									"@enddate": "",
									"@coverage": "1"
								}
							}
						}
					},
					"carrierlist": {
						"carrier": {"@id": "car40906", "@name": "HUMANA MEDICARE"}
					}
				}
			}
		}`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &session.TokenData{
			CookieToken: "token=test-cookie",
			XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
		}},
		clients.NewAdvancedMDClient(server.Client(), time.Now),
		nil,
		time.Now,
	)
	got, err := adapter.GetPatientDemographics(context.Background(), "123")
	if err != nil {
		t.Fatalf("GetPatientDemographics() error = %v", err)
	}
	want := domain.PatientDemographics{
		FullName:            "DOE,JANE",
		CarrierName:         "HUMANA MEDICARE",
		CarrierID:           "car40906",
		InsPlanID:           "ins789",
		RespPartyID:         "resp456",
		SubscriberNum:       "H123",
		DOB:                 "01/15/1980",
		InsuranceStateKnown: true,
	}
	if got != want {
		t.Fatalf("GetPatientDemographics() = %+v, want %+v", got, want)
	}
	message := requestBody["ppmdmsg"].(map[string]any)
	if message["@action"] != "getdemographic" || message["@patientid"] != "123" {
		t.Fatalf("ppmdmsg = %#v", message)
	}
}

func TestAdapterReturnsStableRedactedErrors(t *testing.T) {
	t.Run("session unavailable", func(t *testing.T) {
		adapter := NewAdapter(
			staticSession{err: errors.New("login failed password=secret")},
			clients.NewAdvancedMDClient(http.DefaultClient, time.Now),
			nil,
			time.Now,
		)
		_, err := adapter.SearchPatients(context.Background(), domain.PatientSearch{Phone: "9542872010"})
		if CategoryOf(err) != safeerrors.CategoryUnavailable || err.Error() != "unavailable" {
			t.Fatalf("error = %v category=%q", err, CategoryOf(err))
		}
	})

	t.Run("provider status", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "patientId=123 token=secret", http.StatusServiceUnavailable)
		}))
		defer server.Close()

		adapter := NewAdapter(
			staticSession{token: &session.TokenData{
				CookieToken: "token=test-cookie",
				XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
			}},
			clients.NewAdvancedMDClient(server.Client(), time.Now),
			nil,
			time.Now,
		)
		_, err := adapter.SearchPatients(context.Background(), domain.PatientSearch{Phone: "9542872010"})
		if CategoryOf(err) != safeerrors.CategoryUpstreamStatus || err.Error() != "upstream_status" {
			t.Fatalf("error = %v category=%q", err, CategoryOf(err))
		}
		if strings.Contains(err.Error(), "patientId") || strings.Contains(err.Error(), "token") {
			t.Fatalf("error exposed provider details: %v", err)
		}
	})
}

func TestAdapterClassifiesCreatePatientMutationOutcomes(t *testing.T) {
	command := domain.PatientCreate{
		FirstName: "JANE",
		LastName:  "DOE",
		DOB:       "01/15/1980",
		Phone:     "(954)287-2010",
		Street:    "123 Main St",
		City:      "Spring Hill",
		State:     "FL",
		Zip:       "34609",
		Sex:       "F",
		OfficeID:  "spring_hill",
	}

	t.Run("explicit rejection", func(t *testing.T) {
		requests := 0
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			w.Write([]byte(`{"PPMDResults":{"Error":"Duplicate name/DOB"}}`))
		}))
		defer server.Close()

		adapter := NewAdapter(
			staticSession{token: &session.TokenData{
				CookieToken: "token=test-cookie",
				XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
			}},
			clients.NewAdvancedMDClient(server.Client(), time.Now),
			nil,
			time.Now,
		)
		_, err := adapter.CreatePatient(context.Background(), command)
		if MutationFailureOf(err) != MutationRejected {
			t.Fatalf("CreatePatient() category = %q, want rejected", MutationFailureOf(err))
		}
		if requests != 1 {
			t.Fatalf("requests = %d, want one", requests)
		}
	})

	t.Run("ambiguous response", func(t *testing.T) {
		requests := 0
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			w.Write([]byte(`{"PPMDResults":{"Results":{}}}`))
		}))
		defer server.Close()

		adapter := NewAdapter(
			staticSession{token: &session.TokenData{
				CookieToken: "token=test-cookie",
				XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
			}},
			clients.NewAdvancedMDClient(server.Client(), time.Now),
			nil,
			time.Now,
		)
		_, err := adapter.CreatePatient(context.Background(), command)
		if MutationFailureOf(err) != MutationAmbiguous {
			t.Fatalf("CreatePatient() category = %q, want ambiguous_write", MutationFailureOf(err))
		}
		if requests != 1 {
			t.Fatalf("requests = %d, want one", requests)
		}
	})

	t.Run("explicit HTTP rejection", func(t *testing.T) {
		requests := 0
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			w.WriteHeader(http.StatusUnprocessableEntity)
		}))
		defer server.Close()

		adapter := NewAdapter(
			staticSession{token: &session.TokenData{
				CookieToken: "token=test-cookie",
				XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
			}},
			clients.NewAdvancedMDClient(server.Client(), time.Now),
			nil,
			time.Now,
		)
		_, err := adapter.CreatePatient(context.Background(), command)
		if MutationFailureOf(err) != MutationRejected {
			t.Fatalf("CreatePatient() category = %q, want rejected", MutationFailureOf(err))
		}
		if requests != 1 {
			t.Fatalf("requests = %d, want one", requests)
		}
	})
}

func TestAdapterInsuranceMutationUsesControlledXMLRPCServer(t *testing.T) {
	requests := 0
	var requestBody map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Write([]byte(`{"PPMDResults":{"Results":{}}}`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &session.TokenData{
			CookieToken: "token=test-cookie",
			XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
		}},
		clients.NewAdvancedMDClient(server.Client(), time.Now),
		nil,
		time.Now,
	)
	err := adapter.AddPatientInsurance(context.Background(), domain.PatientInsurance{
		PatientID:     "123",
		RespPartyID:   "resp456",
		CarrierID:     "car308175",
		SubscriberNum: "H123",
	})
	if err != nil {
		t.Fatalf("AddPatientInsurance() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one", requests)
	}
	message := requestBody["ppmdmsg"].(map[string]any)
	if message["@action"] != "addinsurance" {
		t.Fatalf("ppmdmsg = %#v", message)
	}
	patient := message["patient"].(map[string]any)
	if patient["@id"] != "123" {
		t.Fatalf("patient = %#v", patient)
	}
}

func TestAdapterUpcomingAppointmentsUsesControlledRESTServer(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 25, 10, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	var mu sync.Mutex
	requestedColumns := make(map[string]int)
	requestedDays := make(map[string]int)
	var visitRequest map[string]any

	server := patientVisitServer(t, `[
			{"@id": "111", "@visitstartdatetime": "2026-07-20T09:00:00", "@apptstatus": "0"},
			{"@id": "9570263", "@visitstartdatetime": "2026-08-14T12:00:00", "@apptstatus": "0"},
			{"@id": "9570265", "@visitstartdatetime": "2026-08-14T15:00:00", "@apptstatus": "0"},
			{"@id": "9570264", "@visitstartdatetime": "2026-08-15T09:00:00", "@apptstatus": "0"},
			{"@id": "9570266", "@visitstartdatetime": "2026-09-01T09:00:00", "@apptstatus": "10"},
			{"@id": "9570267", "@visitstartdatetime": "2027-06-01T09:00:00", "@apptstatus": "0"}
		]`, &visitRequest, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("forView") != "day" || r.URL.Query().Get("isLegacy") != "true" {
			t.Fatalf("query = %q", r.URL.RawQuery)
		}
		columns := r.URL.Query().Get("columnId")
		day := r.URL.Query().Get("startDate")
		mu.Lock()
		requestedColumns[columns]++
		requestedDays[day]++
		mu.Unlock()

		switch day {
		case "2026-08-14":
			w.Write([]byte(`[
				{"id": 9570263, "startdatetime": "2026-08-14T12:00:00", "columnid": 1513, "provider": "BACH, AUSTIN",
				 "facility": "ABITA EYE GROUP SPRING HILL", "appointmenttypeids": [1007], "patientid": 123},
				{"id": 9570263, "startdatetime": "2026-08-14T12:00:00", "columnid": 1513, "provider": "BACH, AUSTIN",
				 "facility": "ABITA EYE GROUP SPRING HILL", "appointmenttypeids": [1007], "patientid": 123},
				{"id": 222, "startdatetime": "2026-08-14T13:00:00", "columnid": 1513, "patientid": 999}
			]`))
		case "2026-08-15":
			w.Write([]byte(`[{"id": 9570264, "startdatetime": "2026-08-15T09:00:00", "columnid": 1593,
				"provider": "BACH, AUSTIN", "facility": "ABITA EYE GROUP CRYSTAL RIVER",
				"appointmenttypeids": [6169], "patientid": 123}]`))
		case "2027-06-01":
			w.Write([]byte(`[{"id": 9570267, "startdatetime": "2027-06-01T09:00:00", "columnid": 1513,
				"provider": "BACH, AUSTIN", "facility": "ABITA EYE GROUP SPRING HILL",
				"appointmenttypeids": [1007], "patientid": 123}]`))
		default:
			t.Fatalf("unexpected day read %q", day)
		}
	})
	defer server.Close()

	read, err := patientVisitAdapter(server, fixedNow).ReadPatientAppointments(
		context.Background(),
		domain.PatientAppointmentsQuery{
			PatientID: "123",
			OfficeIDs: []string{"spring_hill", "crystal_river", "spring_hill"},
		},
	)
	if err != nil {
		t.Fatalf("ReadPatientAppointments() error = %v", err)
	}
	got := read.Appointments
	if !read.Complete || len(got) != 3 {
		t.Fatalf("ReadPatientAppointments() = %+v, want three complete", read)
	}
	if read.ProviderReads != 4 {
		t.Fatalf("ProviderReads = %d, want one visit read and three day reads", read.ProviderReads)
	}
	if got[0].ID != 9570263 || got[0].OfficeID != "spring_hill" || got[0].Provider != "Dr. Austin Bach" ||
		got[0].Type != "Established Adult Medical (Follow Up)" {
		t.Fatalf("Spring Hill appointment = %+v", got[0])
	}
	if got[1].ID != 9570264 || got[1].OfficeID != "crystal_river" || got[1].Office != "Crystal River" ||
		got[1].Type != "Crystal River Established Patient" {
		t.Fatalf("Crystal River appointment = %+v", got[1])
	}
	if got[2].ID != 9570267 {
		t.Fatalf("appointment beyond six months = %+v", got[2])
	}

	message := visitRequest["ppmdmsg"].(map[string]any)
	if message["@action"] != "getpatientvisits" || message["@patientid"] != "123" {
		t.Fatalf("ppmdmsg = %#v", message)
	}
	mu.Lock()
	defer mu.Unlock()
	wantDays := map[string]int{"2026-08-14": 1, "2026-08-15": 1, "2027-06-01": 1}
	if len(requestedDays) != len(wantDays) {
		t.Fatalf("requested days = %#v, want %#v", requestedDays, wantDays)
	}
	for day, count := range wantDays {
		if requestedDays[day] != count {
			t.Fatalf("requested days = %#v, want %#v", requestedDays, wantDays)
		}
	}
	if len(requestedColumns) != 1 {
		t.Fatalf("requested column groups = %#v, want one batched nearby-office group", requestedColumns)
	}
	for columns := range requestedColumns {
		if !strings.Contains(columns, "1513") || !strings.Contains(columns, "1593") {
			t.Fatalf("requested columns = %q, want both owning offices", columns)
		}
		seen := make(map[string]bool)
		for _, columnID := range strings.Split(columns, "-") {
			if seen[columnID] {
				t.Fatalf("requested columns = %q, want every column exactly once", columns)
			}
			seen[columnID] = true
		}
	}
}

func TestAdapterSkipsDayReadsWithoutUpcomingVisits(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 25, 10, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	for name, visits := range map[string]string{
		"no visits": `null`,
		"past and cancelled": `[
			{"@id": "111", "@visitstartdatetime": "2026-07-20T09:00:00", "@apptstatus": "0"},
			{"@id": "112", "@visitstartdatetime": "2026-08-20T09:00:00", "@apptstatus": "10"}
		]`,
	} {
		t.Run(name, func(t *testing.T) {
			server := patientVisitServer(t, visits, nil, func(_ http.ResponseWriter, r *http.Request) {
				t.Fatalf("unexpected day read %q", r.URL.RawQuery)
			})
			defer server.Close()

			read, err := patientVisitAdapter(server, fixedNow).ReadPatientAppointments(context.Background(),
				domain.PatientAppointmentsQuery{PatientID: "123", OfficeIDs: []string{"spring_hill"}})
			if err != nil || !read.Complete || read.ProviderReads != 1 || len(read.Appointments) != 0 {
				t.Fatalf("ReadPatientAppointments() = %#v, %v; want complete empty read from one visit read", read, err)
			}
		})
	}
}

func TestAdapterReturnsPatientVisitFailureWithoutDayReads(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected day read %q", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	read, err := patientVisitAdapter(server, time.Now()).ReadPatientAppointments(context.Background(),
		domain.PatientAppointmentsQuery{PatientID: "123", OfficeIDs: []string{"spring_hill"}})
	if CategoryOf(err) != safeerrors.CategoryUpstreamStatus || read.ProviderReads != 1 {
		t.Fatalf("ReadPatientAppointments() = %#v, %v; want upstream status after one read", read, err)
	}
}

func patientVisitServer(
	t *testing.T,
	visits string,
	visitRequest *map[string]any,
	dayRead http.HandlerFunc,
) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode visit request: %v", err)
			}
			if visitRequest != nil {
				*visitRequest = request
			}
			var list []any
			if err := json.Unmarshal([]byte(visits), &list); err != nil && visits != "null" {
				t.Fatalf("visit fixture: %v", err)
			}
			visitList := "null"
			if list != nil {
				visitList = `{"visit": ` + visits + `}`
			}
			w.Write([]byte(`{"PPMDResults": {"Results": {"@visitcount": "` + strconv.Itoa(len(list)) +
				`", "visitlist": ` + visitList + `}, "Error": null}}`))
			return
		}
		if r.URL.Path != "/scheduler/appointments" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		dayRead(w, r)
	}))
}

func patientVisitAdapter(server *httptest.Server, now time.Time) *Adapter {
	host := strings.TrimPrefix(server.URL, "https://")
	clock := func() time.Time { return now }
	return NewAdapter(
		staticSession{token: &session.TokenData{
			Token:       "Bearer test-token",
			CookieToken: "token=test-token",
			XmlrpcURL:   host,
			RestApiBase: host,
		}},
		clients.NewAdvancedMDClient(server.Client(), clock),
		clients.NewAdvancedMDRestClient(server.Client()),
		clock,
	)
}

func TestAdapterCanonicalizesSandboxAppointmentTypeIDs(t *testing.T) {
	domain.InitRegistry(domain.Sandbox)
	t.Cleanup(func() { domain.InitRegistry(domain.Prod) })
	office := domain.DefaultOffice()
	fixedNow := time.Date(2026, time.July, 25, 10, 30, 0, 0, time.FixedZone("EDT", -4*60*60))

	server := patientVisitServer(t,
		`[{"@id": "22222", "@visitstartdatetime": "2026-08-14T12:00:00", "@apptstatus": "0"}]`, nil,
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("startDate") != "2026-08-14" ||
				!strings.Contains(r.URL.Query().Get("columnId"), "1716") {
				t.Fatalf("day read = %q", r.URL.RawQuery)
			}
			w.Write([]byte(`[{
				"id": 22222,
				"startdatetime": "2026-08-14T12:00:00",
				"columnid": 1716,
				"patientid": 12345,
				"provider": "BACH, AUSTIN",
				"appointmenttypeids": [18]
			}]`))
		})
	defer server.Close()

	read, err := patientVisitAdapter(server, fixedNow).ReadPatientAppointments(context.Background(), domain.PatientAppointmentsQuery{
		PatientID: "12345",
		OfficeIDs: []string{office.ID},
	})
	if err != nil {
		t.Fatalf("ReadPatientAppointments() error = %v", err)
	}
	got := read.Appointments
	if len(got) != 1 {
		t.Fatalf("appointments = %+v, want one", got)
	}
	if got[0].AppointmentTypeID != 1007 || got[0].Type != "Established Adult Medical (Follow Up)" {
		t.Fatalf("appointment type = %d/%q", got[0].AppointmentTypeID, got[0].Type)
	}
}

func TestPatientAppointmentReadPreservesUnrecognizedTypeForRescheduling(t *testing.T) {
	office := domain.DefaultOffice()
	read := patientAppointmentRead(
		[]clients.AMDAppointmentResponse{{
			ID:               22222,
			StartDateTime:    "2026-08-14T12:00:00",
			ColumnID:         1513,
			PatientID:        12345,
			Provider:         "BACH, AUSTIN",
			AppointmentTypes: []int{9999},
		}},
		12345,
		map[int]*domain.OfficeConfig{1513: office},
		office,
		nil,
	)
	if !read.Complete || len(read.Appointments) != 1 {
		t.Fatalf("appointment read = %#v", read)
	}
	if got := read.Appointments[0]; got.AppointmentTypeID != 9999 || got.Type != "Appointment" {
		t.Fatalf("appointment type = %d/%q", got.AppointmentTypeID, got.Type)
	}
}

func TestAdapterPreservesUnrecognizedProductionAppointmentType(t *testing.T) {
	var bookingPayload map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&bookingPayload); err != nil {
			t.Fatalf("decode booking: %v", err)
		}
		w.Write([]byte(`{"id":98765}`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &session.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
		time.Now,
	)
	_, err := adapter.BookAppointment(context.Background(), Booking{
		PatientID:                 12345,
		OfficeID:                  "spring_hill",
		ColumnID:                  1513,
		ProfileID:                 620,
		Start:                     time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		Duration:                  15,
		ProviderAppointmentTypeID: 9999,
		AppointmentColor:          domain.PreservedAppointmentTypeFallbackColor,
	})
	if err != nil {
		t.Fatalf("BookAppointment error = %v", err)
	}
	types, ok := bookingPayload["type"].([]any)
	if !ok || len(types) != 1 {
		t.Fatalf("booking type = %#v", bookingPayload["type"])
	}
	typePayload, ok := types[0].(map[string]any)
	if !ok || typePayload["id"] != float64(9999) {
		t.Fatalf("booking type = %#v", bookingPayload["type"])
	}
	if bookingPayload["color"] != domain.PreservedAppointmentTypeFallbackColor {
		t.Fatalf("booking color = %#v", bookingPayload["color"])
	}
}

func TestAdapterSingleOfficeMarksUnreconciledRowsIncomplete(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 25, 10, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	server := patientVisitServer(t,
		`[{"@id": "30002", "@visitstartdatetime": "2026-08-14T12:00:00", "@apptstatus": "0"}]`, nil,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`[
				{
					"id": 30001,
					"startdatetime": "not-a-date",
					"patientid": 12345,
					"provider": "BACH, AUSTIN",
					"appointmenttypeids": [1007]
				},
				{
					"id": 30002,
					"startdatetime": "2026-08-14T12:00:00",
					"columnid": 1513,
					"patientid": 12345
				}
			]`))
		})
	defer server.Close()

	read, err := patientVisitAdapter(server, fixedNow).ReadPatientAppointments(context.Background(), domain.PatientAppointmentsQuery{
		PatientID: "12345",
		OfficeIDs: []string{"spring_hill"},
	})
	if err != nil {
		t.Fatalf("ReadPatientAppointments() error = %v", err)
	}
	if read.Complete ||
		read.ProviderReads != 2 ||
		len(read.Appointments) != 1 ||
		read.Appointments[0].ID != 30002 {
		t.Fatalf("ReadPatientAppointments() = %#v, want one partial appointment and incomplete proof", read)
	}
}

func TestAdapterCancelsAndWaitsForRemainingDayReadsWhenOneDayFails(t *testing.T) {
	fixedNow := time.Date(2026, time.July, 25, 10, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	blockedStarted := make(chan struct{}, 2)
	blockedCancelled := make(chan struct{}, 2)
	release := make(chan struct{})
	server := patientVisitServer(t, `[
			{"@id": "1", "@visitstartdatetime": "2026-08-01T09:00:00", "@apptstatus": "0"},
			{"@id": "2", "@visitstartdatetime": "2026-08-02T09:00:00", "@apptstatus": "0"},
			{"@id": "3", "@visitstartdatetime": "2026-08-03T09:00:00", "@apptstatus": "0"}
		]`, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("startDate") == "2026-08-01" {
			for range 2 {
				select {
				case <-blockedStarted:
				case <-release:
					return
				}
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		blockedStarted <- struct{}{}
		select {
		case <-r.Context().Done():
			blockedCancelled <- struct{}{}
		case <-release:
		}
	})
	defer server.Close()
	defer close(release)

	type readOutcome struct {
		err   error
		calls []safeerrors.ProviderCallSummary
	}
	readDone := make(chan readOutcome, 1)
	go func() {
		ctx, diagnostics := safeerrors.WithDiagnostics(context.Background())
		_, err := patientVisitAdapter(server, fixedNow).ReadPatientAppointments(ctx, domain.PatientAppointmentsQuery{
			PatientID: "12345",
			OfficeIDs: []string{"spring_hill"},
		})
		readDone <- readOutcome{err: err, calls: diagnostics.Calls()}
	}()

	guard := time.After(5 * time.Second)
	select {
	case outcome := <-readDone:
		if CategoryOf(outcome.err) != safeerrors.CategoryUpstreamStatus {
			t.Fatalf("ReadPatientAppointments() error = %v, want the failed day's upstream status", outcome.err)
		}
		dayCalls := 0
		for _, call := range outcome.calls {
			if call.Operation == "get_appointments_by_day" {
				dayCalls = call.Calls
			}
		}
		if dayCalls != 3 {
			t.Fatalf("provider calls recorded when ReadPatientAppointments() returned = %+v, want all 3 day reads finished", outcome.calls)
		}
	case <-guard:
		t.Fatal("ReadPatientAppointments() did not return after one day read failed")
	}
	for cancelled := 0; cancelled < 2; cancelled++ {
		select {
		case <-blockedCancelled:
		case <-guard:
			t.Fatalf("%d of 2 remaining day reads were cancelled after one day failed", cancelled)
		}
	}
}

func TestAdapterReadsIntendedAppointmentDay(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("startDate") != "2027-01-03" || r.URL.Query().Get("forView") != "day" {
			t.Fatalf("query = %q, want intended appointment day", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{
				"id": 30004,
				"startdatetime": "2027-01-03T09:00:00",
				"columnid": 1513,
				"patientid": 12345,
			"provider": "BACH, AUSTIN",
			"facility": "ABITA EYE GROUP SPRING HILL",
			"appointmenttypeids": [1007]
		}]`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &session.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
		func() time.Time {
			return time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
		},
	)
	read, err := adapter.ReadPatientAppointmentsForDay(context.Background(), AppointmentDayQuery{
		PatientID: "12345",
		OfficeIDs: []string{"spring_hill"},
		Day:       time.Date(2027, time.January, 3, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadPatientAppointmentsForDay() error = %v", err)
	}
	if !read.Complete ||
		read.ProviderReads != 1 ||
		len(read.Appointments) != 1 ||
		read.Appointments[0].ID != 30004 {
		t.Fatalf("ReadPatientAppointmentsForDay() = %#v, want exact complete match", read)
	}
}

func TestAdapterIntendedDayReadPreservesPerOfficeReconciliation(t *testing.T) {
	var mu sync.Mutex
	requestedColumns := make(map[string]int)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestedColumns[r.URL.Query().Get("columnId")]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &session.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
		time.Now,
	)
	read, err := adapter.ReadPatientAppointmentsForDay(context.Background(), AppointmentDayQuery{
		PatientID: "12345",
		OfficeIDs: []string{"spring_hill", "crystal_river"},
		Day:       time.Date(2027, time.January, 3, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadPatientAppointmentsForDay() error = %v", err)
	}
	if !read.Complete || read.ProviderReads != 2 {
		t.Fatalf("ReadPatientAppointmentsForDay() = %#v, want two complete per-office reads", read)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requestedColumns) != 2 {
		t.Fatalf("requested column groups = %#v, want one per reconciliation office", requestedColumns)
	}
	for columns, count := range requestedColumns {
		if count != 1 {
			t.Fatalf("requests for %q = %d, want one", columns, count)
		}
	}
}

func TestAdapterMarksMissingPatientIDIncomplete(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{
			"id": 30005,
			"startdatetime": "2027-01-03T09:00:00",
			"provider": "BACH, AUSTIN",
			"facility": "ABITA EYE GROUP SPRING HILL",
			"appointmenttypeids": [1007]
		}]`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &session.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
		time.Now,
	)
	read, err := adapter.ReadPatientAppointmentsForDay(context.Background(), AppointmentDayQuery{
		PatientID: "12345",
		OfficeIDs: []string{"spring_hill"},
		Day:       time.Date(2027, time.January, 3, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadPatientAppointmentsForDay() error = %v", err)
	}
	if read.Complete || len(read.Appointments) != 0 {
		t.Fatalf("ReadPatientAppointmentsForDay() = %#v, want incomplete empty read", read)
	}
}

func TestAdapterReadsCurrentAppointmentStateAfterStartTime(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("startDate") != "2026-08-14" || r.URL.Query().Get("forView") != "day" {
			t.Fatalf("query = %q, want appointment day", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{
			"id": 30003,
			"startdatetime": "2026-08-14T12:00:00",
			"patientid": 12345
		}]`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &session.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
		func() time.Time {
			return time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
		},
	)
	state, err := adapter.ReadAppointmentState(context.Background(), AppointmentStateQuery{
		AppointmentID: 30003,
		OfficeID:      "spring_hill",
		Start:         time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadAppointmentState() error = %v", err)
	}
	if !state.Exists || !state.Complete {
		t.Fatalf("ReadAppointmentState() = %#v, want existing complete state", state)
	}
}

func TestAdapterReadsCompleteScheduleThroughDomainSeam(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xmlrpc":
			if r.Header.Get("Cookie") != "token=test-cookie" {
				t.Fatalf("Cookie = %q", r.Header.Get("Cookie"))
			}
			w.Write([]byte(`{
				"PPMDResults": {
					"Results": {
						"columnlist": {"column": [{
							"@id": "col1513",
							"@name": "DR. BACH - SH",
							"@profile": "prof620",
							"@facility": "fac1568",
							"columnsetting": {
								"@start": "0900",
								"@end": "0915",
								"@interval": "15",
								"@maxapptsperslot": "0",
								"@workweek": "1111111"
							}
						}]},
						"profilelist": {"profile": [{"@id": "prof620", "@name": "BACH, AUSTIN"}]},
						"facilitylist": {"facility": [{"@id": "fac1568", "@name": "ABITA EYE GROUP SPRING HILL"}]}
					}
				}
			}`))
		case "/scheduler/appointments":
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			w.Write([]byte(`[{
				"id": 11,
				"startdatetime": "2026-06-03T09:00:00",
				"duration": 15,
				"columnid": 1513,
				"profileid": 620,
				"patientid": 42
			}]`))
		case "/scheduler/blockholds":
			w.Write([]byte(`[{
				"id": 12,
				"startdatetime": "2026-06-03T10:00:00",
				"enddatetime": "2026-06-03T10:15:00",
				"duration": 15,
				"columnid": 1513
			}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &session.TokenData{
			CookieToken: "token=test-cookie",
			Token:       "Bearer test-token",
			XmlrpcURL:   strings.TrimPrefix(server.URL, "https://") + "/xmlrpc",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		clients.NewAdvancedMDClient(server.Client(), time.Now),
		clients.NewAdvancedMDRestClient(server.Client()),
		time.Now,
	)

	setup, err := adapter.GetSchedulerSetup(context.Background())
	if err != nil {
		t.Fatalf("GetSchedulerSetup error = %v", err)
	}
	if len(setup.Columns) != 1 || setup.Columns[0].ID != "1513" {
		t.Fatalf("setup = %#v", setup)
	}

	column, err := adapter.ReadColumnDay(context.Background(), "1513", "2026-06-03")
	if err != nil {
		t.Fatalf("ReadColumnDay error = %v", err)
	}
	if !column.Complete() || len(column.Appointments) != 1 || len(column.BlockHolds) != 1 {
		t.Fatalf("column schedule = %#v", column)
	}
}

func TestAdapterBooksAndCancelsThroughControlledRESTServer(t *testing.T) {
	var bookingPayload map[string]any
	var cancellationPayload map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/scheduler/Appointments":
			if err := json.NewDecoder(r.Body).Decode(&bookingPayload); err != nil {
				t.Fatalf("decode booking: %v", err)
			}
			w.Write([]byte(`{"id":98765}`))
		case r.Method == http.MethodPut && r.URL.Path == "/scheduler/appointments/98765/cancel":
			if err := json.NewDecoder(r.Body).Decode(&cancellationPayload); err != nil {
				t.Fatalf("decode cancellation: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &session.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
		time.Now,
	)
	appointmentID, err := adapter.BookAppointment(context.Background(), Booking{
		PatientID:                 12345,
		OfficeID:                  "spring_hill",
		ColumnID:                  1513,
		ProfileID:                 620,
		Start:                     time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		Duration:                  15,
		ProviderAppointmentTypeID: 1007,
		AppointmentColor:          "ORANGE",
		Force:                     true,
		Comments:                  "Appointment reason: follow up\nReferring doctor: none\n- AI",
	})
	if err != nil || appointmentID != 98765 {
		t.Fatalf("BookAppointment ID = %d, error = %v", appointmentID, err)
	}
	if bookingPayload["patientid"] != float64(12345) ||
		bookingPayload["facilityid"] != float64(1568) ||
		bookingPayload["startdatetime"] != "2026-06-03T09:00" ||
		bookingPayload["force"] != float64(1) {
		t.Fatalf("booking payload = %#v", bookingPayload)
	}

	if err := adapter.CancelAppointment(context.Background(), Cancellation{AppointmentID: appointmentID}); err != nil {
		t.Fatalf("CancelAppointment error = %v", err)
	}
	if cancellationPayload["id"] != float64(98765) {
		t.Fatalf("cancellation payload = %#v", cancellationPayload)
	}
}

func TestAdapterClassifiesProviderMutationOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		category  safeerrors.Category
		ambiguous bool
	}{
		{name: "conflict", status: http.StatusConflict, category: safeerrors.CategoryConflict},
		{name: "authentication", status: http.StatusUnauthorized, category: safeerrors.CategoryAuthentication},
		{name: "forbidden", status: http.StatusForbidden, category: safeerrors.CategoryAuthentication},
		{name: "rejection", status: http.StatusUnprocessableEntity, category: safeerrors.CategoryRejected},
		{name: "bad request", status: http.StatusBadRequest, category: safeerrors.CategoryRejected},
		{name: "timeout", status: http.StatusRequestTimeout, category: safeerrors.CategoryUpstreamStatus, ambiguous: true},
		{name: "ambiguous server failure", status: http.StatusInternalServerError, category: safeerrors.CategoryUpstreamStatus, ambiguous: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			adapter := NewAdapter(
				staticSession{token: &session.TokenData{
					Token:       "Bearer test-token",
					RestApiBase: strings.TrimPrefix(server.URL, "https://"),
				}},
				nil,
				clients.NewAdvancedMDRestClient(server.Client()),
				time.Now,
			)
			_, err := adapter.BookAppointment(context.Background(), Booking{
				PatientID:                 12345,
				OfficeID:                  "spring_hill",
				ColumnID:                  1513,
				ProfileID:                 620,
				Start:                     time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
				Duration:                  15,
				ProviderAppointmentTypeID: 1007,
				AppointmentColor:          "ORANGE",
			})
			if CategoryOf(err) != tt.category || IsAmbiguousWrite(err) != tt.ambiguous {
				t.Fatalf("booking error = %v, category = %q, ambiguous = %t", err, CategoryOf(err), IsAmbiguousWrite(err))
			}

			xmlAdapter := NewAdapter(
				staticSession{token: &session.TokenData{
					CookieToken: "token=test-cookie",
					XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
				}},
				clients.NewAdvancedMDClient(server.Client(), time.Now),
				nil,
				time.Now,
			)
			_, err = xmlAdapter.CreatePatient(context.Background(), domain.PatientCreate{FirstName: "JANE", LastName: "DOE", OfficeID: "spring_hill"})
			if CategoryOf(err) != tt.category || IsAmbiguousWrite(err) != tt.ambiguous {
				t.Fatalf("create error = %v, category = %q, ambiguous = %t", err, CategoryOf(err), IsAmbiguousWrite(err))
			}
		})
	}
}

type staticSession struct {
	token *session.TokenData
	err   error
}

func (s staticSession) Get(context.Context) (*session.TokenData, error) {
	return s.token, s.err
}

func (staticSession) Maintain(context.Context) error {
	return nil
}

func (staticSession) Status() session.SessionStatus {
	return session.SessionStatus{}
}

func TestScheduleReadsAreConcurrentAndCannotUseMalformedOccupancy(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		if r.URL.Path == "/scheduler/appointments" {
			w.Write([]byte(`[{"startdatetime":"invalid"}]`))
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer server.Close()
	adapter := NewAdapter(staticSession{token: &session.TokenData{RestApiBase: strings.TrimPrefix(server.URL, "https://")}}, nil, clients.NewAdvancedMDRestClient(server.Client()), time.Now)
	done := make(chan error, 1)
	go func() {
		column, err := adapter.ReadColumnDay(context.Background(), "1513", "2026-09-15")
		if column.Complete() {
			done <- errors.New("malformed occupancy declared complete")
			return
		}
		if CategoryOf(err) != safeerrors.CategoryInvalidResponse {
			done <- errors.New("missing invalid-response failure")
			return
		}
		done <- nil
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("schedule requests did not start concurrently")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAdapterPatientCandidatesForwardsDOB(t *testing.T) {
	for _, dob := range []string{"01/15/1980", ""} {
		t.Run("dob="+dob, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Msg map[string]any `json:"ppmdmsg"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if request.Msg["@name"] != ",Jane" {
					t.Errorf("name = %v", request.Msg["@name"])
				}
				if dob == "" {
					if _, exists := request.Msg["@dob"]; exists {
						t.Error("empty DOB must be omitted")
					}
				} else if request.Msg["@dob"] != dob {
					t.Errorf("DOB = %v, want %s", request.Msg["@dob"], dob)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"PPMDResults":{"Results":{"patientlist":{"@page":"1","@pagecount":"1","@itemcount":"0"}}}}`))
			}))
			defer server.Close()
			adapter := NewAdapter(staticSession{token: &session.TokenData{CookieToken: "token=test", XmlrpcURL: strings.TrimPrefix(server.URL, "https://")}}, clients.NewAdvancedMDClient(server.Client(), time.Now), nil, time.Now)
			got, err := adapter.ReadPatientCandidates(context.Background(), "Jane", dob)
			if err != nil || !got.Complete || len(got.Patients) != 0 {
				t.Fatalf("read=%+v err=%v", got, err)
			}
		})
	}
}

func TestAdapterPatientAppointmentWindowFollowsTheInjectedEasternClock(t *testing.T) {
	now := time.Date(2026, time.August, 1, 2, 30, 0, 0, time.UTC)
	var mu sync.Mutex
	var requestedDays []string
	server := patientVisitServer(t, `[
			{"@id": "40001", "@visitstartdatetime": "2026-07-31T22:00:00", "@apptstatus": "0"},
			{"@id": "40002", "@visitstartdatetime": "2026-07-31T23:00:00", "@apptstatus": "0"}
		]`, nil, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestedDays = append(requestedDays, r.URL.Query().Get("startDate"))
		mu.Unlock()
		w.Write([]byte(`[
			{"id": 40001, "startdatetime": "2026-07-31T22:00:00", "columnid": 1513, "provider": "BACH, AUSTIN", "appointmenttypeids": [1007], "patientid": 123},
			{"id": 40002, "startdatetime": "2026-07-31T23:00:00", "columnid": 1513, "provider": "BACH, AUSTIN", "appointmenttypeids": [1007], "patientid": 123}
		]`))
	})
	defer server.Close()

	read, err := patientVisitAdapter(server, now).ReadPatientAppointments(context.Background(), domain.PatientAppointmentsQuery{
		PatientID: "123",
		OfficeIDs: []string{"spring_hill"},
	})
	if err != nil {
		t.Fatalf("ReadPatientAppointments() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(requestedDays, []string{"2026-07-31"}) {
		t.Fatalf("requested days = %v, want the Eastern date of the one future visit", requestedDays)
	}
	if len(read.Appointments) != 1 || read.Appointments[0].ID != 40002 {
		t.Fatalf("ReadPatientAppointments() = %#v, want only the 23:00 Eastern appointment after the 22:30 Eastern cutoff", read.Appointments)
	}
}
