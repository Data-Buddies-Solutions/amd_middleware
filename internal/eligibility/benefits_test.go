package eligibility

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestOnlyVisitBenefitsReachSerializedEligibility(t *testing.T) {
	for _, coverage := range []string{"medical", "routine_vision"} {
		t.Run(coverage, func(t *testing.T) {
			raw := fixture(`[{"code":"1","serviceTypeCodes":["30"]},{"code":"B","serviceTypeCodes":["98"],"benefitAmount":"35"},{"code":"B","serviceTypeCodes":["AL"],"benefitAmount":"0.00","planCoverage":"SILVERELITE","coverageLevel":"Individual","inPlanNetworkIndicator":"Yes","additionalInformation":[{"description":"VISION EXAM"}],"futureQualifier":9007199254740993},{"code":"A","serviceTypeCodes":["AL"],"benefitPercent":"0"},{"code":"B","serviceTypeCodes":["1"],"benefitAmount":"999"},{"code":"B","benefitAmount":"888"}]`, `,"x12":"unrelated-hidden-benefits","futureMetadata":9007199254740993,"planInformation":{"planDescription":"SILVERELITE"},"planStatus":[{"serviceTypeCodes":["1"],"planDetails":"unrelated"}]`)
			s := testService(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, raw) })
			in := input()
			in.Plan = "Davis"
			in.CoverageType = coverage
			result, err := s.Check(context.Background(), "office", in)
			if err != nil || result.Status != "active" {
				t.Fatalf("%+v %v", result, err)
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "999") || strings.Contains(string(encoded), "888") || strings.Contains(string(encoded), "unrelated") {
				t.Fatal("unrequested benefits leaked")
			}
			var stored struct {
				Benefits []struct {
					Codes []string `json:"serviceTypeCodes"`
				} `json:"benefitsInformation"`
				Metadata json.Number `json:"futureMetadata"`
			}
			if err := json.Unmarshal(result.ProviderResponse, &stored); err != nil {
				t.Fatal(err)
			}
			want, count := "98", 2
			if coverage == "routine_vision" {
				want, count = "AL", 3
			}
			if len(stored.Benefits) != count || stored.Metadata != "9007199254740993" {
				t.Fatalf("lost evidence: %+v", stored)
			}
			for _, b := range stored.Benefits {
				if len(b.Codes) != 1 || (b.Codes[0] != "30" && b.Codes[0] != want) {
					t.Fatalf("wrong service: %+v", b)
				}
			}
			if coverage == "routine_vision" && (!strings.Contains(string(encoded), "VISION EXAM") || !strings.Contains(string(encoded), `"benefitAmount":"0.00"`)) {
				t.Fatal("lost zero copay or qualifier")
			}
		})
	}
}
