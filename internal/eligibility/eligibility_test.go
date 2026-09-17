package eligibility

import (
	"encoding/json"
	"slices"
	"testing"
)

func example() Person {
	return Person{FirstName: "Mara", LastName: "Peters", DateOfBirth: "19800512", MemberID: "EXAMPLE-42", Address: Address{Address1: "12 Maple Street", State: "FL", PostalCode: "34608"}}
}

func TestIdentityEvidence(t *testing.T) {
	for _, tc := range []struct {
		name           string
		modify         func(*Person)
		status, reason string
	}{
		{"case normalization", func(p *Person) { p.FirstName = "MARA" }, "exact_name_dob", ""},
		{"one letter despite matching address and member", func(p *Person) { p.FirstName = "Sara" }, "identity_conflict", "first_name_conflict"},
		{"different last name", func(p *Person) { p.LastName = "Peeters" }, "identity_conflict", "last_name_conflict"},
		{"wrong DOB", func(p *Person) { p.DateOfBirth = "19800513" }, "identity_conflict", "dob_conflict"},
		{"missing DOB", func(p *Person) { p.DateOfBirth = "" }, "insufficient_data", "missing_or_invalid_identity"},
		{"missing name", func(p *Person) { p.FirstName = "" }, "insufficient_data", "missing_or_invalid_identity"},
		{"changed member ID remains visible", func(p *Person) { p.MemberID = "payer-id" }, "exact_name_dob", "member_id_changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := example()
			returned := expected
			tc.modify(&returned)
			result := Match(expected, returned)
			if result.Status != tc.status || result.ReviewRequired != (tc.status != "exact_name_dob") {
				t.Fatalf("unexpected result: %+v", result)
			}
			if tc.reason != "" && !slices.Contains(result.Reasons, tc.reason) {
				t.Fatalf("missing reason: %+v", result)
			}
		})
	}
	expected, returned := example(), example()
	expected.FirstName = "Mara Ann"
	returned.MiddleName = "Ann"
	if Match(expected, returned).ReviewRequired {
		t.Fatal("explicit middle name should preserve exact agreement")
	}
	returned.MiddleName = ""
	if !Match(expected, returned).ReviewRequired {
		t.Fatal("must not guess middle names")
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

func TestNextRetryOrderingDeduplicationAndBudget(t *testing.T) {
	base := Request{Payer: "EXAMPLE", Subscriber: example(), Provider: Provider{NPI: "1999999984"}, Encounter: Encounter{ServiceTypeCodes: []string{"30"}}}
	names := []RecordedName{{First: "Mara", Last: "Peters", Source: "intake"}, {First: "Mara", Last: "Peters Stone", Source: "chart"}, {First: "Invented", Last: "Person", Source: "guess"}}
	prior := []Request{base}
	for i, want := range []struct{ last, member string }{{"Peters Stone", base.Subscriber.MemberID}, {"Peters", ""}, {"Peters Stone", ""}} {
		next, reason := NextRetry(base, names, prior, []string{"75"}, true)
		if next == nil || reason != "recorded_name_recovery" {
			t.Fatalf("step %d: %s", i, reason)
		}
		if next.Subscriber.LastName != want.last || next.Subscriber.MemberID != want.member || next.Subscriber.DateOfBirth != base.Subscriber.DateOfBirth {
			t.Fatalf("wrong next request: %+v", next)
		}
		for _, previous := range prior {
			if Fingerprint(*next) == Fingerprint(previous) {
				t.Fatal("duplicate retry")
			}
		}
		prior = append(prior, *next)
	}
	if next, reason := NextRetry(base, names, prior, []string{"75"}, true); next != nil || reason != "attempt_limit" {
		t.Fatal("must stop after four total sends")
	}
	if next, reason := NextRetry(base, names, prior[:2], []string{"73"}, true); next != nil || reason != "previously_exhausted" {
		t.Fatal("name-only rejection must not omit member ID")
	}
	for _, code := range []string{"51", "41", "79", "42", "58", "71"} {
		if next, _ := NextRetry(base, names, nil, []string{code}, true); next != nil {
			t.Fatalf("unexpected retry for %s", code)
		}
	}
	if next, reason := NextRetry(base, names, nil, []string{"75"}, false); next != nil || reason != "payer_unsupported" {
		t.Fatal("unsupported payer retried")
	}
	base.Dependents = []Person{example()}
	if next, reason := NextRetry(base, names, nil, []string{"75"}, true); next != nil || reason != "dependent_review" {
		t.Fatal("dependent retried")
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
		if (assessment.NextRequest != nil) != (used < 4) {
			t.Fatalf("prior=%d unexpected next request: %+v", count, assessment.NextRequest)
		}
		if used >= 4 && assessment.RetryReason != "attempt_limit" {
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
