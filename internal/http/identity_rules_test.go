package http

import (
	"encoding/json"
	"strings"
	"testing"
)

func postIdentity(t *testing.T, path string, body map[string]any) (map[string]any, string) {
	t.Helper()
	fields := strings.SplitN(newPinHarness(t).post(t, path, body), "\t", 3)
	var response map[string]any
	if err := json.Unmarshal([]byte(fields[1]), &response); err != nil {
		t.Fatal(err)
	}
	return response, fields[2]
}

func TestEveryPatientRouteStripsThePatPrefixAndPadding(t *testing.T) {
	for _, raw := range []string{"pat101", " 101 ", " pat101 "} {
		resolved, calls := postIdentity(t, "/api/patient/resolve", map[string]any{"office": "Spring Hill", "patientId": raw})
		if resolved["status"] != "verified" || resolved["patientId"] != "101" || calls != `amd getdemographic patientid="101"; appointments patient="101"` {
			t.Errorf("resolve %q = %v, calls %s", raw, resolved, calls)
		}
		updated, calls := postIdentity(t, "/api/patient/update-insurance", map[string]any{
			"office": "Spring Hill", "patientId": raw, "dob": "01/15/1980", "insurance": "Meritain Health", "subscriberNum": "M101",
		})
		want := `amd addinsurance patient="101" carrier="car301578" subscribernum="M101"; amd enddateinsurance patient="101" insplan="ins101"; amd getdemographic patientid="101"`
		if updated["status"] != "updated" || updated["patientId"] != "101" || calls != want {
			t.Errorf("update-insurance %q = %v, calls %s", raw, updated, calls)
		}
	}
	rejected, calls := postIdentity(t, "/api/patient/update-insurance", map[string]any{
		"office": "Spring Hill", "patientId": "abc", "dob": "01/15/1980", "insurance": "Meritain Health", "subscriberNum": "M101",
	})
	if rejected["outcome"] != "validation_failed" || rejected["message"] != "patientId must be numeric" || calls != "" {
		t.Errorf("update-insurance abc = %v, calls %s", rejected, calls)
	}
}

func TestEveryPatientRouteReadsPhonesAsTheSameDigits(t *testing.T) {
	registration := map[string]any{
		"firstName": "Jane", "lastName": "Roe", "dob": "01/15/1980",
		"street": "1 Main St", "city": "Spring Hill", "state": "FL", "zip": "34606", "sex": "female",
		"insurance": "Meritain Health", "subscriberName": "Jane Roe", "subscriberNum": "M123", "office": "Spring Hill",
	}
	for _, raw := range []string{" ", "abc", "+"} {
		created, calls := postIdentity(t, "/api/add-patient", with(registration, map[string]any{"phone": raw}))
		if created["outcome"] != "validation_failed" || created["message"] != "phone must contain at least one digit" || calls != "" {
			t.Errorf("add-patient phone %q = %v, calls %s", raw, created, calls)
		}
	}
	created, calls := postIdentity(t, "/api/add-patient", with(registration, map[string]any{"phone": "555-0199"}))
	if created["status"] != "created" || !strings.Contains(calls, `homephone="5550199"`) || !strings.Contains(calls, `lookuppatient phone="5550199"`) {
		t.Errorf("add-patient seven digits = %v, calls %s", created, calls)
	}
	resolved, _ := postIdentity(t, "/api/patient/resolve", map[string]any{"office": "Spring Hill", "phone": "+1 954 555 0105"})
	if resolved["status"] != "verified" || resolved["phone"] != "9545550105" {
		t.Errorf("resolve phone echo = %v", resolved)
	}
}

func TestEveryDOBRouteRejectsAnInvalidDOB(t *testing.T) {
	for _, raw := range []string{"garbage", "13/45/1980", "2099-01-01"} {
		resolved, calls := postIdentity(t, "/api/patient/resolve", map[string]any{"office": "Spring Hill", "phone": "9545550100", "dob": raw})
		if resolved["status"] != "error" || resolved["message"] != "dob must be a valid date" || calls != "" {
			t.Errorf("resolve phone + dob %q = %v, calls %s", raw, resolved, calls)
		}
		decided, _ := postIdentity(t, "/api/insurance/decision", map[string]any{"office": "Hollywood", "plan": "VSP", "coverageType": "routine_vision", "dob": raw})
		if decided["status"] != "error" || decided["message"] != "dob must be a valid date" {
			t.Errorf("insurance decision dob %q = %v", raw, decided)
		}
	}
	decided, _ := postIdentity(t, "/api/insurance/decision", map[string]any{"office": "Hollywood", "plan": "VSP", "coverageType": "routine_vision", "dob": "May 1, 2020"})
	if decided["outcome"] != "accepted" || len(decided["allowedProviders"].([]any)) != 2 {
		t.Errorf("insurance decision for a six-year-old = %v", decided)
	}
}

