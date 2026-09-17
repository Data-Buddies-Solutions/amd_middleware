package eligibility

import (
	"encoding/json"
	"slices"
	"testing"
)

func example() Person {
	return Person{FirstName: "Mara", LastName: "Peters", DateOfBirth: "19800512", MemberID: "EXAMPLE-42"}
}

func TestIdentityEvidence(t *testing.T) {
	for _, tc := range []struct {
		name           string
		modify         func(*Person)
		status, reason string
	}{
		{"case normalization", func(p *Person) { p.FirstName = "MARA" }, "exact_name_dob", ""},
		{"one letter despite matching member", func(p *Person) { p.FirstName = "Sara" }, "identity_conflict", "first_name_conflict"},
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
