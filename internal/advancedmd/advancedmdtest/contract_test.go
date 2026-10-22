package advancedmdtest_test

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

func TestMain(m *testing.M) {
	domain.InitRegistry(domain.Prod)
	os.Exit(m.Run())
}

type records interface {
	advancedmd.PatientRecords
	advancedmd.SchedulingRecords
}

type outcome struct {
	Value    any
	Category safeerrors.Category
	Mutation advancedmd.MutationFailure
	Writes   int
}

type scenario struct {
	name string
	op   string
	amd  func(*advancedmdtest.Provider)
	fake func(*advancedmdtest.Adapter)
	call func(context.Context, records) (any, error)
	want outcome
}

var contractNow = time.Date(2026, time.July, 25, 10, 30, 0, 0, time.FixedZone("EDT", -4*60*60))

const springHillColumns = "1513-1550-1551-1598-1600"

var (
	writeRoutes = []string{"book", "cancel", "addpatient", "addinsurance"}
	writeOps    = []string{"BookAppointment", "CancelAppointment", "CreatePatient", "AddPatientInsurance", "EndDatePatientInsurance"}
)

func TestFakeAndRealAdapterAgree(t *testing.T) {
	scenarios := slices.Concat(
		patientSearchScenarios(),
		candidateScenarios(),
		demographicScenarios(),
		patientAppointmentScenarios(),
		monthAppointmentScenarios(),
		appointmentStateScenarios(),
		schedulerSetupScenarios(),
		scheduleDayScenarios(),
		scheduleRangeScenarios(),
		bookingScenarios(),
		cancellationScenarios(),
		patientCreateScenarios(),
		insuranceScenarios(),
	)
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			t.Run("real", func(t *testing.T) {
				provider := &advancedmdtest.Provider{Responses: map[string][]advancedmdtest.Response{}}
				if s.amd != nil {
					s.amd(provider)
				}
				value, err := s.call(context.Background(), provider.Adapter(func() time.Time { return contractNow }))
				got := observe(value, err)
				for _, route := range provider.Requests() {
					if slices.Contains(writeRoutes, route) {
						got.Writes++
					}
				}
				for route, unused := range provider.Responses {
					if len(unused) > 0 {
						t.Errorf("scripted AdvancedMD response for %q was never requested; requests were %q", route, provider.Requests())
					}
				}
				assertOutcome(t, "real adapter", got, s.want)
			})
			t.Run("fake", func(t *testing.T) {
				fake := advancedmdtest.NewAdapter()
				if s.fake != nil {
					s.fake(fake)
				}
				value, err := s.call(context.Background(), fake)
				got := observe(value, err)
				calls := fakeCalls(fake)
				for _, op := range writeOps {
					got.Writes += calls[op]
				}
				assertOutcome(t, "fake adapter", got, s.want)
				want := map[string]int{s.op: 1}
				if slices.Contains(writeOps, s.op) {
					want = map[string]int{}
					if s.want.Writes > 0 {
						want[s.op] = s.want.Writes
					}
				}
				if !maps.Equal(calls, want) {
					t.Errorf("fake adapter recorded calls %v, want %v", calls, want)
				}
			})
		})
	}
}

func observe(value any, err error) outcome {
	if err == nil {
		return outcome{Value: value}
	}
	return outcome{
		Value:    value,
		Category: advancedmd.CategoryOf(err),
		Mutation: advancedmd.MutationFailureOf(err),
	}
}

func assertOutcome(t *testing.T, side string, got, want outcome) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s breaks the contract (nil and empty slices differ)\n got: %s\nwant: %s", side, describe(got), describe(want))
	}
}

func describe(o outcome) string {
	return fmt.Sprintf("value=%+v category=%q mutation=%q writes=%d", o.Value, o.Category, o.Mutation, o.Writes)
}

func fakeCalls(fake *advancedmdtest.Adapter) map[string]int {
	calls := map[string]int{
		"SearchPatients":                  fake.SearchPatientCalls,
		"ReadPatientCandidates":           len(fake.CandidateQueries),
		"GetPatientDemographics":          fake.DemographicCalls,
		"ReadPatientAppointments":         fake.AppointmentReadCalls,
		"ReadPatientAppointmentsForMonth": len(fake.AppointmentMonthQueries),
		"ReadAppointmentState":            len(fake.AppointmentStateQueries),
		"GetSchedulerSetup":               fake.SchedulerSetupCalls,
		"ReadSchedule":                    len(fake.ScheduleReadQueries),
		"ReadScheduleRange":               len(fake.ScheduleRangeQueries),
		"BookAppointment":                 len(fake.Bookings),
		"CancelAppointment":               len(fake.Cancellations),
		"CreatePatient":                   fake.CreatePatientCalls,
		"AddPatientInsurance":             fake.AddInsuranceCalls,
		"EndDatePatientInsurance":         fake.EndInsuranceCalls,
	}
	maps.DeleteFunc(calls, func(_ string, count int) bool { return count == 0 })
	return calls
}

func reply(provider *advancedmdtest.Provider, route string, responses ...advancedmdtest.Response) {
	provider.Responses[route] = append(provider.Responses[route], responses...)
}

func ok(body string) advancedmdtest.Response {
	return advancedmdtest.Response{Status: http.StatusOK, Body: body}
}

func status(code int) advancedmdtest.Response {
	return advancedmdtest.Response{Status: code, Body: `{}`}
}

func results(body string) advancedmdtest.Response {
	return ok(`{"PPMDResults":{"Results":` + body + `}}`)
}

func rejected(message string) advancedmdtest.Response {
	return ok(`{"PPMDResults":{"Error":"` + message + `"}}`)
}

