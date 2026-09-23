package eligibility

import (
	"advancedmd-token-management/internal/domain"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestSpringHillMedicalChecksThreeDoctorsConcurrently(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]Request{}
	allStarted := make(chan struct{})
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		var request Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		requests[request.Provider.NPI] = request
		if len(requests) == 3 {
			close(allStarted)
		}
		mu.Unlock()
		select {
		case <-allStarted:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, fixture(`[{"code":"1","serviceTypeCodes":["30"]},{"code":"B","serviceTypeCodes":["98"],"benefitAmount":"35"}]`, `,"futureProviderField":"`+request.Provider.NPI+`"`))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	in := input()
	in.FirstName = "Ane"
	got, err := s.Check(ctx, "spring_hill", in)
	if err != nil || got.Status != "active" || got.Match == nil || got.Match.Status != "matched_with_name_correction" || got.MatchedPatient.FirstName != "Jane" {
		t.Fatalf("missing consensus: %+v %v", got, err)
	}
	if len(got.ProviderResults) != 3 || len(got.ProviderResponse) != 0 {
		t.Fatal("must keep three responses without duplicating provider evidence")
	}
	providers, _ := medicalProviders()
	for i, child := range got.ProviderResults {
		provider := providers[i]
		request := requests[provider.NPI]
		if child.Provider == nil || *child.Provider != provider || !validNPI(provider.NPI) ||
			request.Provider.FirstName != provider.FirstName || request.Provider.LastName != provider.LastName || request.Provider.OrganizationName != "" ||
			len(request.Encounter.ServiceTypeCodes) != 1 || request.Encounter.ServiceTypeCodes[0] != "30" {
			t.Fatalf("wrong provider/request: %+v %+v", child.Provider, request)
		}
		var raw map[string]any
		if json.Unmarshal(child.ProviderResponse, &raw) != nil || raw["futureProviderField"] != provider.NPI || len(raw["benefitsInformation"].([]any)) != 2 {
			t.Fatal("provider evidence lost")
		}
	}
}

func TestSpringHillMedicalRetainsPartialFailuresAndConflicts(t *testing.T) {
	for _, scenario := range []string{"http", "identity", "status", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			s := testService(t, func(w http.ResponseWriter, r *http.Request) {
				var request Request
				_ = json.NewDecoder(r.Body).Decode(&request)
				body := fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, "")
				if request.Provider.NPI == springHillMedicalProviders[1].NPI {
					switch scenario {
					case "http":
						w.WriteHeader(503)
						body = `{"error":"synthetic failure"}`
					case "identity":
						body = `{"meta":{"applicationMode":"production"},"subscriber":{"firstName":"Different","lastName":"Person","dateOfBirth":"19800102"},"benefitsInformation":[{"code":"1","serviceTypeCodes":["30"]}]}`
					case "status":
						body = fixture(`[{"code":"6","serviceTypeCodes":["30"]}]`, "")
					case "timeout":
						<-r.Context().Done()
						return
					}
				}
				fmt.Fprint(w, body)
			})
			s.client.Timeout = 100 * time.Millisecond
			got, err := s.Check(context.Background(), "spring_hill", input())
			if err != nil || got.Status != "review" || got.Match != nil || got.MatchedPatient != nil || len(got.ProviderResults) != 3 {
				t.Fatalf("unsafe consensus: %+v %v", got, err)
			}
			if got.ProviderResults[0].Status != "active" || got.ProviderResults[2].Status != "active" || len(got.ProviderResults[0].ProviderResponse) == 0 {
				t.Fatal("partial failure erased successful provider checks")
			}
		})
	}
}

func TestSpringHillVisionStillUsesConfiguredProvider(t *testing.T) {
	calls := 0
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request Request
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Provider.OrganizationName != "Synthetic Practice" || request.Provider.FirstName != "" {
			t.Error("routine vision was sent as a medical doctor")
		}
		fmt.Fprint(w, fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, ""))
	})
	s.providers["spring_hill"] = s.providers["office"]
	in := input()
	in.CoverageType = "routine_vision"
	got, err := s.Check(context.Background(), "spring_hill", in)
	if err != nil || got.Status != "active" || calls != 1 || len(got.ProviderResults) != 0 {
		t.Fatalf("vision behavior changed: %+v %v", got, err)
	}
	if _, err := New("secret", nil); err != nil {
		t.Fatal("Spring Hill medical must not need an organization-NPI fallback")
	}
	in.CoverageType = "invalid"
	if _, err := s.Check(context.Background(), "spring_hill", in); err == nil || calls != 1 {
		t.Fatal("unknown coverage must not dispatch requests")
	}
}

func TestMedicalProvidersUseCurrentSchedulingRegistry(t *testing.T) {
	defer domain.InitRegistry("prod")
	for _, tc := range []struct {
		env string
		ids []string
	}{{"prod", []string{"620", "2064", "2076"}}, {"dev", []string{"1135", "1141", "1137"}}} {
		domain.InitRegistry(tc.env)
		providers, ok := medicalProviders()
		if !ok || len(providers) != 3 {
			t.Fatal("missing medical provider mapping")
		}
		for i, provider := range providers {
			if provider.ProfileID != tc.ids[i] {
				t.Fatalf("%s: wrong profile %+v", tc.env, provider)
			}
		}
	}
}
