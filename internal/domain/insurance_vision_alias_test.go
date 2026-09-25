package domain

import "testing"

// Python 9cc440a's match_plan with its original vision JSON accepts these
// canonical labels (also when prefixed with "I have"). Both attach to Davis.
func TestVisionAliasMigrationPreservesOriginalAcceptance(t *testing.T) {
	office, _ := ResolveOffice("Spring Hill")
	for _, name := range []string{"Superior", "Versant"} {
		for _, query := range []string{name, "I have " + name} {
			t.Run(query, func(t *testing.T) {
				rule := participationMatch("SPRING_HILL_ROUTINE_VISION", query)
				if rule == nil || rule.Status != "accepted" || rule.Canonical != name {
					t.Fatalf("original Python accepted %q as %q; got %+v", query, name, rule)
				}
				decision := DecideInsurance(query, "routine_vision", office, "01/02/1980")
				if decision.Participation != "accepted" || !decision.CanSchedule || decision.CarrierID != "car280612" || decision.Routing != RoutingOpticalOnly {
					t.Fatalf("vision alias no longer preserves Davis acceptance: %+v", decision)
				}
			})
		}
	}
}

func TestVisionEquivalentCarrierDoesNotResolveDifferentProducts(t *testing.T) {
	for _, query := range []string{"Superior or Versant", "Superior or Davis", "Versant or VSP"} {
		if rule := participationMatch("SPRING_HILL_ROUTINE_VISION", query); rule != nil {
			t.Errorf("different products %q resolved to %+v", query, rule)
		}
	}
}