func patientList(attributes string, patients ...string) advancedmdtest.Response {
	list := `{` + attributes
	switch len(patients) {
	case 0:
	case 1:
		list += `,"patient":` + patients[0]
	default:
		list += `,"patient":[` + strings.Join(patients, ",") + `]`
	}
	return results(`{"patientlist":` + list + `}}`)
}

const (
	janeRow = `{"@id":"pat123","@name":"DOE,JANE","@dob":"01/15/1980","contactinfo":{"@homephone":"(954)287-2010"}}`
	johnRow = `{"@id":"pat456","@name":"DOE,JOHN","@dob":"03/20/1982","contactinfo":{"@cellphone":"(954)287-2011"}}`
)

var (
	jane = domain.Patient{ID: "123", FirstName: "JANE", FullName: "DOE,JANE", DOB: "01/15/1980", Phone: "(954)287-2010"}
	john = domain.Patient{ID: "456", FirstName: "JOHN", FullName: "DOE,JOHN", DOB: "03/20/1982", Phone: "(954)287-2011"}
)

func searchPatients(search domain.PatientSearch) func(context.Context, records) (any, error) {
	return func(ctx context.Context, r records) (any, error) {
		return r.SearchPatients(ctx, search)
	}
}

func patientSearchScenarios() []scenario {
	byPhone := domain.PatientSearch{Phone: "9542872010"}
	byName := domain.PatientSearch{LastName: "Doe", FirstName: "Jane"}
	return []scenario{
		{
			name: "phone search on one page is complete",
			op:   "SearchPatients",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "lookuppatient", patientList(`"@itemcount":"1","@page":"1","@pagecount":"1"`, janeRow))
			},
			fake: func(f *advancedmdtest.Adapter) { f.PatientSearches[byPhone] = []domain.Patient{jane} },
			call: searchPatients(byPhone),
			want: outcome{Value: domain.PatientCandidateRead{Patients: []domain.Patient{jane}, Complete: true}},
		},
		{
			name: "phone search from a legacy unpaginated lookup is incomplete",
			op:   "SearchPatients",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "lookuppatient", patientList(`"@itemcount":"1"`, janeRow))
			},
			fake: func(f *advancedmdtest.Adapter) {
				f.PatientSearches[byPhone] = []domain.Patient{jane}
				f.IncompletePatientSearch = map[domain.PatientSearch]bool{byPhone: true}
			},
			call: searchPatients(byPhone),
			want: outcome{Value: domain.PatientCandidateRead{Patients: []domain.Patient{jane}, Complete: false}},
		},
		{
			name: "phone search that finds nobody is complete and empty",
			op:   "SearchPatients",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "lookuppatient", patientList(`"@itemcount":"0","@page":"1","@pagecount":"0"`))
			},
			call: searchPatients(byPhone),
			want: outcome{Value: domain.PatientCandidateRead{Patients: []domain.Patient{}, Complete: true}},
		},
		{
			name: "name search across two pages is complete",
			op:   "SearchPatients",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "lookuppatient",
					patientList(`"@itemcount":"2","@page":"1","@pagecount":"2"`, janeRow),
					patientList(`"@itemcount":"2","@page":"2","@pagecount":"2"`, johnRow),
				)
			},
			fake: func(f *advancedmdtest.Adapter) { f.PatientSearches[byName] = []domain.Patient{jane, john} },
			call: searchPatients(byName),
			want: outcome{Value: domain.PatientCandidateRead{Patients: []domain.Patient{jane, john}, Complete: true}},
		},
		{
			name: "name search from a legacy empty lookup is incomplete",
			op:   "SearchPatients",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "lookuppatient", patientList(`"@itemcount":"0"`))
			},
			fake: func(f *advancedmdtest.Adapter) {
				f.IncompletePatientSearch = map[domain.PatientSearch]bool{byName: true}
			},
			call: searchPatients(byName),
			want: outcome{Value: domain.PatientCandidateRead{Patients: []domain.Patient{}, Complete: false}},
		},
		{
			name: "phone search rejected by AdvancedMD",
			op:   "SearchPatients",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "lookuppatient", rejected("Invalid phone")) },
			fake: func(f *advancedmdtest.Adapter) {
				f.PatientErrors[byPhone] = advancedmd.NewError(safeerrors.CategoryRejected)
			},
			call: searchPatients(byPhone),
			want: outcome{Value: domain.PatientCandidateRead{}, Category: safeerrors.CategoryRejected, Mutation: advancedmd.MutationRejected},
		},
		{
			name: "phone search that fails returns no patients",
			op:   "SearchPatients",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "lookuppatient", status(http.StatusServiceUnavailable)) },
			fake: func(f *advancedmdtest.Adapter) {
				f.PatientSearchSequence[byPhone] = []advancedmdtest.PatientSearchStep{{
					Patients: []domain.Patient{jane},
					Err:      advancedmd.NewError(safeerrors.CategoryUpstreamStatus),
				}}
			},
			call: searchPatients(byPhone),
			want: outcome{Value: domain.PatientCandidateRead{}, Category: safeerrors.CategoryUpstreamStatus},
		},
	}
}

func readCandidates(ctx context.Context, r records) (any, error) {
	return r.ReadPatientCandidates(ctx, "Jane", "01/15/1980")
}

