package http

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"

	patientmodule "advancedmd-token-management/internal/patient"
	schedulingmodule "advancedmd-token-management/internal/scheduling"
)

const abitaS2SCommit = "2c97833f3075a24c308a2e01c83f98966358d603"

var abitaS2SRequestBodies = []struct {
	name    string
	path    string
	body    map[string]any
	reaches []string
}{
	{"resolve by phone", "/api/patient/resolve", map[string]any{"phone": "3525550100", "office": "spring-hill"}, []string{"Resolve"}},
	{"resolve by first name and dob", "/api/patient/resolve", map[string]any{"firstName": "Jane", "dob": "01/15/1980", "office": "spring-hill"}, []string{"Resolve"}},
	{"resolve by patient id", "/api/patient/resolve", map[string]any{"patientId": "12345", "office": "spring-hill"}, []string{"Resolve"}},
	{"add patient required", "/api/add-patient", map[string]any{
		"firstName": "Jane", "lastName": "Doe", "dob": "01/15/1980", "street": "12 Main St", "city": "Spring Hill",
		"state": "FL", "zip": "34609", "sex": "female", "phone": "3525550100", "aptSuite": "",
		"insurance": "Aetna Commercial", "subscriberName": "Jane Doe", "subscriberNum": "M123", "office": "spring-hill",
	}, []string{"Create"}},
	{"add patient with optional fields", "/api/add-patient", map[string]any{
		"firstName": "Jane", "lastName": "Doe", "dob": "01/15/1980", "street": "12 Main St", "city": "Spring Hill",
		"state": "FL", "zip": "34609", "sex": "female", "phone": "3525550100", "aptSuite": "Apt 2",
		"insurance": "VSP", "subscriberName": "Jane Doe", "subscriberNum": "M123", "office": "spring-hill",
		"insurancePlanId": "plan-1", "email": "jane@example.com", "coverageType": "routine_vision",
	}, []string{"Create"}},
	{"update insurance required", "/api/patient/update-insurance", map[string]any{
		"patientId": "12345", "dob": "01/15/1980", "insurance": "Aetna Commercial", "coverageType": "medical",
		"subscriberNum": "M123", "office": "spring-hill",
	}, []string{"UpdateInsurance"}},
	{"update insurance with plan id", "/api/patient/update-insurance", map[string]any{
		"patientId": "12345", "dob": "01/15/1980", "insurance": "Aetna Commercial", "coverageType": "medical",
		"subscriberNum": "M123", "office": "spring-hill", "insurancePlanId": "plan-1",
	}, []string{"UpdateInsurance"}},
	{"slots", "/api/scheduler/slots", map[string]any{
		"office": "spring-hill", "startDate": "2026-06-01", "patientId": "12345", "visitType": "medical", "dob": "01/15/1980",
	}, []string{"List"}},
	{"book required", "/api/appointment/book", abitaBookingBody(nil), []string{"Book"}},
	{"book with referring doctor contact", "/api/appointment/book", abitaBookingBody(map[string]any{
		"referringDoctorPhone": "3525550101", "referringDoctorAddress": "1 Clinic Way",
	}), []string{"Book"}},
	{"reschedule", "/api/appointment/reschedule", abitaBookingBody(map[string]any{
		"rescheduleToken": "reschedule-token",
	}), []string{"Reschedule"}},
	{"reschedule with referring doctor contact", "/api/appointment/reschedule", abitaBookingBody(map[string]any{
		"rescheduleToken": "reschedule-token", "referringDoctorPhone": "3525550101", "referringDoctorAddress": "1 Clinic Way",
	}), []string{"Reschedule"}},
	{"cancel", "/api/appointment/cancel", map[string]any{"patientId": "12345", "cancellationToken": "cancellation-token"}, []string{"Cancel"}},
	{"insurance decision", "/api/insurance/decision", map[string]any{
		"office": "spring-hill", "plan": "Aetna", "coverageType": "medical", "dob": "01/15/1980",
	}, nil},
	{"insurance decision with offered plans", "/api/insurance/decision", map[string]any{
		"office": "spring-hill", "plan": "Aetna", "coverageType": "medical", "dob": "01/15/1980", "offeredPlanIds": []string{"plan-1"},
	}, nil},
}

func abitaBookingBody(extra map[string]any) map[string]any {
	body := map[string]any{
		"patientId": "12345", "patientName": "DOE,JANE", "dob": "01/15/1980", "bookingToken": "booking-token",
		"visitCategory": "medical", "patientStatus": "established", "appointmentReason": "Eye follow-up",
		"visitReason": "Eye follow-up", "referringDoctor": "Dr. Lee",
	}
	for key, value := range extra {
		body[key] = value
	}
	return body
}

