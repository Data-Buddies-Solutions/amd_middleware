package advancedmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/session"
)

func TestAdapterSearchPatientsUsesControlledXMLRPCServer(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Cookie") != "token=test-cookie" {
			t.Fatalf("Cookie = %q", r.Header.Get("Cookie"))
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"PPMDResults": {
				"Results": {
					"patientlist": {
						"@itemcount": "1",
						"patient": {
							"@id": "pat123",
							"@name": "DOE,JANE",
							"@dob": "01/15/1980",
							"contactinfo": {"@cellphone": "850-373-3869"}
						}
					}
				}
			}
		}`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &domain.TokenData{
			CookieToken: "token=test-cookie",
			XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
		}},
		clients.NewAdvancedMDClient(server.Client()),
		nil,
	)

	got, err := adapter.SearchPatients(context.Background(), domain.PatientSearch{Phone: "9542872010"})
	if err != nil {
		t.Fatalf("SearchPatients() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != "123" || got[0].Phone != "850-373-3869" {
		t.Fatalf("SearchPatients() = %+v", got)
	}
	message, ok := requestBody["ppmdmsg"].(map[string]any)
	if !ok {
		t.Fatalf("request body = %#v", requestBody)
	}
	if message["@action"] != "lookuppatient" || message["@phone"] != "9542872010" {
		t.Fatalf("ppmdmsg = %#v", message)
	}
}

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
		staticSession{token: &domain.TokenData{
			CookieToken: "token=test-cookie",
			XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
		}},
		clients.NewAdvancedMDClient(server.Client()),
		nil,
	)
	got, err := adapter.SearchPatients(context.Background(), domain.PatientSearch{
		LastName:  "Doe",
		FirstName: "Jane",
	})
	if err != nil {
		t.Fatalf("SearchPatients() error = %v", err)
	}
	if len(got) != 0 {
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
		staticSession{token: &domain.TokenData{
			CookieToken: "token=test-cookie",
			XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
		}},
		clients.NewAdvancedMDClient(server.Client()),
		nil,
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
			clients.NewAdvancedMDClient(http.DefaultClient),
			nil,
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
			staticSession{token: &domain.TokenData{
				CookieToken: "token=test-cookie",
				XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
			}},
			clients.NewAdvancedMDClient(server.Client()),
			nil,
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
	domain.InitRegistry("")
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
			staticSession{token: &domain.TokenData{
				CookieToken: "token=test-cookie",
				XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
			}},
			clients.NewAdvancedMDClient(server.Client()),
			nil,
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
			staticSession{token: &domain.TokenData{
				CookieToken: "token=test-cookie",
				XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
			}},
			clients.NewAdvancedMDClient(server.Client()),
			nil,
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
			staticSession{token: &domain.TokenData{
				CookieToken: "token=test-cookie",
				XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
			}},
			clients.NewAdvancedMDClient(server.Client()),
			nil,
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
	domain.InitRegistry("")
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
		staticSession{token: &domain.TokenData{
			CookieToken: "token=test-cookie",
			XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
		}},
		clients.NewAdvancedMDClient(server.Client()),
		nil,
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
	domain.InitRegistry("")
	fixedNow := time.Date(2026, time.July, 25, 10, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	var requestedColumns map[string]int = make(map[string]int)
	var requestedMonths map[string]int = make(map[string]int)
	var mu sync.Mutex
	augustResponded := make(chan struct{})
	var releaseJuly sync.Once

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/scheduler/appointments" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("forView") != "month" || r.URL.Query().Get("isLegacy") != "true" {
			t.Fatalf("query = %q", r.URL.RawQuery)
		}
		columns := r.URL.Query().Get("columnId")
		mu.Lock()
		requestedColumns[columns]++
		requestedMonths[r.URL.Query().Get("startDate")]++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("startDate") == "2026-07-01" && strings.Contains(columns, "1513") {
			// Force a later month to complete first so ordering cannot depend on
			// goroutine completion order.
			<-augustResponded
			w.Write([]byte(`[
				{
					"id": 111,
					"startdatetime": "2026-07-20T09:00:00",
					"columnid": 1513,
					"patientid": 123
				},
				{
					"id": 222,
					"startdatetime": "2026-07-30T09:00:00",
					"columnid": 1513,
					"patientid": 999
				}
			]`))
			return
		}
		if r.URL.Query().Get("startDate") != "2026-08-01" {
			w.Write([]byte(`[]`))
			return
		}
		switch {
		case strings.Contains(columns, "1513") && strings.Contains(columns, "1593"):
			w.Write([]byte(`[
					{
						"id": 9570264,
						"startdatetime": "2026-08-15T09:00:00",
						"columnid": 1593,
						"provider": "BACH, AUSTIN",
						"facility": "ABITA EYE GROUP CRYSTAL RIVER",
						"appointmenttypeids": [6169],
						"patientid": 123
					},
					{
						"id": 9570263,
						"startdatetime": "2026-08-14T12:00:00",
						"columnid": 1513,
						"provider": "BACH, AUSTIN",
						"facility": "ABITA EYE GROUP SPRING HILL",
						"appointmenttypeids": [1007],
						"patientid": 123
					},
					{
						"id": 9570263,
						"startdatetime": "2026-08-14T12:00:00",
						"columnid": 1513,
						"provider": "BACH, AUSTIN",
						"facility": "ABITA EYE GROUP SPRING HILL",
						"appointmenttypeids": [1007],
						"patientid": 123
					}
				]`))
		default:
			w.Write([]byte(`[]`))
		}
		releaseJuly.Do(func() { close(augustResponded) })
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &domain.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
		func() time.Time { return fixedNow },
	)

	read, err := adapter.ReadPatientAppointments(
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
	if len(got) != 2 {
		t.Fatalf("ReadPatientAppointments() = %+v, want two", got)
	}
	if read.ProviderReads != 6 {
		t.Fatalf("ProviderReads = %d, want six", read.ProviderReads)
	}
	if got[0].OfficeID != "spring_hill" || got[0].Provider != "Dr. Austin Bach" ||
		got[0].Type != "Established Adult Medical (Follow Up)" {
		t.Fatalf("Spring Hill appointment = %+v", got[0])
	}
	if got[1].OfficeID != "crystal_river" || got[1].Office != "Crystal River" ||
		got[1].Type != "Crystal River Established Patient" {
		t.Fatalf("Crystal River appointment = %+v", got[1])
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requestedColumns) != 1 {
		t.Fatalf("requested column groups = %#v, want one batched nearby-office group", requestedColumns)
	}
	for columns, count := range requestedColumns {
		if count != 6 {
			t.Fatalf("requests for %q = %d, want six months", columns, count)
		}
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
	if len(requestedMonths) != 6 {
		t.Fatalf("requested months = %#v, want six-month horizon", requestedMonths)
	}
	for month, count := range requestedMonths {
		if count != 1 {
			t.Fatalf("requests for month %q = %d, want one", month, count)
		}
	}
}

func TestAdapterCanonicalizesDevelopmentAppointmentTypeIDs(t *testing.T) {
	domain.InitRegistry("dev")
	t.Cleanup(func() { domain.InitRegistry("") })
	office := domain.DefaultOffice()
	fixedNow := time.Date(2026, time.July, 25, 10, 30, 0, 0, time.FixedZone("EDT", -4*60*60))

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("startDate") != "2026-08-01" ||
			!strings.Contains(r.URL.Query().Get("columnId"), "1716") {
			w.Write([]byte(`[]`))
			return
		}
		w.Write([]byte(`[{
				"id": 22222,
				"startdatetime": "2026-08-14T12:00:00",
				"columnid": 1716,
				"patientid": 12345,
			"provider": "BACH, AUSTIN",
			"appointmenttypeids": [18]
		}]`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &domain.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
		func() time.Time { return fixedNow },
	)
	read, err := adapter.ReadPatientAppointments(context.Background(), domain.PatientAppointmentsQuery{
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
	domain.InitRegistry("")
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
	domain.InitRegistry("")
	var bookingPayload map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&bookingPayload); err != nil {
			t.Fatalf("decode booking: %v", err)
		}
		w.Write([]byte(`{"id":98765}`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &domain.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
	)
	_, err := adapter.BookAppointment(context.Background(), Booking{
		PatientID:                 12345,
		OfficeID:                  "spring_hill",
		ColumnID:                  1513,
		ProfileID:                 620,
		Start:                     time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		Duration:                  15,
		AppointmentTypeID:         9999,
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

func TestAdapterSingleOfficeUsesSixReadsAndMarksUnreconciledRowsIncomplete(t *testing.T) {
	domain.InitRegistry("")
	fixedNow := time.Date(2026, time.July, 25, 10, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("startDate") != "2026-08-01" {
			w.Write([]byte(`[]`))
			return
		}
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
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &domain.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
		func() time.Time { return fixedNow },
	)
	read, err := adapter.ReadPatientAppointments(context.Background(), domain.PatientAppointmentsQuery{
		PatientID: "12345",
		OfficeIDs: []string{"spring_hill"},
	})
	if err != nil {
		t.Fatalf("ReadPatientAppointments() error = %v", err)
	}
	if read.Complete ||
		read.ProviderReads != 6 ||
		len(read.Appointments) != 1 ||
		read.Appointments[0].ID != 30002 {
		t.Fatalf("ReadPatientAppointments() = %#v, want one partial appointment and incomplete proof", read)
	}
}

func TestAdapterReadsIntendedAppointmentMonth(t *testing.T) {
	domain.InitRegistry("")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("startDate") != "2027-01-01" {
			t.Fatalf("startDate = %q, want intended appointment month", r.URL.Query().Get("startDate"))
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
		staticSession{token: &domain.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
		func() time.Time {
			return time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
		},
	)
	read, err := adapter.ReadPatientAppointmentsForMonth(context.Background(), AppointmentMonthQuery{
		PatientID: "12345",
		OfficeIDs: []string{"spring_hill"},
		Month:     time.Date(2027, time.January, 3, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadPatientAppointmentsForMonth() error = %v", err)
	}
	if !read.Complete ||
		read.ProviderReads != 1 ||
		len(read.Appointments) != 1 ||
		read.Appointments[0].ID != 30004 {
		t.Fatalf("ReadPatientAppointmentsForMonth() = %#v, want exact complete match", read)
	}
}

func TestAdapterIntendedMonthReadPreservesPerOfficeReconciliation(t *testing.T) {
	domain.InitRegistry("")
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
		staticSession{token: &domain.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
	)
	read, err := adapter.ReadPatientAppointmentsForMonth(context.Background(), AppointmentMonthQuery{
		PatientID: "12345",
		OfficeIDs: []string{"spring_hill", "crystal_river"},
		Month:     time.Date(2027, time.January, 3, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadPatientAppointmentsForMonth() error = %v", err)
	}
	if !read.Complete || read.ProviderReads != 2 {
		t.Fatalf("ReadPatientAppointmentsForMonth() = %#v, want two complete per-office reads", read)
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
	domain.InitRegistry("")
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
		staticSession{token: &domain.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
	)
	read, err := adapter.ReadPatientAppointmentsForMonth(context.Background(), AppointmentMonthQuery{
		PatientID: "12345",
		OfficeIDs: []string{"spring_hill"},
		Month:     time.Date(2027, time.January, 3, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ReadPatientAppointmentsForMonth() error = %v", err)
	}
	if read.Complete || len(read.Appointments) != 0 {
		t.Fatalf("ReadPatientAppointmentsForMonth() = %#v, want incomplete empty read", read)
	}
}

func TestAdapterReadsCurrentAppointmentStateAfterStartTime(t *testing.T) {
	domain.InitRegistry("")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("startDate") != "2026-08-01" {
			t.Fatalf("startDate = %q, want appointment month", r.URL.Query().Get("startDate"))
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
		staticSession{token: &domain.TokenData{
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
		staticSession{token: &domain.TokenData{
			CookieToken: "token=test-cookie",
			Token:       "Bearer test-token",
			XmlrpcURL:   strings.TrimPrefix(server.URL, "https://") + "/xmlrpc",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		clients.NewAdvancedMDClient(server.Client()),
		clients.NewAdvancedMDRestClient(server.Client()),
	)

	setup, err := adapter.GetSchedulerSetup(context.Background())
	if err != nil {
		t.Fatalf("GetSchedulerSetup error = %v", err)
	}
	if len(setup.Columns) != 1 || setup.Columns[0].ID != "1513" {
		t.Fatalf("setup = %#v", setup)
	}

	read, err := adapter.ReadSchedule(context.Background(), domain.ScheduleReadQuery{
		ColumnIDs: []string{"1513"},
		Date:      "2026-06-03",
	})
	if err != nil {
		t.Fatalf("ReadSchedule error = %v", err)
	}
	column := read.Columns["1513"]
	if !column.Complete() || len(column.Appointments) != 1 || len(column.BlockHolds) != 1 {
		t.Fatalf("column schedule = %#v", column)
	}
}

func TestAdapterPreservesPartialScheduleReads(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/scheduler/appointments" && r.URL.Query().Get("columnId") == "1513" {
			http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	adapter := NewAdapter(
		staticSession{token: &domain.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
	)
	read, err := adapter.ReadSchedule(context.Background(), domain.ScheduleReadQuery{
		ColumnIDs: []string{"1513", "1598"},
		Date:      "2026-06-03",
	})
	if err != nil {
		t.Fatalf("ReadSchedule error = %v", err)
	}
	if read.Columns["1513"].AppointmentsComplete ||
		!read.Columns["1513"].BlockHoldsComplete ||
		!read.Columns["1598"].Complete() {
		t.Fatalf("read = %#v, want one explicit partial column", read)
	}
}

func TestAdapterBooksAndCancelsThroughControlledRESTServer(t *testing.T) {
	domain.InitRegistry("")
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
		staticSession{token: &domain.TokenData{
			Token:       "Bearer test-token",
			RestApiBase: strings.TrimPrefix(server.URL, "https://"),
		}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
	)
	appointmentID, err := adapter.BookAppointment(context.Background(), Booking{
		PatientID:                 12345,
		OfficeID:                  "spring_hill",
		ColumnID:                  1513,
		ProfileID:                 620,
		Start:                     time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		Duration:                  15,
		AppointmentTypeID:         1007,
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

	if err := adapter.CancelAppointment(context.Background(), Cancellation{
		PatientID:     "12345",
		AppointmentID: appointmentID,
		OfficeID:      "spring_hill",
	}); err != nil {
		t.Fatalf("CancelAppointment error = %v", err)
	}
	if cancellationPayload["id"] != float64(98765) {
		t.Fatalf("cancellation payload = %#v", cancellationPayload)
	}
}

func TestAdapterClassifiesProviderMutationOutcomes(t *testing.T) {
	domain.InitRegistry("")
	tests := []struct {
		name      string
		status    int
		category  safeerrors.Category
		ambiguous bool
	}{
		{name: "conflict", status: http.StatusConflict, category: safeerrors.CategoryConflict},
		{name: "authentication", status: http.StatusUnauthorized, category: safeerrors.CategoryAuthentication},
		{name: "rejection", status: http.StatusUnprocessableEntity, category: safeerrors.CategoryRejected},
		{name: "ambiguous server failure", status: http.StatusInternalServerError, category: safeerrors.CategoryUpstreamStatus, ambiguous: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			adapter := NewAdapter(
				staticSession{token: &domain.TokenData{
					Token:       "Bearer test-token",
					RestApiBase: strings.TrimPrefix(server.URL, "https://"),
				}},
				nil,
				clients.NewAdvancedMDRestClient(server.Client()),
			)
			_, err := adapter.BookAppointment(context.Background(), Booking{
				PatientID:                 12345,
				OfficeID:                  "spring_hill",
				ColumnID:                  1513,
				ProfileID:                 620,
				Start:                     time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
				Duration:                  15,
				AppointmentTypeID:         1007,
				ProviderAppointmentTypeID: 1007,
				AppointmentColor:          "ORANGE",
			})
			if CategoryOf(err) != tt.category || IsAmbiguousWrite(err) != tt.ambiguous {
				t.Fatalf("error = %v, category = %q, ambiguous = %t", err, CategoryOf(err), IsAmbiguousWrite(err))
			}
		})
	}
}

type staticSession struct {
	token *domain.TokenData
	err   error
}

func (s staticSession) Get(context.Context) (*domain.TokenData, error) {
	return s.token, s.err
}

func (staticSession) Maintain(context.Context) error {
	return nil
}

func (staticSession) Status() session.SessionStatus {
	return session.SessionStatus{}
}