func candidateScenarios() []scenario {
	candidate := domain.Patient{ID: "123", FirstName: "JANE", LastName: "DOE", FullName: "DOE,JANE", DOB: "01/15/1980", Phone: "(954)287-2010"}
	return []scenario{
		{
			name: "candidates by first name and DOB on one page are complete",
			op:   "ReadPatientCandidates",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "lookuppatient", patientList(`"@itemcount":"1","@page":"1","@pagecount":"1"`, janeRow))
			},
			fake: func(f *advancedmdtest.Adapter) {
				f.CandidateReads["Jane"] = domain.PatientCandidateRead{Patients: []domain.Patient{candidate}, Complete: true}
			},
			call: readCandidates,
			want: outcome{Value: domain.PatientCandidateRead{Patients: []domain.Patient{candidate}, Complete: true}},
		},
		{
			name: "candidates from a legacy unpaginated lookup are incomplete",
			op:   "ReadPatientCandidates",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "lookuppatient", patientList(`"@itemcount":"1"`, janeRow))
			},
			fake: func(f *advancedmdtest.Adapter) {
				f.CandidateReads["Jane"] = domain.PatientCandidateRead{Patients: []domain.Patient{candidate}, Complete: false}
			},
			call: readCandidates,
			want: outcome{Value: domain.PatientCandidateRead{Patients: []domain.Patient{candidate}, Complete: false}},
		},
		{
			name: "candidates that find nobody are complete and empty",
			op:   "ReadPatientCandidates",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "lookuppatient", patientList(`"@itemcount":"0","@page":"1","@pagecount":"0"`))
			},
			call: readCandidates,
			want: outcome{Value: domain.PatientCandidateRead{Patients: []domain.Patient{}, Complete: true}},
		},
		{
			name: "candidates that fail return no patients",
			op:   "ReadPatientCandidates",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "lookuppatient", status(http.StatusBadGateway)) },
			fake: func(f *advancedmdtest.Adapter) {
				f.CandidateReads["Jane"] = domain.PatientCandidateRead{Patients: []domain.Patient{candidate}, Complete: true}
				f.CandidateReadErrors["Jane"] = advancedmd.NewError(safeerrors.CategoryUpstreamStatus)
			},
			call: readCandidates,
			want: outcome{Value: domain.PatientCandidateRead{}, Category: safeerrors.CategoryUpstreamStatus},
		},
	}
}

func readDemographics(ctx context.Context, r records) (any, error) {
	return r.GetPatientDemographics(ctx, "123")
}

func demographicScenarios() []scenario {
	found := domain.PatientDemographics{
		FullName: "DOE,JANE", CarrierName: "HUMANA MEDICARE", CarrierID: "car40906", InsPlanID: "ins789",
		RespPartyID: "resp456", SubscriberNum: "H123", DOB: "01/15/1980", InsuranceStateKnown: true,
	}
	return []scenario{
		{
			name: "demographics found",
			op:   "GetPatientDemographics",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "getdemographic", results(`{
					"patientlist":{"patient":{"@id":"pat123","@name":"DOE,JANE","@respparty":"resp456","@dob":"01/15/1980",
						"insplanlist":{"insplan":{"@id":"ins789","@carrier":"car40906","@subscriber":"resp456","@subscribernum":"H123","@enddate":"","@coverage":"1"}}}},
					"carrierlist":{"carrier":{"@id":"car40906","@name":"HUMANA MEDICARE"}}}`))
			},
			fake: func(f *advancedmdtest.Adapter) { f.Demographics["123"] = found },
			call: readDemographics,
			want: outcome{Value: found},
		},
		{
			name: "demographics for a different patient are an invalid response",
			op:   "GetPatientDemographics",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "getdemographic", results(`{"patientlist":{"patient":{"@id":"pat999","@name":"ROE,RICHARD"}}}`))
			},
			fake: func(f *advancedmdtest.Adapter) {
				f.DemographicErrors["123"] = advancedmd.NewError(safeerrors.CategoryInvalidResponse)
			},
			call: readDemographics,
			want: outcome{Value: domain.PatientDemographics{}, Category: safeerrors.CategoryInvalidResponse},
		},
		{
			name: "demographics rejected by AdvancedMD",
			op:   "GetPatientDemographics",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "getdemographic", rejected("Patient not found")) },
			fake: func(f *advancedmdtest.Adapter) {
				f.DemographicErrors["123"] = advancedmd.NewError(safeerrors.CategoryRejected)
			},
			call: readDemographics,
			want: outcome{Value: domain.PatientDemographics{}, Category: safeerrors.CategoryRejected, Mutation: advancedmd.MutationRejected},
		},
	}
}

var bachFollowUp = domain.PatientAppointment{
	ID:                9570263,
	Start:             time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC),
	Provider:          "Dr. Austin Bach",
	Type:              "Established Adult Medical (Follow Up)",
	AppointmentTypeID: 1007,
	Facility:          "Abita Eye Group Spring Hill",
	OfficeID:          "spring_hill",
	Office:            "Spring Hill",
}

var lichtCrystalRiver = domain.PatientAppointment{
	ID:                9570264,
	Start:             time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC),
	Provider:          "Dr. Joseph Licht",
	Type:              "Crystal River Established Patient",
	AppointmentTypeID: 6169,
	Facility:          "Abita Eye Group Crystal River",
	OfficeID:          "crystal_river",
	Office:            "Crystal River",
}

func patientRows(p *advancedmdtest.Provider) {
	p.Appointments = []clients.AMDAppointmentResponse{
		{ID: 9570263, StartDateTime: "2026-08-03T09:00:00", ColumnID: 1513, Provider: "BACH, AUSTIN", Facility: "ABITA EYE GROUP SPRING HILL", AppointmentTypes: []int{1007}, PatientID: 123},
		{ID: 9570264, StartDateTime: "2026-08-15T09:00:00", ColumnID: 1593, Provider: "LICHT, JOSEPH", Facility: "ABITA EYE GROUP CRYSTAL RIVER", AppointmentTypes: []int{6169}, PatientID: 123},
		{ID: 111, StartDateTime: "2026-07-20T09:00:00", ColumnID: 1513, PatientID: 123},
		{ID: 222, StartDateTime: "2026-08-10T09:00:00", ColumnID: 1513, PatientID: 999},
	}
}

