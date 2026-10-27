package advancedmd

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/session"
)

func TestAdapterMapsEveryProviderCategory(t *testing.T) {
	type mutation struct {
		category  safeerrors.Category
		ambiguous bool
		failure   MutationFailure
	}
	ambiguous := func(category safeerrors.Category) mutation {
		return mutation{category: category, ambiguous: true, failure: MutationAmbiguous}
	}
	refused := func(category safeerrors.Category) mutation {
		return mutation{category: category, failure: MutationRejected}
	}
	rows := map[safeerrors.Category]struct {
		read safeerrors.Category
		sent mutation
	}{
		safeerrors.CategoryNone:            {read: safeerrors.CategoryInternal, sent: ambiguous(safeerrors.CategoryInternal)},
		"canceled":                         {read: "canceled", sent: ambiguous("canceled")},
		safeerrors.CategoryTimeout:         {read: safeerrors.CategoryTimeout, sent: ambiguous(safeerrors.CategoryTimeout)},
		safeerrors.CategoryNetwork:         {read: safeerrors.CategoryNetwork, sent: ambiguous(safeerrors.CategoryNetwork)},
		safeerrors.CategoryConflict:        {read: safeerrors.CategoryConflict, sent: refused(safeerrors.CategoryConflict)},
		safeerrors.CategoryRejected:        {read: safeerrors.CategoryRejected, sent: refused(safeerrors.CategoryRejected)},
		safeerrors.CategoryAuthentication:  {read: safeerrors.CategoryAuthentication, sent: refused(safeerrors.CategoryAuthentication)},
		safeerrors.CategoryUnavailable:     {read: safeerrors.CategoryUnavailable, sent: ambiguous(safeerrors.CategoryUnavailable)},
		safeerrors.CategoryUpstreamStatus:  {read: safeerrors.CategoryUpstreamStatus, sent: ambiguous(safeerrors.CategoryUpstreamStatus)},
		safeerrors.CategoryInvalidResponse: {read: safeerrors.CategoryInvalidResponse, sent: ambiguous(safeerrors.CategoryInvalidResponse)},
		safeerrors.CategoryInternal:        {read: safeerrors.CategoryInternal, sent: ambiguous(safeerrors.CategoryInternal)},
		safeerrors.CategoryUpstreamError:   {read: safeerrors.CategoryUpstreamError, sent: ambiguous(safeerrors.CategoryUpstreamError)},
	}
	declared := declaredCategories(t)
	for _, category := range declared {
		if _, ok := rows[category]; !ok {
			t.Errorf("category %q has no row in the adapter mapping table", category)
		}
	}
	mutationOf := func(err error) mutation {
		return mutation{category: CategoryOf(err), ambiguous: IsAmbiguousWrite(err), failure: MutationFailureOf(err)}
	}
	adapter := &Adapter{session: staticSession{}}
	for category, row := range rows {
		t.Run(string(category), func(t *testing.T) {
			if !slices.Contains(declared, category) {
				t.Fatalf("row %q is not a declared safeerrors category", category)
			}
			providerErr := safeerrors.New(category, "provider failure")
			if category == "canceled" {
				providerErr = fmt.Errorf("request failed: %w", context.Canceled)
			}
			if got := CategoryOf(adapter.classify(nil, providerErr)); got != row.read {
				t.Errorf("read category = %q, want %q", got, row.read)
			}
			if got := mutationOf(adapter.classifyMutation(nil, providerErr)); got != row.sent {
				t.Errorf("sent write = %+v, want %+v", got, row.sent)
			}
			unsent := safeerrors.NotSent("failed to create request: %w", providerErr)
			if got := mutationOf(adapter.classifyMutation(nil, unsent)); got != (mutation{category: safeerrors.CategoryInternal}) {
				t.Errorf("unsent write = %+v, want a definite internal failure", got)
			}
		})
	}
}

func declaredCategories(t *testing.T) []safeerrors.Category {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "safeerrors", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var categories []safeerrors.Category
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			constants, ok := decl.(*ast.GenDecl)
			if !ok || constants.Tok != token.CONST {
				continue
			}
			for _, spec := range constants.Specs {
				value := spec.(*ast.ValueSpec)
				if typeName, ok := value.Type.(*ast.Ident); !ok || typeName.Name != "Category" {
					continue
				}
				for _, expr := range value.Values {
					literal, ok := expr.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						t.Fatalf("%s: Category constant %v is not a string literal", path, value.Names)
					}
					text, _ := strconv.Unquote(literal.Value)
					categories = append(categories, safeerrors.Category(text))
				}
			}
		}
	}
	if len(categories) == 0 {
		t.Fatal("found no safeerrors.Category constants")
	}
	return categories
}

