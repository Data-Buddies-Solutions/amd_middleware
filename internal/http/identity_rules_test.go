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