func readPatientAppointments(patientID string) func(context.Context, records) (any, error) {
	return func(ctx context.Context, r records) (any, error) {
		return r.ReadPatientAppointments(ctx, domain.PatientAppointmentsQuery{PatientID: patientID, OfficeIDs: []string{"spring_hill"}})
	}
}

func patientAppointmentScenarios() []scenario {
	return []scenario{
		{
			name: "patient appointments found across six month reads",
			op:   "ReadPatientAppointments",
			amd:  patientRows,
			fake: func(f *advancedmdtest.Adapter) {
				f.AppointmentResults["123"] = advancedmdtest.AppointmentResult{Read: advancedmd.AppointmentRead{
					Appointments: []domain.PatientAppointment{bachFollowUp}, Complete: true,
				}}
			},
			call: readPatientAppointments("123"),
			want: outcome{Value: advancedmd.AppointmentRead{Appointments: []domain.PatientAppointment{bachFollowUp}, Complete: true, ProviderReads: 6}},
		},
		{
			name: "patient with no appointments",
			op:   "ReadPatientAppointments",
			call: readPatientAppointments("123"),
			want: outcome{Value: advancedmd.AppointmentRead{Appointments: []domain.PatientAppointment{}, Complete: true, ProviderReads: 6}},
		},
		{
			name: "patient appointments with a row missing its patient are incomplete",
			op:   "ReadPatientAppointments",
			amd: func(p *advancedmdtest.Provider) {
				patientRows(p)
				p.Appointments = append(p.Appointments, clients.AMDAppointmentResponse{ID: 333, StartDateTime: "2026-09-01T09:00:00", ColumnID: 1598})
			},
			fake: func(f *advancedmdtest.Adapter) {
				f.AppointmentResults["123"] = advancedmdtest.AppointmentResult{Read: advancedmd.AppointmentRead{
					Appointments: []domain.PatientAppointment{bachFollowUp}, Complete: false,
				}}
			},
			call: readPatientAppointments("123"),
			want: outcome{Value: advancedmd.AppointmentRead{Appointments: []domain.PatientAppointment{bachFollowUp}, Complete: false, ProviderReads: 6}},
		},
		{
			name: "patient appointments fail when one month fails",
			op:   "ReadPatientAppointments",
			amd: func(p *advancedmdtest.Provider) {
				patientRows(p)
				reply(p, "appointments month 2026-09-01 "+springHillColumns, status(http.StatusServiceUnavailable))
			},
			fake: func(f *advancedmdtest.Adapter) {
				f.AppointmentResults["123"] = advancedmdtest.AppointmentResult{Err: advancedmd.NewError(safeerrors.CategoryUpstreamStatus)}
			},
			call: readPatientAppointments("123"),
			want: outcome{Value: advancedmd.AppointmentRead{ProviderReads: 6}, Category: safeerrors.CategoryUpstreamStatus},
		},
		{
			name: "patient appointments for a non-numeric patient ID are an internal error",
			op:   "ReadPatientAppointments",
			call: readPatientAppointments("pat123"),
			want: outcome{Value: advancedmd.AppointmentRead{}, Category: safeerrors.CategoryInternal},
		},
	}
}

func readMonth(offices []string, month time.Time) func(context.Context, records) (any, error) {
	return func(ctx context.Context, r records) (any, error) {
		return r.ReadPatientAppointmentsForMonth(ctx, advancedmd.AppointmentMonthQuery{PatientID: "123", OfficeIDs: offices, Month: month})
	}
}

func monthAppointmentScenarios() []scenario {
	august := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	return []scenario{
		{
			name: "month read for one office",
			op:   "ReadPatientAppointmentsForMonth",
			amd:  patientRows,
			fake: func(f *advancedmdtest.Adapter) {
				f.AppointmentResults["123"] = advancedmdtest.AppointmentResult{Read: advancedmd.AppointmentRead{
					Appointments: []domain.PatientAppointment{bachFollowUp}, Complete: true,
				}}
			},
			call: readMonth([]string{"spring_hill"}, august),
			want: outcome{Value: advancedmd.AppointmentRead{Appointments: []domain.PatientAppointment{bachFollowUp}, Complete: true, ProviderReads: 1}},
		},
		{
			name: "month read reads each office once",
			op:   "ReadPatientAppointmentsForMonth",
			amd:  patientRows,
			fake: func(f *advancedmdtest.Adapter) {
				f.AppointmentResults["123"] = advancedmdtest.AppointmentResult{Read: advancedmd.AppointmentRead{
					Appointments: []domain.PatientAppointment{bachFollowUp, lichtCrystalRiver}, Complete: true,
				}}
			},
			call: readMonth([]string{"spring_hill", "crystal_river"}, august),
			want: outcome{Value: advancedmd.AppointmentRead{Appointments: []domain.PatientAppointment{bachFollowUp, lichtCrystalRiver}, Complete: true, ProviderReads: 2}},
		},
		{
			name: "month read fails when its office read fails",
			op:   "ReadPatientAppointmentsForMonth",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "appointments month 2026-08-01 "+springHillColumns, status(http.StatusGatewayTimeout))
			},
			fake: func(f *advancedmdtest.Adapter) {
				f.AppointmentResults["123"] = advancedmdtest.AppointmentResult{Err: advancedmd.NewError(safeerrors.CategoryUpstreamStatus)}
			},
			call: readMonth([]string{"spring_hill"}, august),
			want: outcome{Value: advancedmd.AppointmentRead{Appointments: []domain.PatientAppointment{}, Complete: true, ProviderReads: 1}, Category: safeerrors.CategoryUpstreamStatus},
		},
		{
			name: "month read without a month is an internal error",
			op:   "ReadPatientAppointmentsForMonth",
			call: readMonth([]string{"spring_hill"}, time.Time{}),
			want: outcome{Value: advancedmd.AppointmentRead{}, Category: safeerrors.CategoryInternal},
		},
	}
}

