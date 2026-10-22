package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	patientmodule "advancedmd-token-management/internal/patient"
	schedulingmodule "advancedmd-token-management/internal/scheduling"
	"advancedmd-token-management/internal/session"
)

var updateIdentityGolden = flag.Bool("update", false, "rewrite testdata/identity.golden.tsv")

const (
	pinSecret        = "pin-api-secret"
	pinBookingSecret = "pin-booking-secret"
)

type pinChart struct {
	id          string
	name        string
	dob         string
	lookupPhone string
	cellPhone   string
	plan        *pinPlan
}

type pinPlan struct {
	id, carrierID, carrierName, subscriberNum string
}

var humanaPlan = &pinPlan{id: "ins101", carrierID: "car40906", carrierName: "HUMANA MEDICARE", subscriberNum: "H101"}

var pinCharts = []pinChart{
	{id: "101", name: "SMITH,MARY ANN", dob: "01/15/1980", lookupPhone: "9545550100", cellPhone: "954-555-0100", plan: humanaPlan},
	{id: "102", name: "SMITH,JOHN", dob: "03/20/1982", lookupPhone: "9545550100", cellPhone: "954-555-0100"},
	{id: "103", name: "SMITH, ANNA MARIE", dob: "07/04/2015", lookupPhone: "9545550100", cellPhone: "(954) 555-0100"},
	{id: "104", name: "GARCÍA,JOSÉ", dob: "01/15/1980", lookupPhone: "9545550104", cellPhone: "954-555-0104"},
	{id: "105", name: "LEE,MARYANN", dob: "01/15/1980", lookupPhone: "9545550105"},
	{id: "106", name: "JONES,MARY", dob: "01/15/1980", lookupPhone: "9545550106", cellPhone: "954.555.0106"},
	{id: "107", name: "PRINCE", dob: "02/02/1990", lookupPhone: "9545550107", cellPhone: "9545550107"},
	{id: "12345", name: "DOE,JANE", dob: "01/15/1980", lookupPhone: "9545550145", cellPhone: "954-555-0145"},
}

type pinAMD struct {
	mu    sync.Mutex
	calls []string
}

func (a *pinAMD) record(format string, args ...any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, fmt.Sprintf(format, args...))
}

