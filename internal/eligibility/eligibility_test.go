package eligibility

import (
	"encoding/json"
	"testing"
)

func example() Person {
	return Person{FirstName: "Mara", LastName: "Peters", DateOfBirth: "19800512", MemberID: "EXAMPLE-42", Address: Address{Address1: "12 Maple Street", State: "FL", PostalCode: "34608"}}
}

func TestIdentityEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*Person)
		want   string
	}{
		{"exact", func(p *Person) { p.FirstName = "MARA" }, "exact_name_dob"},
		{"corroborated spelling", func(p *Person) { p.FirstName = "Sara"; p.Address.Address1 = "12 MAPLE ST" }, "corroborated_variant"},
		{"wrong DOB", func(p *Person) { p.DateOfBirth = "19800513" }, "identity_conflict"},
		{"missing DOB", func(p *Person) { p.DateOfBirth = "" }, "insufficient_data"},
		{"wrong address", func(p *Person) { p.LastName = "Peeters"; p.Address.Address1 = "99 Elm Rd" }, "near_name"},
		{"two differences", func(p *Person) { p.FirstName = "Sara"; p.LastName = "Peeters" }, "identity_conflict"},
		{"missing name", func(p *Person) { p.FirstName = "" }, "insufficient_data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := example()
			b := a
			tc.modify(&b)
			r := Match(a, b, b.MemberID)
			if r.Status != tc.want {
				t.Fatalf("got %s want %s", r.Status, tc.want)
			}
			if r.Status != "exact_name_dob" && !r.ReviewRequired {
				t.Fatal("fuzzy result must require review")
			}
		})
	}
}

func TestShortDependentNameIsNotAutoAccepted(t *testing.T) {
	a := example()
	a.FirstName = "Amy"
	b := a
	b.FirstName = "Emy"
	b.MemberID = ""
	b.Address.Address1 = "12 Maple St"
	r := Match(a, b, a.MemberID)
	if r.Status != "corroborated_variant" || !r.ReviewRequired || r.MemberMatch || !r.PolicyMatch {
		t.Fatalf("unexpected evidence: %+v", r)
	}
}

func TestNoEmptyOrApartmentCorroboration(t *testing.T) {
	a := example()
	b := a
	b.LastName = "Peeters"
	b.MemberID = ""
	b.Address = Address{}
	if r := Match(a, b, ""); r.Status != "near_name" || r.AddressMatch || r.PolicyMatch {
		t.Fatalf("empty data corroborated: %+v", r)
	}
	b = a
	b.FirstName = "Sara"
	a.Address.Address2 = "Apt 1"
	b.Address.Address2 = "Apt 2"
	if r := Match(a, b, b.MemberID); r.AddressMatch || !r.AddressConflict || r.Status != "near_name" {
		t.Fatalf("unit conflict lost: %+v", r)
	}
}

func TestFamilyAndErrorResponsesStayUnverified(t *testing.T) {
	p := example()
	base := Case{Expected: p, Source: "eligibility", Response: json.RawMessage(`{"subscriber":{"firstName":"Mara","lastName":"Peters","dateOfBirth":"19800512"},"errors":[{"code":"73"}],"benefitsInformation":[{"code":"1"}]}`)}
	a, err := Assess(base)
	if err != nil || a.ActiveResponse || a.Match.Status != "payer_rejected" {
		t.Fatalf("error accepted: %+v %v", a, err)
	}
	base.Response = json.RawMessage(`{"dependents":[{},{}],"benefitsInformation":[{"code":"1"}]}`)
	a, err = Assess(base)
	if err != nil || a.Match.Status != "ambiguous_dependents" || !a.Match.ReviewRequired {
		t.Fatalf("ambiguous family accepted: %+v", a)
	}
	base.Response = json.RawMessage(`{"subscriber":{"firstName":"Mara","lastName":"Peters","dateOfBirth":"19800512"},"benefitsInformation":[{"code":"1"}]}`)
	base.Source = "discovery"
	a, err = Assess(base)
	if err != nil || a.Match.Status != "discovery_review" || !a.Match.ReviewRequired {
		t.Fatal("discovery autoaccepted")
	}
}