func readState(query advancedmd.AppointmentStateQuery) func(context.Context, records) (any, error) {
	return func(ctx context.Context, r records) (any, error) {
		return r.ReadAppointmentState(ctx, query)
	}
}

func appointmentStateScenarios() []scenario {
	query := advancedmd.AppointmentStateQuery{AppointmentID: 9570263, OfficeID: "spring_hill", Start: bachFollowUp.Start}
	return []scenario{
		{
			name: "appointment state for a booked appointment",
			op:   "ReadAppointmentState",
			amd:  patientRows,
			fake: func(f *advancedmdtest.Adapter) {
				f.AppointmentStateResults[9570263] = advancedmdtest.AppointmentStateResult{State: advancedmd.AppointmentState{Exists: true, Complete: true}}
			},
			call: readState(query),
			want: outcome{Value: advancedmd.AppointmentState{Exists: true, Complete: true}},
		},
		{
			name: "appointment state without a start is an internal error",
			op:   "ReadAppointmentState",
			call: readState(advancedmd.AppointmentStateQuery{AppointmentID: 9570263, OfficeID: "spring_hill"}),
			want: outcome{Value: advancedmd.AppointmentState{}, Category: safeerrors.CategoryInternal},
		},
	}
}

func readSetup(ctx context.Context, r records) (any, error) {
	return r.GetSchedulerSetup(ctx)
}

func schedulerSetupScenarios() []scenario {
	column := domain.SchedulerColumn{
		ID: "1513", Name: "BACH SPRING HILL", ProfileID: "620", FacilityID: "1568",
		StartTime: "08:00", EndTime: "17:00", Interval: 15, MaxApptsPerSlot: 2, Workweek: 62,
	}
	return []scenario{
		{
			name: "scheduler setup",
			op:   "GetSchedulerSetup",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "getschedulersetup", results(`{"columnlist":{"column":{"@id":"col1513","@name":"BACH SPRING HILL","@profile":"prof620","@facility":"fac1568",
					"columnsetting":{"@start":"0800","@end":"1700","@interval":"15","@maxapptsperslot":"2","@workweek":"1111100"}}}}`))
			},
			fake: func(f *advancedmdtest.Adapter) {
				f.SchedulerSetup = domain.SchedulerSetup{Columns: []domain.SchedulerColumn{column}}
			},
			call: readSetup,
			want: outcome{Value: domain.SchedulerSetup{Columns: []domain.SchedulerColumn{column}}},
		},
		{
			name: "scheduler setup without columns is an invalid response",
			op:   "GetSchedulerSetup",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "getschedulersetup", results(`{"columnlist":{}}`)) },
			call: readSetup,
			want: outcome{Value: domain.SchedulerSetup{}, Category: safeerrors.CategoryInvalidResponse},
		},
		{
			name: "scheduler setup rejected by AdvancedMD",
			op:   "GetSchedulerSetup",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "getschedulersetup", rejected("Not authorized")) },
			fake: func(f *advancedmdtest.Adapter) {
				f.SchedulerSetupError = advancedmd.NewError(safeerrors.CategoryRejected)
			},
			call: readSetup,
			want: outcome{Value: domain.SchedulerSetup{}, Category: safeerrors.CategoryRejected, Mutation: advancedmd.MutationRejected},
		},
	}
}

func at(date string, clock string) time.Time {
	parsed, err := time.Parse("2006-01-02 15:04", date+" "+clock)
	if err != nil {
		panic(err)
	}
	return parsed
}

func appointmentRow(id, column int, start time.Time, duration int) clients.AMDAppointmentResponse {
	return clients.AMDAppointmentResponse{ID: id, StartDateTime: start.Format("2006-01-02T15:04:05"), Duration: duration, ColumnID: column}
}

func holdRow(id, column int, start, end time.Time) advancedmdtest.HoldRow {
	return advancedmdtest.HoldRow{ID: id, StartDateTime: start.Format("2006-01-02T15:04:05"), EndDateTime: end.Format("2006-01-02T15:04:05"), Column: advancedmdtest.HoldColumn{ID: column}}
}

func scheduled(id, column int, start time.Time, duration int) domain.Appointment {
	return domain.Appointment{ID: id, StartDateTime: start, Duration: duration, ColumnID: column}
}

func held(id, column int, start, end time.Time) domain.BlockHold {
	return domain.BlockHold{ID: id, StartDateTime: start, EndDateTime: end, ColumnID: column}
}

func complete(appointments []domain.Appointment, holds []domain.BlockHold) domain.ColumnSchedule {
	return domain.ColumnSchedule{Appointments: appointments, BlockHolds: holds, AppointmentsComplete: true, BlockHoldsComplete: true}
}

func readDay(ctx context.Context, r records) (any, error) {
	return r.ReadSchedule(ctx, domain.ScheduleReadQuery{ColumnIDs: []string{"1513", "1598"}, Date: "2026-06-03"})
}