func TestAdapterReportsWritesThatNeverLeftAsDefiniteFailures(t *testing.T) {
	unbuildable := &session.TokenData{
		Token:       "Bearer test-token",
		CookieToken: "token=test-cookie",
		XmlrpcURL:   "provider host/xmlrpc/processrequest.aspx",
		RestApiBase: "provider host/api",
	}
	adapter := NewAdapter(
		staticSession{token: unbuildable},
		clients.NewAdvancedMDClient(http.DefaultClient, time.Now),
		clients.NewAdvancedMDRestClient(http.DefaultClient),
		time.Now,
	)
	ctx := context.Background()
	writes := map[string]func() error{
		"create patient": func() error {
			_, err := adapter.CreatePatient(ctx, domain.PatientCreate{FirstName: "JANE", LastName: "DOE", OfficeID: "spring_hill"})
			return err
		},
		"add insurance": func() error {
			return adapter.AddPatientInsurance(ctx, domain.PatientInsurance{PatientID: "123", RespPartyID: "resp456", CarrierID: "car308175", SubscriberNum: "H123"})
		},
		"end-date insurance": func() error {
			return adapter.EndDatePatientInsurance(ctx, domain.PatientInsuranceEnd{PatientID: "123", InsPlanID: "ins789"})
		},
		"book appointment": func() error {
			_, err := adapter.BookAppointment(ctx, Booking{
				PatientID: 12345, OfficeID: "spring_hill", ColumnID: 1513, ProfileID: 620,
				Start: time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC), Duration: 15,
				ProviderAppointmentTypeID: 1007, AppointmentColor: "ORANGE",
			})
			return err
		},
		"cancel appointment": func() error {
			return adapter.CancelAppointment(ctx, Cancellation{AppointmentID: 98765})
		},
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			err := write()
			if CategoryOf(err) != safeerrors.CategoryInternal || IsAmbiguousWrite(err) || MutationFailureOf(err) != "" {
				t.Fatalf("category = %q, ambiguous = %t, mutation failure = %q; a write that was never sent must be a definite internal failure",
					CategoryOf(err), IsAmbiguousWrite(err), MutationFailureOf(err))
			}
		})
	}

	reads := map[string]func() error{
		"patient candidates": func() error {
			_, err := adapter.ReadPatientCandidates(ctx, "JANE", "01/15/1980")
			return err
		},
		"demographics": func() error {
			_, err := adapter.GetPatientDemographics(ctx, "123")
			return err
		},
		"schedule range": func() error {
			_, err := adapter.ReadScheduleRange(ctx, domain.ScheduleRangeQuery{
				ColumnIDs: []string{"1513"},
				Start:     time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC),
				End:       time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC),
			})
			return err
		},
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			if got := CategoryOf(read()); got != safeerrors.CategoryInternal {
				t.Fatalf("category = %q, want internal for a read that was never sent", got)
			}
		})
	}
}

func TestAdapterReportsInconsistentProviderReadsAsInvalidResponses(t *testing.T) {
	lookup := func(pages ...string) func(*testing.T) error {
		return func(t *testing.T) error {
			served := 0
			adapter := xmlrpcAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte(pages[min(served, len(pages)-1)]))
				served++
			})
			_, err := adapter.ReadPatientCandidates(context.Background(), "JANE", "01/15/1980")
			return err
		}
	}
	patient := func(id string) string {
		return `{"@id":"` + id + `","@name":"DOE,JANE","@dob":"01/15/1980"}`
	}
	page := func(page, pages, total string, patients string) string {
		return `{"PPMDResults":{"Results":{"patientlist":{"@page":"` + page + `","@pagecount":"` + pages + `","@itemcount":"` + total + `","patient":` + patients + `}}}}`
	}
	tests := map[string]func(*testing.T) error{
		"repeated patient ID":      lookup(page("1", "1", "2", "["+patient("pat1")+","+patient("pat1")+"]")),
		"missing patient ID":       lookup(page("1", "1", "1", patient(""))),
		"contradictory empty":      lookup(page("1", "1", "0", patient("pat1"))),
		"unreadable pagination":    lookup(page("1", "many", "1", patient("pat1"))),
		"page past reported count": lookup(page("1", "2", "2", patient("pat1")), page("2", "1", "2", patient("pat2"))),
		"changed during pages":     lookup(page("1", "2", "2", patient("pat1")), page("2", "2", "3", patient("pat2"))),
		"fewer rows than reported": lookup(page("1", "1", "2", patient("pat1"))),
		"legacy fewer rows":        lookup(`{"PPMDResults":{"Results":{"patientlist":{"@itemcount":"2","patient":` + patient("pat1") + `}}}}`),
		"mismatched demographics patient": func(t *testing.T) error {
			adapter := xmlrpcAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte(`{"PPMDResults":{"Results":{"patientlist":{"patient":{"@id":"pat999","@name":"DOE,JANE","@dob":"01/15/1980"}}}}}`))
			})
			_, err := adapter.GetPatientDemographics(context.Background(), "123")
			return err
		},
		"invalid scheduler column collection": func(t *testing.T) error {
			adapter := xmlrpcAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte(`{"PPMDResults":{"Results":{"columnlist":{"column":"not a column"}}}}`))
			})
			_, err := adapter.GetSchedulerSetup(context.Background())
			return err
		},
	}
	for name, read := range tests {
		t.Run(name, func(t *testing.T) {
			if got := CategoryOf(read(t)); got != safeerrors.CategoryInvalidResponse {
				t.Fatalf("category = %q, want invalid_response", got)
			}
		})
	}
}

