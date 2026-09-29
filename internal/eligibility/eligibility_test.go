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

func assess(t *testing.T, body string) responseAssessment {
	t.Helper()
	var response Response
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatal(err)
	}
	return assessResponse(response, example(), false)
}

func TestFamilyAndErrorResponsesStayUnverified(t *testing.T) {
	a := assess(t, `{"subscriber":{"firstName":"Mara","lastName":"Peters","dateOfBirth":"19800512"},"errors":[{"code":"73"}],"benefitsInformation":[{"code":"1"}]}`)
	if a.Coverage == "active" || a.Match.Status != "payer_rejected" {
		t.Fatalf("error accepted: %+v", a)
	}
	a = assess(t, `{"dependents":[{},{}],"benefitsInformation":[{"code":"1"}]}`)
	if a.Match.Status != "ambiguous_dependents" || !a.Match.ReviewRequired {
		t.Fatalf("ambiguous family accepted: %+v", a)
	}
}

func TestUnknownResponsesAreUnrecognized(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"unexpected":true}`} {
		if assess(t, body).Recognized {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestMiddleNameRequiresSeparateToken(t *testing.T) {
	returned := example()
	returned.FirstName, returned.MiddleName = "Anna", "Belle"
	for _, first := range []string{"Annabelle", "Anna Belle"} {
		expected := returned
		expected.FirstName = first
		expected.MiddleName = ""
		got := Match(expected, returned)
		if got.ReviewRequired != (first == "Annabelle") {
			t.Fatalf("%s: %+v", first, got)
		}
	}
}

func TestSupportedNameCorrection(t *testing.T) {
	expected := Person{FirstName: "Avery", LastName: "Stone Jr.", DateOfBirth: "19800512", MemberID: "EXAMPLE-42"}
	returned := Person{FirstName: "Mavery", LastName: "Stone", DateOfBirth: "19800512", MemberID: "EXAMPLE-42"}
	for _, tc := range []struct {
		name   string
		change func(*Person)
		want   bool
	}{
		{"missing initial and suffix", func(*Person) {}, true},
		{"member conflict", func(p *Person) { p.MemberID = "other" }, false},
		{"missing member", func(p *Person) { p.MemberID = "" }, false},
		{"DOB conflict", func(p *Person) { p.DateOfBirth = "19800513" }, false},
		{"different first", func(p *Person) { p.FirstName = "Oliver" }, false},
		{"different last", func(p *Person) { p.LastName = "Jones" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := returned
			tc.change(&p)
			got := Match(expected, p)
			if (got.Status == "matched_with_name_correction" && !got.ReviewRequired) != tc.want {
				t.Fatalf("unexpected correction: %+v", got)
			}
		})
	}
}