func scheduleDayScenarios() []scenario {
	const day = "2026-06-03"
	dayRows := func(p *advancedmdtest.Provider) {
		p.Appointments = []clients.AMDAppointmentResponse{
			appointmentRow(18, 1513, at(day, "09:00"), 30),
			appointmentRow(18, 1513, at(day, "09:00"), 30),
			appointmentRow(0, 1513, at(day, "10:00"), 15),
			appointmentRow(0, 1513, at(day, "10:00"), 15),
		}
		p.Holds = []advancedmdtest.HoldRow{
			holdRow(7, 1513, at(day, "12:00"), at(day, "13:00")),
			holdRow(7, 1513, at(day, "12:00"), at(day, "13:00")),
		}
	}
	filedRows := func(f *advancedmdtest.Adapter) {
		f.Schedule[day] = map[string]advancedmdtest.ColumnRows{
			"1513": {
				Appointments: []domain.Appointment{
					scheduled(18, 1513, at(day, "09:00"), 30),
					scheduled(18, 1513, at(day, "09:00"), 30),
					scheduled(0, 1513, at(day, "10:00"), 15),
					scheduled(0, 1513, at(day, "10:00"), 15),
				},
				BlockHolds: []domain.BlockHold{held(7, 1513, at(day, "12:00"), at(day, "13:00")), held(7, 1513, at(day, "12:00"), at(day, "13:00"))},
			},
		}
	}
	bach := complete(
		[]domain.Appointment{
			scheduled(18, 1513, at(day, "09:00"), 30),
			scheduled(0, 1513, at(day, "10:00"), 15),
			scheduled(0, 1513, at(day, "10:00"), 15),
		},
		[]domain.BlockHold{held(7, 1513, at(day, "12:00"), at(day, "13:00"))},
	)
	return []scenario{
		{
			name: "day read merges repeated rows, keeps id-less rows, and returns every queried column",
			op:   "ReadSchedule",
			amd:  dayRows,
			fake: filedRows,
			call: readDay,
			want: outcome{Value: domain.ScheduleReadResult{Columns: map[string]domain.ColumnSchedule{
				"1513": bach,
				"1598": complete(nil, nil),
			}}},
		},
		{
			name: "day read with one failing column reports that column incomplete",
			op:   "ReadSchedule",
			amd: func(p *advancedmdtest.Provider) {
				dayRows(p)
				reply(p, "appointments day "+day+" 1598", status(http.StatusServiceUnavailable))
				reply(p, "blockholds day "+day+" 1598", status(http.StatusServiceUnavailable))
			},
			fake: func(f *advancedmdtest.Adapter) {
				filedRows(f)
				f.ColumnReadErrors[advancedmdtest.ColumnDay{Date: day, ColumnID: "1598"}] = advancedmd.NewError(safeerrors.CategoryUpstreamStatus)
			},
			call: readDay,
			want: outcome{Value: domain.ScheduleReadResult{Columns: map[string]domain.ColumnSchedule{
				"1513": bach,
				"1598": {},
			}}},
		},
		{
			name: "day read fails when every column fails",
			op:   "ReadSchedule",
			amd: func(p *advancedmdtest.Provider) {
				for _, route := range []string{"appointments", "blockholds"} {
					for _, column := range []string{"1513", "1598"} {
						reply(p, route+" day "+day+" "+column, status(http.StatusServiceUnavailable))
					}
				}
			},
			fake: func(f *advancedmdtest.Adapter) {
				f.ScheduleReadErrors[day] = advancedmd.NewError(safeerrors.CategoryUpstreamStatus)
			},
			call: readDay,
			want: outcome{
				Value:    domain.ScheduleReadResult{Columns: map[string]domain.ColumnSchedule{"1513": {}, "1598": {}}},
				Category: safeerrors.CategoryUpstreamStatus,
			},
		},
	}
}

func readRange(ctx context.Context, r records) (any, error) {
	return r.ReadScheduleRange(ctx, domain.ScheduleRangeQuery{
		ColumnIDs: []string{"1513", "1598"},
		Start:     time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
	})
}