func TestAdapterReportsProviderRejectedReadsAsRejected(t *testing.T) {
	adapter := xmlrpcAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"PPMDResults":{"Error":{"Fault":{"faultcode":"42","description":"patient-secret"}}}}`))
	})
	_, candidatesErr := adapter.ReadPatientCandidates(context.Background(), "JANE", "01/15/1980")
	_, demographicsErr := adapter.GetPatientDemographics(context.Background(), "123")
	_, setupErr := adapter.GetSchedulerSetup(context.Background())
	for name, err := range map[string]error{"candidates": candidatesErr, "demographics": demographicsErr, "scheduler setup": setupErr} {
		if got := CategoryOf(err); got != safeerrors.CategoryRejected {
			t.Errorf("%s category = %q, want rejected", name, got)
		}
	}
}

func TestAdapterTreatsUnusableBookingAnswerAsAmbiguousInvalidResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"id":0}`))
	}))
	defer server.Close()
	adapter := NewAdapter(
		staticSession{token: &session.TokenData{Token: "Bearer test-token", RestApiBase: strings.TrimPrefix(server.URL, "https://")}},
		nil,
		clients.NewAdvancedMDRestClient(server.Client()),
		time.Now,
	)
	_, err := adapter.BookAppointment(context.Background(), Booking{
		PatientID: 12345, OfficeID: "spring_hill", ColumnID: 1513, ProfileID: 620,
		Start: time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC), Duration: 15,
		ProviderAppointmentTypeID: 1007, AppointmentColor: "ORANGE",
	})
	if CategoryOf(err) != safeerrors.CategoryInvalidResponse || MutationFailureOf(err) != MutationAmbiguous {
		t.Fatalf("category = %q, mutation failure = %q, want an ambiguous invalid_response", CategoryOf(err), MutationFailureOf(err))
	}
}

func TestAdapterClassifiesReadStatusesWithoutWriteSemantics(t *testing.T) {
	for status, want := range map[int]safeerrors.Category{
		http.StatusUnauthorized:        safeerrors.CategoryAuthentication,
		http.StatusForbidden:           safeerrors.CategoryAuthentication,
		http.StatusConflict:            safeerrors.CategoryUpstreamStatus,
		http.StatusUnprocessableEntity: safeerrors.CategoryUpstreamStatus,
		http.StatusRequestTimeout:      safeerrors.CategoryUpstreamStatus,
		http.StatusBadGateway:          safeerrors.CategoryUpstreamStatus,
	} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()
			base := strings.TrimPrefix(server.URL, "https://")
			adapter := NewAdapter(
				staticSession{token: &session.TokenData{Token: "Bearer test-token", CookieToken: "token=test-cookie", XmlrpcURL: base, RestApiBase: base}},
				clients.NewAdvancedMDClient(server.Client(), time.Now),
				clients.NewAdvancedMDRestClient(server.Client()),
				time.Now,
			)
			_, xmlrpcErr := adapter.GetPatientDemographics(context.Background(), "123")
			_, restErr := adapter.ReadScheduleRange(context.Background(), domain.ScheduleRangeQuery{
				ColumnIDs: []string{"1513"},
				Start:     time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC),
				End:       time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC),
			})
			if CategoryOf(xmlrpcErr) != want || CategoryOf(restErr) != want {
				t.Fatalf("xmlrpc category = %q, rest category = %q, want %q", CategoryOf(xmlrpcErr), CategoryOf(restErr), want)
			}
		})
	}
}

func xmlrpcAdapter(t *testing.T, handler http.HandlerFunc) *Adapter {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return NewAdapter(
		staticSession{token: &session.TokenData{
			CookieToken: "token=test-cookie",
			XmlrpcURL:   strings.TrimPrefix(server.URL, "https://"),
		}},
		clients.NewAdvancedMDClient(server.Client(), time.Now),
		nil,
		time.Now,
	)
}