func TestRetryBudgetProvenanceDeduplication(t *testing.T) {
	base := Request{Payer: "EXAMPLE", Subscriber: example(), Provider: Provider{NPI: "1234567890"}, Encounter: Encounter{ServiceTypeCodes: []string{"30"}}}
	names := []RecordedName{{First: "Mara", Last: "Peters", Source: "intake"}, {First: "Mara", Last: "Peters Stone", Source: "chart"}, {First: "Invented", Last: "Person", Source: "guess"}}
	p := PlanRetries(base, names, []Request{base}, []string{"73"}, true)
	if len(p.Attempts) != 1 {
		t.Fatalf("expected only the complete recorded chart name, got %d", len(p.Attempts))
	}
	seen := map[string]bool{Fingerprint(base): true}
	for _, a := range p.Attempts {
		k := Fingerprint(a.Request)
		if seen[k] {
			t.Fatal("duplicate")
		}
		seen[k] = true
		if a.Request.Subscriber.FirstName == "Invented" {
			t.Fatal("untrusted name")
		}
		if a.Request.Subscriber.DateOfBirth != base.Subscriber.DateOfBirth {
			t.Fatal("DOB mutated")
		}
		if a.Request.Subscriber.MemberID != "" && a.Request.Subscriber.MemberID != base.Subscriber.MemberID {
			t.Fatal("member ID invented")
		}
	}
	for _, code := range []string{"51", "41", "79", "42", "58", "71"} {
		if q := PlanRetries(base, names, nil, []string{code}, true); len(q.Attempts) != 0 {
			t.Fatalf("permuted non-identity error %s", code)
		}
	}

	if q := PlanRetries(base, names, nil, []string{"73"}, false); q.Reason != "payer_unsupported" {
		t.Fatal("unsupported payer retried")
	}
	base.Dependents = []Person{example()}
	if q := PlanRetries(base, names, nil, []string{"73"}, true); len(q.Attempts) > 0 {
		t.Fatal("dependent guessed")
	}
}

func TestRecordedNamesBeforeOmissions(t *testing.T) {
	base := Request{Subscriber: example()}
	p := PlanRetries(base, []RecordedName{{First: "Mara", Last: "Peters", Source: "intake"}, {First: "Mara", Last: "Peters Stone", Source: "chart"}}, nil, []string{"73"}, true)
	if len(p.Attempts) != 1 || p.Attempts[0].Label != "chart_name" {
		t.Fatalf("wrong priority: %+v", p)
	}
}

func TestUnknownResponsesAreErrors(t *testing.T) {
	if _, err := Assess(Case{Source: "unknown", Response: json.RawMessage(`{"errors":[{"code":"73"}]}`)}); err == nil {
		t.Fatal("unknown source accepted")
	}
	for _, body := range []string{`null`, `{}`, `{"unexpected":true}`} {
		if _, err := Assess(Case{Source: "eligibility", Response: json.RawMessage(body)}); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestRequestOmitsEmptyAddress(t *testing.T) {
	b, _ := json.Marshal(Person{FirstName: "Mara"})
	var fields map[string]any
	_ = json.Unmarshal(b, &fields)
	if _, ok := fields["address"]; ok {
		t.Fatal("empty address would violate payer request contract")
	}
}

func TestReplaySharesTotalAttemptBudget(t *testing.T) {
	base := Request{Payer: "SYNTHETIC", Subscriber: example(), Encounter: Encounter{ServiceTypeCodes: []string{"30"}}}
	names := []RecordedName{{First: "Maria", Last: "Peters", Source: "chart"}, {First: "Mary", Last: "Peters", Source: "intake"}, {First: "Marie", Last: "Peters", Source: "chart"}, {First: "Martha", Last: "Peters", Source: "chart"}}
	for _, count := range []int{0, 1, 3, 4, 5} {
		prior := []Request{}
		for i := 0; i < count; i++ {
			prior = append(prior, base)
		}
		assessment, err := Assess(Case{Expected: base.Subscriber, Base: base, Prior: prior, RecordedNames: names, Supported: true, Source: "eligibility", Response: json.RawMessage(`{"errors":[{"code":"73"}]}`)})
		if err != nil {
			t.Fatal(err)
		}
		// Even duplicate prior entries represent sends and consume the budget. An
		// omitted base still counts because it generated the rejection being replayed.
		used := max(count, 1)
		remaining := max(4-used, 0)
		if len(assessment.RetryPlan.Attempts) != remaining {
			t.Fatalf("prior=%d got %d retries, want %d", count, len(assessment.RetryPlan.Attempts), remaining)
		}
		if remaining == 0 && assessment.RetryPlan.Reason != "attempt_limit" {
			t.Fatal("exhaustion must be explicit")
		}
	}
}

func TestTypedRequestFingerprintAndPrivateReplayFields(t *testing.T) {
	var a, b Request
	if err := json.Unmarshal([]byte(`{"provider":{"firstName":"Sam","lastName":"Provider","npi":"1999999984"},"subscriber":{"firstName":"Jane","lastName":"Sample"},"encounter":{"serviceTypeCodes":["30"],"dateOfService":"20260916"}}`), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"encounter":{"dateOfService":"20260916","serviceTypeCodes":["30"]},"subscriber":{"lastName":"SAMPLE","firstName":"JANE"},"provider":{"npi":"1999999984","lastName":"Provider","firstName":"Sam"},"eligibilitySearchId":"chain"}`), &b); err != nil {
		t.Fatal(err)
	}
	if a.Provider.FirstName != "Sam" || a.Provider.LastName != "Provider" || Fingerprint(a) != Fingerprint(b) {
		t.Fatal("typed replay lost provider or cosmetic stability")
	}
	b.Encounter.DateOfService = "20260917"
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatal("service date must bind request identity")
	}
	b = a
	b.Provider.NPI = "1659862753"
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatal("provider must bind request identity")
	}
}