func scheduleRangeScenarios() []scenario {
	rangeRows := func(p *advancedmdtest.Provider) {
		p.Appointments = []clients.AMDAppointmentResponse{
			appointmentRow(18, 1513, at("2026-06-30", "09:00"), 30),
			appointmentRow(18, 1513, at("2026-06-30", "09:00"), 30),
			appointmentRow(0, 1513, at("2026-07-01", "10:00"), 15),
			appointmentRow(0, 1513, at("2026-07-01", "10:00"), 15),
			appointmentRow(19, 1598, at("2026-07-02", "11:00"), 15),
		}
		p.Holds = []advancedmdtest.HoldRow{
			holdRow(7, 1513, at("2026-06-30", "22:00"), at("2026-07-01", "02:00")),
		}
	}
	overnight := held(7, 1513, at("2026-06-30", "22:00"), at("2026-07-01", "02:00"))
	filedRows := func(f *advancedmdtest.Adapter) {
		f.Schedule["2026-06-30"] = map[string]advancedmdtest.ColumnRows{
			"1513": {
				Appointments: []domain.Appointment{scheduled(18, 1513, at("2026-06-30", "09:00"), 30), scheduled(18, 1513, at("2026-06-30", "09:00"), 30)},
				BlockHolds:   []domain.BlockHold{overnight},
			},
		}
		f.Schedule["2026-07-01"] = map[string]advancedmdtest.ColumnRows{
			"1513": {
				Appointments: []domain.Appointment{scheduled(0, 1513, at("2026-07-01", "10:00"), 15), scheduled(0, 1513, at("2026-07-01", "10:00"), 15)},
				BlockHolds:   []domain.BlockHold{overnight},
			},
		}
		f.Schedule["2026-07-02"] = map[string]advancedmdtest.ColumnRows{
			"1598": {Appointments: []domain.Appointment{scheduled(19, 1598, at("2026-07-02", "11:00"), 15)}},
		}
	}
	empty := complete(nil, nil)
	read := map[string]domain.ScheduleReadResult{
		"2026-06-29": {Columns: map[string]domain.ColumnSchedule{"1513": empty, "1598": empty}},
		"2026-06-30": {Columns: map[string]domain.ColumnSchedule{
			"1513": complete([]domain.Appointment{scheduled(18, 1513, at("2026-06-30", "09:00"), 30)}, []domain.BlockHold{overnight}),
			"1598": empty,
		}},
		"2026-07-01": {Columns: map[string]domain.ColumnSchedule{
			"1513": complete(
				[]domain.Appointment{scheduled(0, 1513, at("2026-07-01", "10:00"), 15), scheduled(0, 1513, at("2026-07-01", "10:00"), 15)},
				[]domain.BlockHold{overnight},
			),
			"1598": empty,
		}},
		"2026-07-02": {Columns: map[string]domain.ColumnSchedule{
			"1513": empty,
			"1598": complete([]domain.Appointment{scheduled(19, 1598, at("2026-07-02", "11:00"), 15)}, nil),
		}},
	}
	unattributed := maps.Clone(read)
	unattributed["2026-07-02"] = domain.ScheduleReadResult{Columns: map[string]domain.ColumnSchedule{
		"1513": {},
		"1598": {Appointments: []domain.Appointment{scheduled(19, 1598, at("2026-07-02", "11:00"), 15)}},
	}}
	return []scenario{
		{
			name: "range read merges repeated rows, keeps id-less rows, files holds on every day they cover, and returns every queried column",
			op:   "ReadScheduleRange",
			amd:  rangeRows,
			fake: filedRows,
			call: readRange,
			want: outcome{Value: read},
		},
		{
			name: "range read reports a day incomplete when a row belongs to no queried column",
			op:   "ReadScheduleRange",
			amd: func(p *advancedmdtest.Provider) {
				rangeRows(p)
				reply(p, "appointments month 2026-07-01 1513-1598", ok(`[
					{"id":0,"startdatetime":"2026-07-01T10:00:00","duration":15,"columnid":1513},
					{"id":0,"startdatetime":"2026-07-01T10:00:00","duration":15,"columnid":1513},
					{"id":19,"startdatetime":"2026-07-02T11:00:00","duration":15,"columnid":1598},
					{"id":20,"startdatetime":"2026-07-02T11:00:00","duration":15,"columnid":9999}
				]`))
			},
			fake: func(f *advancedmdtest.Adapter) {
				filedRows(f)
				f.IncompleteDays["2026-07-02"] = true
			},
			call: readRange,
			want: outcome{Value: unattributed},
		},
		{
			name: "range read fails when one month fails",
			op:   "ReadScheduleRange",
			amd: func(p *advancedmdtest.Provider) {
				rangeRows(p)
				reply(p, "blockholds month 2026-07-01 1513-1598", status(http.StatusServiceUnavailable))
			},
			fake: func(f *advancedmdtest.Adapter) {
				filedRows(f)
				f.ColumnReadErrors[advancedmdtest.ColumnDay{Date: "2026-07-01", ColumnID: "1598"}] = advancedmd.NewError(safeerrors.CategoryUpstreamStatus)
			},
			call: readRange,
			want: outcome{Value: map[string]domain.ScheduleReadResult(nil), Category: safeerrors.CategoryUpstreamStatus},
		},
	}
}

var booking = advancedmd.Booking{
	PatientID: 123, OfficeID: "spring_hill", ColumnID: 1513, ProfileID: 620,
	Start: time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC), Duration: 15,
	ProviderAppointmentTypeID: 1007, AppointmentColor: "#3366FF", Comments: "Follow up",
}

func book(b advancedmd.Booking) func(context.Context, records) (any, error) {
	return func(ctx context.Context, r records) (any, error) {
		return r.BookAppointment(ctx, b)
	}
}

func bookingScenarios() []scenario {
	untyped := booking
	untyped.ProviderAppointmentTypeID = 0
	uncolored := booking
	uncolored.AppointmentColor = ""
	elsewhere := booking
	elsewhere.OfficeID = "nowhere"
	return []scenario{
		{
			name: "book",
			op:   "BookAppointment",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "book", ok(`{"id":98765}`)) },
			fake: func(f *advancedmdtest.Adapter) { f.BookAppointmentID = 98765 },
			call: book(booking),
			want: outcome{Value: 98765, Writes: 1},
		},
		{
			name: "book rejected by AdvancedMD",
			op:   "BookAppointment",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "book", status(http.StatusBadRequest)) },
			fake: func(f *advancedmdtest.Adapter) {
				f.BookAppointmentErr = advancedmd.NewError(safeerrors.CategoryRejected)
			},
			call: book(booking),
			want: outcome{Value: 0, Category: safeerrors.CategoryRejected, Mutation: advancedmd.MutationRejected, Writes: 1},
		},
		{
			name: "book with an unknown outcome is an ambiguous write",
			op:   "BookAppointment",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "book", status(http.StatusServiceUnavailable)) },
			fake: func(f *advancedmdtest.Adapter) {
				f.BookAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUpstreamStatus)
			},
			call: book(booking),
			want: outcome{Value: 0, Category: safeerrors.CategoryUpstreamStatus, Mutation: advancedmd.MutationAmbiguous, Writes: 1},
		},
		{
			name: "book without a provider appointment type never reaches AdvancedMD",
			op:   "BookAppointment",
			call: book(untyped),
			want: outcome{Value: 0, Category: safeerrors.CategoryInternal},
		},
		{
			name: "book without an appointment color never reaches AdvancedMD",
			op:   "BookAppointment",
			call: book(uncolored),
			want: outcome{Value: 0, Category: safeerrors.CategoryInternal},
		},
		{
			name: "book for an unknown office never reaches AdvancedMD",
			op:   "BookAppointment",
			call: book(elsewhere),
			want: outcome{Value: 0, Category: safeerrors.CategoryInternal},
		},
	}
}

