package insurance

import (
	"reflect"
	"slices"
	"testing"

	"advancedmd-token-management/internal/domain"
)

func TestSandboxSpringHillListsItsPlansWithoutOfficesMissingFromTheSandbox(t *testing.T) {
	prodList, _ := ListPlans("medical", office(t, "spring_hill"))
	prodVisionList, _ := ListPlans("routine_vision", office(t, "spring_hill"))
	prodMedical, prodVision := prodList.Plans, prodVisionList.Plans
	if len(prodMedical) == 0 || len(prodVision) == 0 {
		t.Fatalf("production Spring Hill lists %d medical and %d routine vision plans, want both", len(prodMedical), len(prodVision))
	}
	acceptedAtCrystalRiver := 0
	for i := range prodMedical {
		if slices.Contains(prodMedical[i].AcceptedAt, "Crystal River") {
			acceptedAtCrystalRiver++
		}
		prodMedical[i].AcceptedAt = slices.DeleteFunc(prodMedical[i].AcceptedAt, func(name string) bool { return name == "Crystal River" })
	}
	if acceptedAtCrystalRiver == 0 {
		t.Fatal("no production Spring Hill medical plan points callers to Crystal River, so the sandbox case proves nothing")
	}

	t.Cleanup(func() { domain.InitRegistry(domain.Prod) })
	domain.InitRegistry(domain.Sandbox)
	springHill := office(t, "spring_hill")

	medicalList, ok := ListPlans("medical", springHill)
	medical := medicalList.Plans
	if !ok {
		t.Fatal("sandbox Spring Hill rejected medical coverage")
	}
	if !reflect.DeepEqual(medical, prodMedical) {
		t.Fatalf("sandbox Spring Hill medical plans differ from production beyond Crystal River:\n got %+v\nwant %+v", medical, prodMedical)
	}

	visionList, ok := ListPlans("routine_vision", springHill)
	vision := visionList.Plans
	if !ok || len(vision) != 0 {
		t.Fatalf("sandbox Spring Hill routine vision = (%d plans, %v), want none because the sandbox has no optical provider", len(vision), ok)
	}
}

func TestEveryCatalogOfficeIsServedOrDeclinedInEachEnvironment(t *testing.T) {
	t.Cleanup(func() { domain.InitRegistry(domain.Prod) })
	for _, env := range []domain.Environment{domain.Prod, domain.Sandbox} {
		domain.InitRegistry(env)
		served := map[string]bool{}
		for _, list := range active.Load().lists {
			for _, officeID := range list.Offices {
				if o, ok := domain.LookupOfficeByID(officeID); ok {
					served[o.DisplayName] = true
				}
			}
		}
		for _, list := range active.Load().lists {
			for _, officeID := range list.Offices {
				o, ok := domain.LookupOfficeByID(officeID)
				if !ok {
					if _, inTable := domain.TableOfficeProviderNames(officeID); !inTable {
						t.Errorf("%s: catalog office %s is not in the office table", env, officeID)
					}
					continue
				}
				doctors := map[string]bool{}
				for _, column := range o.Columns {
					doctors[column.ShortName] = true
				}
				for _, coverage := range []string{"medical", "routine_vision"} {
					listed, ok := ListPlans(coverage, o)
					if !ok {
						t.Fatalf("%s: %s rejected %s", env, officeID, coverage)
					}
					for _, s := range listed.Plans {
						for _, provider := range s.AllowedProviders {
							if !doctors[provider] {
								t.Errorf("%s: %s %s allows %s, who has no column there", env, officeID, s.PlanID, provider)
							}
						}
						for _, name := range s.AcceptedAt {
							if !served[name] {
								t.Errorf("%s: %s %s points callers to %s, which this environment does not serve", env, officeID, s.PlanID, name)
							}
						}
					}
					for _, p := range list.Plans {
						for _, dob := range []string{adultDOB, dobYearsAgo(8)} {
							for _, provider := range DecidePlan(p.ID, coverage, o, dob, testNow).AllowedProviders {
								if !doctors[provider] {
									t.Errorf("%s: %s %s decision allows %s, who has no column there", env, officeID, p.ID, provider)
								}
							}
						}
					}
				}
			}
		}
	}
}