func (a *pinAMD) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Message map[string]any `json:"ppmdmsg"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	message := request.Message
	w.Header().Set("Content-Type", "application/json")
	switch message["@action"] {
	case "lookuppatient":
		a.record("amd lookuppatient%s", presentAttributes(message, "@name", "@phone", "@dob"))
		writeResults(w, map[string]any{"patientlist": lookupResult(lookupCharts(message))})
	case "getdemographic":
		requested, _ := message["@patientid"].(string)
		a.record("amd getdemographic patientid=%q", requested)
		chart, ok := chartByID(strings.TrimPrefix(requested, "pat"))
		if !ok {
			w.Write([]byte(`{"PPMDResults":{"Error":{"Fault":{"detail":{"description":"Patient not found"}}}}}`))
			return
		}
		writeResults(w, demographicResult(chart))
	case "addpatient":
		patient := message["patientlist"].(map[string]any)["patient"].(map[string]any)
		contact := patient["contactinfo"].(map[string]any)
		a.record("amd addpatient name=%q dob=%q homephone=%q", patient["@name"], patient["@dob"], contact["@homephone"])
		writeResults(w, map[string]any{"patientlist": map[string]any{"patient": map[string]any{
			"@id": "pat900", "@name": patient["@name"], "@respparty": "resp900",
		}}})
	case "addinsurance":
		patient := message["patient"].(map[string]any)
		plan := patient["insplanlist"].(map[string]any)["insplan"].(map[string]any)
		if _, ending := plan["@enddate"]; ending {
			a.record("amd enddateinsurance patient=%q insplan=%q", patient["@id"], plan["@id"])
		} else {
			a.record("amd addinsurance patient=%q carrier=%q subscribernum=%q", patient["@id"], plan["@carrier"], plan["@subscribernum"])
		}
		writeResults(w, map[string]any{"patient": map[string]any{"@id": patient["@id"]}})
	default:
		http.Error(w, "unsupported action", http.StatusBadRequest)
	}
}

func presentAttributes(message map[string]any, keys ...string) string {
	var b strings.Builder
	for _, key := range keys {
		if value, ok := message[key]; ok {
			fmt.Fprintf(&b, " %s=%q", strings.TrimPrefix(key, "@"), value)
		}
	}
	return b.String()
}

func writeResults(w http.ResponseWriter, results any) {
	json.NewEncoder(w).Encode(map[string]any{"PPMDResults": map[string]any{"Results": results}})
}

func lookupCharts(message map[string]any) []pinChart {
	phone, _ := message["@phone"].(string)
	dob, _ := message["@dob"].(string)
	name, _ := message["@name"].(string)
	last, first, _ := strings.Cut(strings.ToUpper(name), ",")
	var found []pinChart
	for _, chart := range pinCharts {
		chartLast, chartGiven, _ := strings.Cut(domain.StripDiacritics(chart.name), ",")
		switch {
		case phone != "":
			if chart.lookupPhone == phone {
				found = append(found, chart)
			}
		case dob != "":
			if chart.dob == dob {
				found = append(found, chart)
			}
		case strings.HasPrefix(chartLast, last) && strings.HasPrefix(strings.TrimSpace(chartGiven), first):
			found = append(found, chart)
		}
	}
	return found
}

func lookupResult(charts []pinChart) map[string]any {
	patients := make([]map[string]any, 0, len(charts))
	for _, chart := range charts {
		patients = append(patients, map[string]any{
			"@id": "pat" + chart.id, "@name": chart.name, "@dob": chart.dob, "@respparty": "resp" + chart.id,
			"contactinfo": map[string]any{"@cellphone": chart.cellPhone},
		})
	}
	return map[string]any{
		"@itemcount": fmt.Sprint(len(charts)), "@page": "1", "@pagecount": "1", "patient": patients,
	}
}

func chartByID(id string) (pinChart, bool) {
	for _, chart := range pinCharts {
		if chart.id == id {
			return chart, true
		}
	}
	return pinChart{}, false
}

func demographicResult(chart pinChart) map[string]any {
	patient := map[string]any{"@id": "pat" + chart.id, "@name": chart.name, "@respparty": "resp" + chart.id, "@dob": chart.dob}
	result := map[string]any{"patientlist": map[string]any{"patient": patient}}
	if chart.plan != nil {
		patient["insplanlist"] = map[string]any{"insplan": map[string]any{
			"@id": chart.plan.id, "@carrier": chart.plan.carrierID, "@subscriber": "resp" + chart.id,
			"@subscribernum": chart.plan.subscriberNum, "@enddate": "", "@coverage": "1",
		}}
		result["carrierlist"] = map[string]any{"carrier": map[string]any{"@id": chart.plan.carrierID, "@name": chart.plan.carrierName}}
	}
	return result
}

type pinSession struct {
	token *session.TokenData
}

func (s pinSession) Get(context.Context) (*session.TokenData, error) {
	return s.token, nil
}

func (pinSession) Maintain(context.Context) error {
	return nil
}

func (pinSession) Status() session.SessionStatus {
	return session.SessionStatus{}
}

type pinRecords struct {
	amd   *advancedmd.Adapter
	calls *pinAMD
}

func (r pinRecords) SearchPatients(ctx context.Context, search domain.PatientSearch) (domain.PatientCandidateRead, error) {
	return r.amd.SearchPatients(ctx, search)
}

func (r pinRecords) ReadPatientCandidates(ctx context.Context, firstName, dob string) (domain.PatientCandidateRead, error) {
	return r.amd.ReadPatientCandidates(ctx, firstName, dob)
}

func (r pinRecords) GetPatientDemographics(ctx context.Context, patientID string) (domain.PatientDemographics, error) {
	return r.amd.GetPatientDemographics(ctx, patientID)
}

func (r pinRecords) CreatePatient(ctx context.Context, command domain.PatientCreate) (domain.CreatedPatient, error) {
	return r.amd.CreatePatient(ctx, command)
}

func (r pinRecords) AddPatientInsurance(ctx context.Context, command domain.PatientInsurance) error {
	return r.amd.AddPatientInsurance(ctx, command)
}

func (r pinRecords) EndDatePatientInsurance(ctx context.Context, command domain.PatientInsuranceEnd) error {
	return r.amd.EndDatePatientInsurance(ctx, command)
}

func (r pinRecords) ReadPatientAppointments(_ context.Context, query domain.PatientAppointmentsQuery) (advancedmd.AppointmentRead, error) {
	r.calls.record("appointments patient=%q", query.PatientID)
	return pinAppointmentRead(query.PatientID), nil
}

func (r pinRecords) ReadPatientAppointmentsForMonth(_ context.Context, query advancedmd.AppointmentMonthQuery) (advancedmd.AppointmentRead, error) {
	r.calls.record("appointment-month patient=%q", query.PatientID)
	return pinAppointmentRead(query.PatientID), nil
}

func (pinRecords) ReadAppointmentState(context.Context, advancedmd.AppointmentStateQuery) (advancedmd.AppointmentState, error) {
	return advancedmd.AppointmentState{Complete: true}, nil
}

func (pinRecords) GetSchedulerSetup(context.Context) (domain.SchedulerSetup, error) {
	return domain.SchedulerSetup{Columns: []domain.SchedulerColumn{{
		ID: "1513", Name: "BACH, AUSTIN", ProfileID: "620", FacilityID: "1568",
		StartTime: "09:00", EndTime: "09:15", Interval: 15, Workweek: 127,
	}}}, nil
}

func (pinRecords) ReadColumnDay(context.Context, string, string) (domain.ColumnSchedule, error) {
	return domain.ColumnSchedule{AppointmentsComplete: true, BlockHoldsComplete: true}, nil
}

func (pinRecords) ReadScheduleRange(_ context.Context, query domain.ScheduleRangeQuery) (map[string]domain.ScheduleReadResult, error) {
	results := map[string]domain.ScheduleReadResult{}
	for day := query.Start; !day.After(query.End); day = day.AddDate(0, 0, 1) {
		results[day.Format("2006-01-02")] = openSchedule(query.ColumnIDs)
	}
	return results, nil
}

func (r pinRecords) BookAppointment(_ context.Context, booking advancedmd.Booking) (int, error) {
	r.calls.record("book patient=%d", booking.PatientID)
	return 98765, nil
}

func (r pinRecords) CancelAppointment(_ context.Context, cancellation advancedmd.Cancellation) error {
	r.calls.record("cancel appointment=%d", cancellation.AppointmentID)
	return nil
}

func openSchedule(columnIDs []string) domain.ScheduleReadResult {
	columns := map[string]domain.ColumnSchedule{}
	for _, id := range columnIDs {
		columns[id] = domain.ColumnSchedule{AppointmentsComplete: true, BlockHoldsComplete: true}
	}
	return domain.ScheduleReadResult{Columns: columns}
}

func pinAppointmentRead(patientID string) advancedmd.AppointmentRead {
	appointments := map[string][]domain.PatientAppointment{
		"101": {{
			ID: 33101, Start: pinStart, Provider: "Dr. Austin Bach", Type: "Established Adult Medical (Follow Up)",
			AppointmentTypeID: 1007, Facility: "Abita Eye Group Spring Hill", OfficeID: "spring_hill", Office: "Spring Hill",
		}},
		"12345": {pinOriginal},
	}
	return advancedmd.AppointmentRead{Appointments: appointments[patientID], Complete: true, ProviderReads: 1}
}

type pinHarness struct {
	router http.Handler
	calls  *pinAMD
}

var pinStart = time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)

var pinOriginal = domain.PatientAppointment{
	ID: 54321, Start: time.Date(2026, 6, 4, 9, 0, 0, 0, time.UTC), Provider: "Dr. Austin Bach",
	AppointmentTypeID: 1007, OfficeID: "spring_hill", Office: "Spring Hill",
}

func newPinHarness(t *testing.T) pinHarness {
	t.Helper()
	calls := &pinAMD{}
	server := httptest.NewTLSServer(calls)
	t.Cleanup(server.Close)
	amdSession := pinSession{token: &session.TokenData{CookieToken: "token=pin", XmlrpcURL: strings.TrimPrefix(server.URL, "https://")}}
	records := pinRecords{
		amd:   advancedmd.NewAdapter(amdSession, clients.NewAdvancedMDClient(server.Client(), testNow), nil, testNow),
		calls: calls,
	}
	tokens := schedulingmodule.NewAppointmentTokens(pinBookingSecret, testNow)
	handlers := NewHandlers(amdSession, patientmodule.New(records, tokens, testNow), schedulingmodule.New(records, pinBookingSecret, testNow), testNow)
	return pinHarness{router: NewRouter(handlers, pinSecret, nil), calls: calls}
}

func (h pinHarness) post(t *testing.T, path string, body map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(encoded)))
	request.Header.Set("Authorization", "Bearer "+pinSecret)
	response := httptest.NewRecorder()
	h.router.ServeHTTP(response, request)
	var decoded any
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("%s %s: decode %q: %v", path, encoded, response.Body.String(), err)
	}
	projected, err := json.Marshal(pinProjection(decoded))
	if err != nil {
		t.Fatal(err)
	}
	h.calls.mu.Lock()
	calls := slices.Clone(h.calls.calls)
	h.calls.calls = nil
	h.calls.mu.Unlock()
	slices.Sort(calls)
	return fmt.Sprintf("%d\t%s\t%s", response.Code, projected, strings.Join(calls, "; "))
}

func pinProjection(value any) any {
	switch value := value.(type) {
	case map[string]any:
		projected := map[string]any{}
		for key, field := range value {
			switch {
			case key == "insuranceDecision":
				continue
			case key == "slots":
				slots, _ := field.([]any)
				projected["slotCount"] = len(slots)
				if len(slots) > 0 {
					projected["firstSlot"] = pinProjection(slots[0])
				}
			case strings.HasSuffix(key, "Token"):
				projected[key] = tokenClaims(field)
			default:
				projected[key] = pinProjection(field)
			}
		}
		return projected
	case []any:
		projected := make([]any, len(value))
		for i, item := range value {
			projected[i] = pinProjection(item)
		}
		return projected
	default:
		return value
	}
}

func tokenClaims(value any) any {
	token, _ := value.(string)
	body, _, found := strings.Cut(token, ".")
	if !found {
		return value
	}
	decoded, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return value
	}
	return "claims:" + string(decoded)
}

func signedPinSlot(t *testing.T, dob domain.DOB) string {
	t.Helper()
	token, err := schedulingmodule.SignSlotToken(pinBookingSecret, schedulingmodule.SlotPolicy{
		OfficeID: "spring_hill", Routing: domain.RoutingBachOnly, ColumnID: 1513, ProfileID: 620,
		StartDatetime: pinStart.Format("2006-01-02T15:04"), Duration: 15, DOB: dob,
		AppointmentTypeIDs: []int{1007}, SameStartCapacity: 2,
		IssuedAt: testNow().Unix(), ExpiresAt: testNow().Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func pinAppointmentToken(t *testing.T, purpose string, appointment domain.PatientAppointment) string {
	t.Helper()
	tokens := schedulingmodule.NewAppointmentTokens(pinBookingSecret, testNow)
	issue := tokens.IssueCancellationToken
	if purpose == "reschedule" {
		issue = tokens.IssueRescheduleToken
	}
	token, err := issue("12345", appointment)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

type pinCase struct {
	name   string
	fields map[string]any
}

func with(base map[string]any, overrides map[string]any) map[string]any {
	merged := map[string]any{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overrides {
		if value == nil {
			delete(merged, key)
			continue
		}
		merged[key] = value
	}
	return merged
}

func overridesLabel(fields map[string]any) string {
	shown := map[string]any{}
	for key, value := range fields {
		if strings.HasSuffix(key, "Token") {
			shown[key] = "<signed>"
			continue
		}
		shown[key] = value
	}
	encoded, _ := json.Marshal(shown)
	return string(encoded)
}

func TestIdentityHandlingAcrossRoutesMatchesGolden(t *testing.T) {
	var rows []string
	run := func(route, path string, base map[string]any, cases []pinCase) {
		for _, tc := range cases {
			harness := newPinHarness(t)
			got := harness.post(t, path, with(base, tc.fields))
			rows = append(rows, strings.Join([]string{route, tc.name, overridesLabel(tc.fields), got}, "\t"))
		}
	}

	resolve := map[string]any{"office": "Spring Hill"}
	run("resolve", "/api/patient/resolve", resolve, []pinCase{
		{"patientId numeric", map[string]any{"patientId": "101"}},
		{"patientId pat prefix", map[string]any{"patientId": "pat101"}},
		{"patientId padded", map[string]any{"patientId": " 101 "}},
		{"patientId trailing space", map[string]any{"patientId": "101 "}},
		{"patientId padded pat prefix", map[string]any{"patientId": " pat101 "}},
		{"patientId uppercase PAT", map[string]any{"patientId": "PAT101"}},
		{"patientId signed", map[string]any{"patientId": "+101"}},
		{"patientId leading zero", map[string]any{"patientId": "0101"}},
		{"patientId letters", map[string]any{"patientId": "abc"}},
		{"patientId bare pat", map[string]any{"patientId": "pat"}},
		{"patientId unknown chart", map[string]any{"patientId": "999"}},
		{"patientId with dob", map[string]any{"patientId": "101", "dob": "01/15/1980"}},
		{"phone digits household", map[string]any{"phone": "9545550100"}},
		{"phone punctuated household", map[string]any{"phone": "(954) 555-0100"}},
		{"phone e164 household", map[string]any{"phone": "+19545550100"}},
		{"phone country code dashed", map[string]any{"phone": "1-954-555-0100"}},
		{"phone dotted padded", map[string]any{"phone": " 954.555.0100 "}},
		{"phone single chart", map[string]any{"phone": "+1 (954) 555-0104"}},
		{"phone single chart without record phone", map[string]any{"phone": "+1 954 555 0105"}},
		{"phone record without comma", map[string]any{"phone": "954-555-0107"}},
		{"phone seven digits", map[string]any{"phone": "555-0100"}},
		{"phone no digits", map[string]any{"phone": "+"}},
		{"phone letters", map[string]any{"phone": "abc"}},
		{"phone + firstName Mary", map[string]any{"phone": "9545550100", "firstName": "Mary"}},
		{"phone + firstName MARY ANN", map[string]any{"phone": "9545550100", "firstName": "MARY ANN"}},
		{"phone + firstName Maryann", map[string]any{"phone": "9545550100", "firstName": "Maryann"}},
		{"phone + firstName Mary-Ann", map[string]any{"phone": "9545550100", "firstName": "Mary-Ann"}},
		{"phone + firstName prefix Mar", map[string]any{"phone": "9545550100", "firstName": "Mar"}},
		{"phone + firstName padded", map[string]any{"phone": "9545550100", "firstName": " Mary"}},
		{"phone + firstName middle name only", map[string]any{"phone": "9545550100", "firstName": "Ann"}},
		{"phone + firstName Anna", map[string]any{"phone": "9545550100", "firstName": "anna"}},
		{"phone + firstName diacritics", map[string]any{"phone": "9545550104", "firstName": "José"}},
		{"phone + firstName letterless", map[string]any{"phone": "9545550100", "firstName": "123"}},
		{"phone + dob slashes", map[string]any{"phone": "9545550100", "dob": "01/15/1980"}},
		{"phone + dob iso", map[string]any{"phone": "9545550100", "dob": "1980-01-15"}},
		{"phone + dob unpadded", map[string]any{"phone": "9545550100", "dob": "1/15/1980"}},
		{"phone + dob words", map[string]any{"phone": "9545550100", "dob": "January 15, 1980"}},
		{"phone + dob impossible", map[string]any{"phone": "9545550100", "dob": "13/45/1980"}},
		{"phone + dob future", map[string]any{"phone": "9545550100", "dob": "2099-01-01"}},
		{"phone + dob garbage", map[string]any{"phone": "9545550100", "dob": "garbage"}},
		{"phone + dob padded", map[string]any{"phone": "9545550100", "dob": " 01/15/1980"}},
		{"phone + firstName + dob", map[string]any{"phone": "9545550100", "firstName": "John", "dob": "03/20/1982"}},
		{"firstName + dob Mary", map[string]any{"firstName": "Mary", "dob": "01/15/1980"}},
		{"firstName + dob Mary Ann", map[string]any{"firstName": "Mary Ann", "dob": "01/15/1980"}},
		{"firstName + dob Maryann", map[string]any{"firstName": "Maryann", "dob": "01/15/1980"}},
		{"firstName + dob Jane", map[string]any{"firstName": "Jane", "dob": "01/15/1980"}},
		{"firstName + dob jane iso", map[string]any{"firstName": "jane", "dob": "1980-01-15"}},
		{"firstName + dob Jose diacritics", map[string]any{"firstName": "José", "dob": "01/15/1980"}},
		{"firstName + dob Jose plain", map[string]any{"firstName": "Jose", "dob": "01/15/1980"}},
		{"firstName + dob padded name", map[string]any{"firstName": " Jane ", "dob": "01/15/1980"}},
		{"firstName + dob letterless", map[string]any{"firstName": "123", "dob": "01/15/1980"}},
		{"firstName + dob no match", map[string]any{"firstName": "Zed", "dob": "01/15/1980"}},
		{"firstName + dob invalid", map[string]any{"firstName": "Jane", "dob": "02/30/1980"}},
		{"firstName + dob future", map[string]any{"firstName": "Jane", "dob": "2099-01-01"}},
		{"firstName + dob padded dob", map[string]any{"firstName": "Jane", "dob": "01/15/1980 "}},
		{"lastName + dob", map[string]any{"lastName": "Smith", "dob": "03/20/1982"}},
		{"lastName + dob diacritics", map[string]any{"lastName": "García", "dob": "01/15/1980"}},
		{"lastName + firstName + dob", map[string]any{"lastName": "Smith", "firstName": "Mary", "dob": "01/15/1980"}},
		{"lastName + firstName Mary Ann + dob", map[string]any{"lastName": "Smith", "firstName": "Mary Ann", "dob": "01/15/1980"}},
		{"lastName without dob", map[string]any{"lastName": "Smith"}},
		{"dob only", map[string]any{"dob": "01/15/1980"}},
	})

	create := map[string]any{
		"firstName": "Jane", "lastName": "Roe", "dob": "01/15/1980", "phone": "9545550199",
		"street": "1 Main St", "city": "Spring Hill", "state": "FL", "zip": "34606", "sex": "female",
		"insurance": "Meritain Health", "subscriberName": "Jane Roe", "subscriberNum": "M123", "office": "Spring Hill",
	}
	run("add-patient", "/api/add-patient", create, []pinCase{
		{"baseline", nil},
		{"phone punctuated", map[string]any{"phone": "(954) 555-0199"}},
		{"phone e164", map[string]any{"phone": "+19545550199"}},
		{"phone seven digits", map[string]any{"phone": "555-0199"}},
		{"phone international", map[string]any{"phone": "+44 20 7946 0958"}},
		{"phone blank", map[string]any{"phone": " "}},
		{"phone letters", map[string]any{"phone": "abc"}},
		{"phone missing", map[string]any{"phone": ""}},
		{"dob iso", map[string]any{"dob": "1980-01-15"}},
		{"dob unpadded", map[string]any{"dob": "1/15/1980"}},
		{"dob words", map[string]any{"dob": "January 15, 1980"}},
		{"dob impossible", map[string]any{"dob": "13/45/1980"}},
		{"dob future", map[string]any{"dob": "2099-01-01"}},
		{"dob garbage", map[string]any{"dob": "garbage"}},
		{"dob padded", map[string]any{"dob": " 01/15/1980"}},
		{"dob invalid and street missing", map[string]any{"dob": "garbage", "street": ""}},
		{"names diacritics", map[string]any{"firstName": "José", "lastName": "Muñoz"}},
		{"names padded", map[string]any{"firstName": " Jane ", "lastName": " Roe "}},
		{"names uppercase", map[string]any{"firstName": "JANE", "lastName": "ROE"}},
		{"existing chart first name", map[string]any{"firstName": "Mary", "lastName": "Smith", "phone": "9545550100"}},
		{"existing chart lowercase", map[string]any{"firstName": "mary", "lastName": "Smith", "phone": "(954) 555-0100"}},
		{"existing chart full given name", map[string]any{"firstName": "Mary Ann", "lastName": "Smith", "phone": "9545550100"}},
		{"existing chart joined given name", map[string]any{"firstName": "Maryann", "lastName": "Smith", "phone": "9545550100"}},
		{"existing chart other dob", map[string]any{"firstName": "Mary", "lastName": "Smith", "phone": "9545550100", "dob": "01/16/1980"}},
		{"existing chart iso dob", map[string]any{"firstName": "Mary", "lastName": "Smith", "phone": "9545550100", "dob": "1980-01-15"}},
		{"existing chart without comma", map[string]any{"firstName": "Prince", "lastName": "Prince", "phone": "9545550107", "dob": "02/02/1990"}},
		{"household member", map[string]any{"firstName": "Peter", "lastName": "Smith", "phone": "9545550100"}},
	})

	update := map[string]any{"patientId": "101", "dob": "01/15/1980", "insurance": "Meritain Health", "subscriberNum": "M101", "office": "Spring Hill"}
	run("update-insurance", "/api/patient/update-insurance", update, []pinCase{
		{"baseline", nil},
		{"patientId pat prefix", map[string]any{"patientId": "pat101"}},
		{"patientId padded", map[string]any{"patientId": " 101 "}},
		{"patientId padded pat prefix", map[string]any{"patientId": " pat101 "}},
		{"patientId signed", map[string]any{"patientId": "+101"}},
		{"patientId letters", map[string]any{"patientId": "abc"}},
		{"patientId bare pat", map[string]any{"patientId": "pat"}},
		{"patientId missing", map[string]any{"patientId": ""}},
		{"dob iso", map[string]any{"dob": "1980-01-15"}},
		{"dob words", map[string]any{"dob": "Jan 15, 1980"}},
		{"dob other day", map[string]any{"dob": "01/16/1980"}},
		{"dob garbage", map[string]any{"dob": "garbage"}},
		{"dob future", map[string]any{"dob": "2099-01-01"}},
		{"dob missing", map[string]any{"dob": ""}},
		{"patientId letters and dob garbage", map[string]any{"patientId": "abc", "dob": "garbage"}},
	})

	book := map[string]any{
		"patientId": "12345", "patientName": "DOE,JANE", "dob": "01/15/1980",
		"bookingToken": signedPinSlot(t, "01/15/1980"), "appointmentTypeId": 1007, "office": "Spring Hill",
	}
	undatedSlot := signedPinSlot(t, "")
	run("book", "/api/appointment/book", book, []pinCase{
		{"baseline", nil},
		{"patientId pat prefix", map[string]any{"patientId": "pat12345"}},
		{"patientId padded pat prefix", map[string]any{"patientId": " pat12345 "}},
		{"patientId signed", map[string]any{"patientId": "+12345"}},
		{"patientId letters", map[string]any{"patientId": "abc"}},
		{"patientId bare pat", map[string]any{"patientId": "pat"}},
		{"patientId missing", map[string]any{"patientId": ""}},
		{"patientId letters and token missing", map[string]any{"patientId": "abc", "bookingToken": ""}},
		{"dob iso", map[string]any{"dob": "1980-01-15"}},
		{"dob omitted", map[string]any{"dob": nil}},
		{"dob other day", map[string]any{"dob": "01/16/1980"}},
		{"dob garbage", map[string]any{"dob": "garbage"}},
		{"undated slot dob slashes", map[string]any{"bookingToken": undatedSlot}},
		{"undated slot dob iso", map[string]any{"bookingToken": undatedSlot, "dob": "1980-01-15"}},
		{"undated slot dob garbage", map[string]any{"bookingToken": undatedSlot, "dob": "garbage"}},
		{"undated slot dob future", map[string]any{"bookingToken": undatedSlot, "dob": "2099-01-01"}},
		{"undated slot dob other day", map[string]any{"bookingToken": undatedSlot, "dob": "01/16/1980"}},
		{"undated slot patientId letters and dob garbage", map[string]any{"bookingToken": undatedSlot, "patientId": "abc", "dob": "garbage"}},
		{"patientName comma space", map[string]any{"patientName": "Doe, Jane"}},
		{"patientName lowercase plain", map[string]any{"patientName": "jane doe"}},
		{"patientName mixed case", map[string]any{"patientName": "Jane McDoe"}},
		{"patientName middle name", map[string]any{"patientName": "SMITH,MARY ANN"}},
		{"patientName empty given", map[string]any{"patientName": "DOE,"}},
		{"patientName empty last", map[string]any{"patientName": ",JANE"}},
		{"patientName padded", map[string]any{"patientName": "  DOE , JANE  "}},
		{"patientName omitted", map[string]any{"patientName": nil}},
	})

	reschedule := with(book, map[string]any{
		"visitCategory":   "medical",
		"rescheduleToken": pinAppointmentToken(t, "reschedule", pinOriginal),
	})
	run("reschedule", "/api/appointment/reschedule", reschedule, []pinCase{
		{"baseline", nil},
		{"patientId pat prefix", map[string]any{"patientId": "pat12345"}},
		{"patientId padded pat prefix", map[string]any{"patientId": " pat12345 "}},
		{"patientId double prefix", map[string]any{"patientId": "patpat12345"}},
		{"patientId letters", map[string]any{"patientId": "abc"}},
		{"patientId other patient", map[string]any{"patientId": "99999"}},
		{"patientId missing", map[string]any{"patientId": ""}},
		{"dob other day", map[string]any{"dob": "01/16/1980"}},
	})

	cancellable := domain.PatientAppointment{ID: 33333, Start: pinStart, OfficeID: "spring_hill", Office: "Spring Hill"}
	cancel := map[string]any{"cancellationToken": pinAppointmentToken(t, "cancel", cancellable), "appointmentId": 33333, "office": "Spring Hill"}
	run("cancel", "/api/appointment/cancel", cancel, []pinCase{
		{"token only", nil},
		{"patientId numeric", map[string]any{"patientId": "12345"}},
		{"patientId pat prefix", map[string]any{"patientId": "pat12345"}},
		{"patientId padded", map[string]any{"patientId": " 12345 "}},
		{"patientId letters", map[string]any{"patientId": "abc"}},
		{"patientId other patient", map[string]any{"patientId": "99999"}},
		{"patientId bare pat", map[string]any{"patientId": "pat"}},
	})

	slots := map[string]any{"office": "Spring Hill", "startDate": "2026-06-03", "routing": "bach_only", "visitType": "medical"}
	run("slots", "/api/scheduler/slots", slots, []pinCase{
		{"no identity", nil},
		{"patientId numeric", map[string]any{"patientId": "12345"}},
		{"patientId pat prefix", map[string]any{"patientId": "pat12345"}},
		{"patientId letters", map[string]any{"patientId": "abc"}},
		{"dob slashes", map[string]any{"dob": "01/15/1980"}},
		{"dob iso", map[string]any{"dob": "1980-01-15"}},
		{"dob words", map[string]any{"dob": "January 15 1980"}},
		{"dob child", map[string]any{"dob": "07/04/2015"}},
		{"dob impossible", map[string]any{"dob": "13/45/1980"}},
		{"dob future", map[string]any{"dob": "2099-01-01"}},
		{"dob garbage", map[string]any{"dob": "garbage"}},
		{"dob padded", map[string]any{"dob": "01/15/1980 "}},
	})

	decision := map[string]any{"plan": "VSP", "coverageType": "routine_vision", "office": "Hollywood"}
	run("insurance-decision", "/api/insurance/decision", decision, []pinCase{
		{"no dob", nil},
		{"dob adult", map[string]any{"dob": "01/15/1980"}},
		{"dob adult iso", map[string]any{"dob": "1980-01-15"}},
		{"dob six years old", map[string]any{"dob": "05/01/2020"}},
		{"dob six years old words", map[string]any{"dob": "May 1, 2020"}},
		{"dob three years old", map[string]any{"dob": "05/01/2023"}},
		{"dob three years old iso", map[string]any{"dob": "2023-05-01"}},
		{"dob garbage", map[string]any{"dob": "garbage"}},
		{"dob future", map[string]any{"dob": "2099-01-01"}},
	})

	checkIdentityGolden(t, strings.Join(rows, "\n")+"\n")
}

func checkIdentityGolden(t *testing.T, got string) {
	t.Helper()
	path := filepath.Join("testdata", "identity.golden.tsv")
	if *updateIdentityGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/http -run IdentityHandling -update)", err)
	}
	if got == string(want) {
		return
	}
	wantRows, gotRows := strings.Split(string(want), "\n"), strings.Split(got, "\n")
	for i := range max(len(wantRows), len(gotRows)) {
		var w, g string
		if i < len(wantRows) {
			w = wantRows[i]
		}
		if i < len(gotRows) {
			g = gotRows[i]
		}
		if w != g {
			t.Errorf("row %d\nwant %s\n got %s", i+1, w, g)
		}
	}
	t.Fatal("identity handling changed; if intended, run go test ./internal/http -run IdentityHandling -update and review the diff of testdata/identity.golden.tsv")
}