func cancel(ctx context.Context, r records) (any, error) {
	return nil, r.CancelAppointment(ctx, advancedmd.Cancellation{AppointmentID: 9570263})
}

func cancellationScenarios() []scenario {
	return []scenario{
		{
			name: "cancel",
			op:   "CancelAppointment",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "cancel", ok(`{}`)) },
			call: cancel,
			want: outcome{Writes: 1},
		},
		{
			name: "cancel refused as a conflict",
			op:   "CancelAppointment",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "cancel", status(http.StatusConflict)) },
			fake: func(f *advancedmdtest.Adapter) {
				f.CancelAppointmentErr = advancedmd.NewError(safeerrors.CategoryConflict)
			},
			call: cancel,
			want: outcome{Category: safeerrors.CategoryConflict, Mutation: advancedmd.MutationRejected, Writes: 1},
		},
		{
			name: "cancel with an unknown outcome is an ambiguous write",
			op:   "CancelAppointment",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "cancel", status(http.StatusInternalServerError)) },
			fake: func(f *advancedmdtest.Adapter) {
				f.CancelAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUpstreamStatus)
			},
			call: cancel,
			want: outcome{Category: safeerrors.CategoryUpstreamStatus, Mutation: advancedmd.MutationAmbiguous, Writes: 1},
		},
	}
}

func createPatient(officeID string) func(context.Context, records) (any, error) {
	return func(ctx context.Context, r records) (any, error) {
		return r.CreatePatient(ctx, domain.PatientCreate{
			FirstName: "JANE", LastName: "DOE", DOB: "01/15/1980", Phone: "(954)287-2010",
			Street: "123 Main St", City: "Spring Hill", State: "FL", Zip: "34609", Sex: "F", OfficeID: officeID,
		})
	}
}

func patientCreateScenarios() []scenario {
	created := domain.CreatedPatient{ID: "555", RespPartyID: "resp555", Name: "DOE,JANE"}
	return []scenario{
		{
			name: "create patient",
			op:   "CreatePatient",
			amd: func(p *advancedmdtest.Provider) {
				reply(p, "addpatient", results(`{"patientlist":{"patient":{"@id":"pat555","@respparty":"resp555","@name":"DOE,JANE"}}}`))
			},
			fake: func(f *advancedmdtest.Adapter) { f.CreatedPatient = created },
			call: createPatient("spring_hill"),
			want: outcome{Value: created, Writes: 1},
		},
		{
			name: "create patient rejected by AdvancedMD",
			op:   "CreatePatient",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "addpatient", rejected("Duplicate name/DOB")) },
			fake: func(f *advancedmdtest.Adapter) {
				f.CreatedPatient = created
				f.CreatePatientError = advancedmd.NewError(safeerrors.CategoryRejected)
			},
			call: createPatient("spring_hill"),
			want: outcome{Value: domain.CreatedPatient{}, Category: safeerrors.CategoryRejected, Mutation: advancedmd.MutationRejected, Writes: 1},
		},
		{
			name: "create patient for an unknown office never reaches AdvancedMD",
			op:   "CreatePatient",
			fake: func(f *advancedmdtest.Adapter) { f.CreatedPatient = created },
			call: createPatient("nowhere"),
			want: outcome{Value: domain.CreatedPatient{}, Category: safeerrors.CategoryInternal},
		},
	}
}

func addInsurance(ctx context.Context, r records) (any, error) {
	return nil, r.AddPatientInsurance(ctx, domain.PatientInsurance{PatientID: "123", RespPartyID: "resp456", CarrierID: "car40906", SubscriberNum: "H123"})
}

func endInsurance(ctx context.Context, r records) (any, error) {
	return nil, r.EndDatePatientInsurance(ctx, domain.PatientInsuranceEnd{PatientID: "123", InsPlanID: "ins789"})
}

func insuranceScenarios() []scenario {
	saved := results(`{"patient":{"@id":"pat123"}}`)
	return []scenario{
		{
			name: "add insurance",
			op:   "AddPatientInsurance",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "addinsurance", saved) },
			call: addInsurance,
			want: outcome{Writes: 1},
		},
		{
			name: "add insurance rejected by AdvancedMD",
			op:   "AddPatientInsurance",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "addinsurance", rejected("Invalid carrier")) },
			fake: func(f *advancedmdtest.Adapter) {
				f.AddInsuranceError = advancedmd.NewError(safeerrors.CategoryRejected)
			},
			call: addInsurance,
			want: outcome{Category: safeerrors.CategoryRejected, Mutation: advancedmd.MutationRejected, Writes: 1},
		},
		{
			name: "end-date insurance",
			op:   "EndDatePatientInsurance",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "addinsurance", saved) },
			call: endInsurance,
			want: outcome{Writes: 1},
		},
		{
			name: "end-date insurance with an unknown outcome is an ambiguous write",
			op:   "EndDatePatientInsurance",
			amd:  func(p *advancedmdtest.Provider) { reply(p, "addinsurance", status(http.StatusBadGateway)) },
			fake: func(f *advancedmdtest.Adapter) {
				f.EndInsuranceError = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryUpstreamStatus)
			},
			call: endInsurance,
			want: outcome{Category: safeerrors.CategoryUpstreamStatus, Mutation: advancedmd.MutationAmbiguous, Writes: 1},
		},
	}
}
