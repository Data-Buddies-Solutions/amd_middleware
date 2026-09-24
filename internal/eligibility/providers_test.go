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
	providers, _ := checkedProviders("spring_hill", springHillMedicalProviders)
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

func TestSweetwaterVisionChecksThreeDoctors(t *testing.T) {
	for _, failFarnan := range []bool{false, true} {
		t.Run(fmt.Sprintf("failFarnan=%t", failFarnan), func(t *testing.T) {
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
				if failFarnan && request.Provider.NPI == "1568198158" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				fmt.Fprint(w, fixture(`[{"code":"1","serviceTypeCodes":["30"]},{"code":"B","serviceTypeCodes":["AL"],"benefitAmount":"20"},{"code":"B","serviceTypeCodes":["98"],"benefitAmount":"35"}]`, ""))
			})
			s.providers = nil
			in := input()
			in.Plan, in.CoverageType = "Davis Vision", "routine_vision"
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := s.Check(ctx, "sweetwater", in)
			if err != nil || len(got.ProviderResults) != 3 {
				t.Fatalf("missing optical checks: %+v err=%v", got, err)
			}
			if failFarnan {
				if got.Status != "review" || got.ReviewReason != "provider_results_need_review" || got.MatchedPatient != nil || got.ProviderResults[1].Status != "unknown" {
					t.Fatalf("unsafe partial result: %+v", got)
				}
			} else if got.Status != "active" || got.MatchedPatient == nil {
				t.Fatalf("missing consensus: %+v", got)
			}
			want := []CheckedProvider{
				{ProfileID: "1996", Name: "Dr. Maria Casas", FirstName: "Maria", LastName: "Casas", NPI: "1851438519"},
				{ProfileID: "2075", Name: "Dr. Kyler Farnan", FirstName: "Kyler", LastName: "Farnan", NPI: "1568198158"},
				{ProfileID: "1993", Name: "Dr. Gisselle Calero", FirstName: "Gisselle", LastName: "Calero", NPI: "1619592607"},
			}
			for i, provider := range want {
				child := got.ProviderResults[i]
				request := requests[provider.NPI]
				if child.Provider == nil || *child.Provider != provider || request.Provider.FirstName != provider.FirstName || request.Provider.LastName != provider.LastName || request.Provider.OrganizationName != "" || request.Payer != "00157" {
					t.Fatalf("wrong optical provider/request: %+v %+v", child.Provider, request)
				}
				if failFarnan && i == 1 {
					continue
				}
				var raw map[string]any
				if child.Status != "active" || json.Unmarshal(child.ProviderResponse, &raw) != nil || len(raw["benefitsInformation"].([]any)) != 2 {
					t.Fatalf("optical benefit evidence missing: %+v", child)
				}
			}
		})
	}
}

func TestSpringHillVisionUsesOtero(t *testing.T) {
	calls := 0
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request Request
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Provider.OrganizationName != "" || request.Provider.FirstName != "Melissa" || request.Provider.LastName != "Otero" || request.Provider.NPI != "1457904765" {
			t.Error("routine vision must use Otero")
		}
		fmt.Fprint(w, fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, ""))
	})
	s.providers["spring_hill"] = s.providers["office"]
	in := input()
	in.CoverageType = "routine_vision"
	in.Plan = "Davis Vision"
	got, err := s.Check(context.Background(), "spring_hill", in)
	if err != nil || got.Status != "active" || calls != 1 || len(got.ProviderResults) != 1 || got.ProviderResults[0].Provider.ProfileID != "1983" {
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
		providers, ok := checkedProviders("spring_hill", springHillMedicalProviders)
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