func TestASpokenFullGivenNameMatchesAChartWithAMiddleName(t *testing.T) {
	for _, spoken := range []string{"Mary Ann", "Maryann", "MARY-ANN"} {
		resolved, _ := postIdentity(t, "/api/patient/resolve", map[string]any{"office": "Spring Hill", "firstName": spoken, "dob": "01/15/1980"})
		matches, _ := resolved["matches"].([]any)
		if resolved["status"] != "multiple_matches" || len(matches) != 2 || matches[0].(map[string]any)["patientId"] != "101" {
			t.Errorf("resolve %q must not pick LEE,MARYANN over SMITH,MARY ANN: %v", spoken, resolved)
		}
		created, calls := postIdentity(t, "/api/add-patient", map[string]any{
			"firstName": spoken, "lastName": "Smith", "dob": "01/15/1980", "phone": "9545550100",
			"street": "1 Main St", "city": "Spring Hill", "state": "FL", "zip": "34606", "sex": "female",
			"insurance": "Meritain Health", "subscriberName": "Mary Smith", "subscriberNum": "M123", "office": "Spring Hill",
		})
		if created["outcome"] != "validation_failed" || strings.Contains(calls, "addpatient") {
			t.Errorf("add-patient %q must find SMITH,MARY ANN: %v, calls %s", spoken, created, calls)
		}
	}
}

func TestPhoneAndLastNameLookupsCompareFirstNamesByTheirLetters(t *testing.T) {
	for _, spoken := range []string{"Maryann", "Mary-Ann", " Mary", "Mar"} {
		resolved, _ := postIdentity(t, "/api/patient/resolve", map[string]any{"office": "Spring Hill", "phone": "9545550100", "firstName": spoken})
		if resolved["status"] != "verified" || resolved["patientId"] != "101" {
			t.Errorf("phone + firstName %q = %v", spoken, resolved)
		}
	}
	resolved, _ := postIdentity(t, "/api/patient/resolve", map[string]any{"office": "Spring Hill", "phone": "9545550100", "firstName": "123"})
	if resolved["status"] != "not_found" {
		t.Errorf("phone + letterless firstName = %v", resolved)
	}
}

func TestCandidatesNameAChartTheSameWayOnEveryLookup(t *testing.T) {
	candidate := func(body map[string]any) map[string]any {
		resolved, _ := postIdentity(t, "/api/patient/resolve", body)
		matches, _ := resolved["matches"].([]any)
		for _, match := range matches {
			if match.(map[string]any)["patientId"] == "101" {
				return match.(map[string]any)
			}
		}
		t.Fatalf("chart 101 missing from %v", resolved)
		return nil
	}
	byPhone := candidate(map[string]any{"office": "Spring Hill", "phone": "9545550100"})
	byFirstName := candidate(map[string]any{"office": "Spring Hill", "firstName": "Mary", "dob": "01/15/1980"})
	if byPhone["firstName"] != "MARY ANN" || byFirstName["firstName"] != "MARY ANN" || byPhone["lastName"] != byFirstName["lastName"] {
		t.Errorf("phone candidate %v, first-name candidate %v", byPhone, byFirstName)
	}
}

func TestBookingReceiptNamesThePatientFromTheChartName(t *testing.T) {
	for raw, want := range map[string]string{"SMITH,MARY ANN": "Mary Ann Smith", "DOE,": "Doe", ",JANE": "Jane", "Jane McDoe": "Jane McDoe"} {
		booked, _ := postIdentity(t, "/api/appointment/book", map[string]any{
			"patientId": "12345", "patientName": raw, "dob": "01/15/1980",
			"bookingToken": signedPinSlot(t, "01/15/1980"), "appointmentTypeId": 1007, "office": "Spring Hill",
		})
		if booked["status"] != "booked" || booked["patientName"] != want {
			t.Errorf("patientName %q = %v, want %q", raw, booked["patientName"], want)
		}
	}
}