func TestAbitaS2SRequestBodiesPassStrictDecoding(t *testing.T) {
	for _, request := range abitaS2SRequestBodies {
		t.Run("abita_s2s@"+abitaS2SCommit[:7]+"/"+request.name, func(t *testing.T) {
			router, features := newDecodingRouter()
			recorder := postAuthenticatedJSON(t, router, request.path, request.body)
			if recorder.Code != http.StatusOK || !slices.Equal(features.calls, request.reaches) {
				t.Fatalf("abita_s2s %s body rejected by %s: status=%d calls=%v body=%s", abitaS2SCommit, request.path, recorder.Code, features.calls, recorder.Body)
			}
		})
	}
}

func TestJSONRoutesRejectUnknownFieldsBeforeTheFeature(t *testing.T) {
	type rejection struct {
		name, path string
		body       map[string]any
		want       string
	}
	invalidBooking := `{"status":"error","message":"Invalid JSON body"}`
	rejections := []rejection{
		{"add patient", "/api/add-patient", map[string]any{"firstName": "Jane", "office": "spring-hill", "unexpected": true},
			`{"status":"error","message":"Invalid JSON body"}`},
		{"resolve", "/api/patient/resolve", map[string]any{"phone": "3525550100", "office": "spring-hill", "unexpected": true},
			`{"status":"error","appointments":null,"message":"Invalid JSON body","matches":null}`},
		{"book", "/api/appointment/book", abitaBookingBody(map[string]any{"unexpected": true}), invalidBooking},
		{"update insurance", "/api/patient/update-insurance", map[string]any{"patientId": "12345", "office": "spring-hill", "unexpected": true},
			`{"effect":"no_effect","status":"error","message":"Invalid JSON body"}`},
	}
	serverOwnedBookingFields := map[string]any{
		"visitKind": "post_op", "ageBand": "adult", "isPostOp": true, "insurancePlan": "ignored", "columnId": 999,
		"profileId": 999, "startDatetime": "2026-06-03T10:00", "duration": 60, "routing": "optical_only",
	}
	for field, value := range serverOwnedBookingFields {
		rejections = append(rejections,
			rejection{"book server-owned " + field, "/api/appointment/book", abitaBookingBody(map[string]any{field: value}), invalidBooking},
			rejection{"reschedule server-owned " + field, "/api/appointment/reschedule",
				abitaBookingBody(map[string]any{"rescheduleToken": "reschedule-token", field: value}),
				`{"status":"failed","message":"Invalid JSON body"}`},
		)
	}
	for _, rejection := range rejections {
		t.Run(rejection.name, func(t *testing.T) {
			router, features := newDecodingRouter()
			recorder := postAuthenticatedJSON(t, router, rejection.path, rejection.body)
			var got, want any
			if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response %q: %v", recorder.Body, err)
			}
			if err := json.Unmarshal([]byte(rejection.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) || len(features.calls) != 0 {
				t.Fatalf("response=%s calls=%v, want %s and no feature call", recorder.Body, features.calls, rejection.want)
			}
		})
	}
}

func newDecodingRouter() (http.Handler, *featureCalls) {
	features := &featureCalls{}
	return NewRouter(&Handlers{patient: features, scheduling: features, now: testNow}, "agent-secret", nil), features
}

type featureCalls struct {
	calls []string
}

func (f *featureCalls) Resolve(context.Context, patientmodule.ResolveQuery) (patientmodule.ResolveResult, error) {
	f.calls = append(f.calls, "Resolve")
	return patientmodule.ResolveResult{}, nil
}

func (f *featureCalls) Create(context.Context, patientmodule.CreateCommand) patientmodule.CreateResult {
	f.calls = append(f.calls, "Create")
	return patientmodule.CreateResult{}
}

func (f *featureCalls) UpdateInsurance(context.Context, patientmodule.UpdateInsuranceCommand) patientmodule.UpdateInsuranceResult {
	f.calls = append(f.calls, "UpdateInsurance")
	return patientmodule.UpdateInsuranceResult{}
}

func (f *featureCalls) List(context.Context, schedulingmodule.ListCommand) (schedulingmodule.AvailabilityResponse, error) {
	f.calls = append(f.calls, "List")
	return schedulingmodule.AvailabilityResponse{}, nil
}

func (f *featureCalls) Book(context.Context, schedulingmodule.BookCommand) (schedulingmodule.BookReceipt, error) {
	f.calls = append(f.calls, "Book")
	return schedulingmodule.BookReceipt{}, nil
}

func (f *featureCalls) Cancel(context.Context, schedulingmodule.CancelCommand) (schedulingmodule.CancelReceipt, error) {
	f.calls = append(f.calls, "Cancel")
	return schedulingmodule.CancelReceipt{}, nil
}

func (f *featureCalls) Reschedule(context.Context, schedulingmodule.BookCommand) (schedulingmodule.RescheduleReceipt, error) {
	f.calls = append(f.calls, "Reschedule")
	return schedulingmodule.RescheduleReceipt{}, nil
}
