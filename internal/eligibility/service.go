package eligibility

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"advancedmd-token-management/internal/domain"
)

const endpoint = "https://healthcare.us.stedi.com/2024-04-01/change/medicalnetwork/eligibility/v3"

type Provider struct {
	OrganizationName string `json:"organizationName,omitempty"`
	FirstName        string `json:"firstName,omitempty"`
	LastName         string `json:"lastName,omitempty"`
	NPI              string `json:"npi"`
}

// CheckInput is collected during intake, before registration or booking.
type CheckInput struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	DOB          string `json:"dob"`
	MemberID     string `json:"memberId"`
	Plan         string `json:"plan"`
	CoverageType string `json:"coverageType,omitempty"`
}

// Result reports current STC 30 plan activity, not network participation, visit
// coverage or copay. Review/unknown never establish that the patient is uninsured.
// ProviderResponse preserves all returned evidence independently of that assessment.
type Result struct {
	InsuranceResolution *InsuranceResolution `json:"insuranceResolution,omitempty"`
	Provider            *CheckedProvider     `json:"provider,omitempty"`
	ProviderResults     []Result             `json:"providerResults,omitempty"`
	MatchedPatient      *Person              `json:"matchedPatient,omitempty"`
	ProviderResponse    json.RawMessage      `json:"providerResponse,omitempty"`
	ProviderHTTPStatus  int                  `json:"providerHttpStatus,omitempty"`
	Status              string               `json:"status"` // active, inactive, review, unknown
	OfficeID            string               `json:"officeId"`
	PayerID             string               `json:"payerId,omitempty"`
	ReviewReason        string               `json:"reviewReason,omitempty"`
	SearchID            string               `json:"eligibilitySearchId,omitempty"`
	CheckID             string               `json:"checkId,omitempty"`
	ErrorCodes          []string             `json:"errorCodes,omitempty"`
	Match               *MatchResult         `json:"identity,omitempty"`
	CheckedAt           time.Time            `json:"checkedAt"`
}

type Service struct {
	key       string
	providers map[string]Provider
	client    *http.Client
	url       string
	now       func() time.Time
}

func New(key string, providers map[string]Provider) (*Service, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("eligibility_configuration_required")
	}
	copyProviders := make(map[string]Provider, len(providers))
	for office, p := range providers {
		if office == "" || strings.TrimSpace(p.OrganizationName) == "" || !validNPI(p.NPI) {
			return nil, errors.New("invalid_eligibility_provider")
		}
		copyProviders[office] = p
	}
	return &Service{key: key, providers: copyProviders, url: endpoint, now: time.Now, client: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func validNPI(n string) bool {
	if len(n) != 10 {
		return false
	}
	sum := 24 // Luhn prefix 80840
	for i, c := range n {
		if c < '0' || c > '9' {
			return false
		}
		v := int(c - '0')
		if i%2 == 0 {
			v *= 2
			if v > 9 {
				v -= 9
			}
		}
		sum += v
	}
	return sum%10 == 0
}
func validPerson(p Person) bool {
	return name(p.FirstName) != "" && name(p.LastName) != "" && validDOB(p.DateOfBirth)
}

// Check performs one intake check per relevant provider. OfficeID comes from the authenticated
// adapter's existing office resolution. There is no automatic retry or durable
// job requirement; an unknown outcome must not be blindly resubmitted.
func (s *Service) Check(ctx context.Context, officeID string, in CheckInput) (Result, error) {
	dob := strings.TrimSpace(in.DOB)
	if !validDOB(dob) {
		if parsed, err := time.Parse("01/02/2006", domain.NormalizeDOB(dob)); err == nil {
			dob = parsed.Format("20060102")
		}
	}
	patient := Person{FirstName: strings.TrimSpace(in.FirstName), LastName: strings.TrimSpace(in.LastName), DateOfBirth: dob, MemberID: strings.TrimSpace(in.MemberID)}
	if !validPerson(patient) || patient.MemberID == "" || strings.TrimSpace(in.Plan) == "" || (in.CoverageType != "" && in.CoverageType != "medical" && in.CoverageType != "routine_vision") {
		return Result{}, errors.New("invalid_eligibility_input")
	}
	now := s.now()
	payer, reason := Route(in.Plan, now.In(domain.EasternLocation()).Format("20060102"))
	// Oscar's directory route covers medical/dental; its optional Davis product
	// must be identified separately for routine vision.
	if in.CoverageType == "routine_vision" && payer == "OSCAR" {
		reason = "vision_product_required"
	}
	out := Result{OfficeID: officeID, PayerID: payer, Status: "review", ReviewReason: reason, CheckedAt: now.UTC()}
	if reason != "" {
		return out, nil
	}
	if (officeID == "spring_hill" && in.CoverageType != "routine_vision") || officeID == "crystal_river" || (officeID == "north_miami_beach_optical" && in.CoverageType == "routine_vision") {
		var providers []CheckedProvider
		var ok bool
		if officeID == "north_miami_beach_optical" {
			providers, ok = checkedProviders(officeID, []CheckedProvider{miriamBachProvider})
		} else if officeID == "crystal_river" {
			providers, ok = checkedProviders(officeID, []CheckedProvider{lichtProvider})
		} else {
			providers, ok = medicalProviders()
		}
		if !ok {
			out.ReviewReason = "provider_profile_unavailable"
			return out, nil
		}
		out.ProviderResults = make([]Result, len(providers))
		var pending sync.WaitGroup
		for i, provider := range providers {
			pending.Add(1)
			go func() {
				defer pending.Done()
				child := out
				child.ProviderResults = nil
				child.Provider = &provider
				out.ProviderResults[i] = s.checkProvider(ctx, child, patient, Provider{FirstName: provider.FirstName, LastName: provider.LastName, NPI: provider.NPI})
			}()
		}
		pending.Wait()
		return providerConsensus(out), nil
	}
	provider, ok := s.providers[officeID]

	if !ok {
		out.ReviewReason = "provider_not_configured"
		return out, nil
	}
	return s.checkProvider(ctx, out, patient, provider), nil
}

func (s *Service) checkProvider(ctx context.Context, out Result, patient Person, provider Provider) Result {
	// Supply the patient's actual demographics as the initial lookup. Do not
	// manufacture a policyholder or dependent relationship when it is unknown.
	// Stedi recommends omitting the service date for a current-date check.
	reqBody := Request{Payer: out.PayerID, Provider: provider, Subscriber: patient, Encounter: Encounter{ServiceTypeCodes: []string{"30"}}}
	out.Status = "unknown"
	out.ReviewReason = "request_outcome_unknown"
	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(data))
	if err != nil {
		return out
	}
	req.Header.Set("Authorization", s.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	out.ProviderHTTPStatus = resp.StatusCode
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
	if err != nil || len(raw) > 8*1024*1024 {
		return out
	}
	// Keep the original JSON without decoding numbers through float64. Invalid
	// JSON is retained as a JSON string, but can never establish coverage.
	if json.Valid(raw) {
		out.ProviderResponse = json.RawMessage(raw)
	} else {
		out.ProviderResponse, _ = json.Marshal(string(raw))
	}
	if resp.StatusCode != http.StatusOK {
		out.ReviewReason = "stedi_http_failure"
		return out
	}
	var response Response
	if json.Unmarshal(raw, &response) != nil {
		out.ReviewReason = "unrecognized_response"
		return out
	}
	out.SearchID, out.CheckID = response.SearchID, response.ID
	assessment := assessResponse(response, patient, false)
	if !assessment.Recognized {
		out.ReviewReason = "unrecognized_response"
		return out
	}
	out.Match = &assessment.Match
	if response.Meta.Mode != "production" {
		out.ReviewReason = "nonproduction_or_unknown_mode"
		return out
	}
	out.ErrorCodes = assessment.ErrorCodes
	if len(out.ErrorCodes) > 0 {
		out.Status = "review"
		out.ReviewReason = "payer_rejected"
		return out
	}
	out.Status = "review"
	out.ReviewReason = "identity_uncertain"
	if assessment.Match.ReviewRequired {
		if len(response.Dependents) > 0 {
			out.ReviewReason = "subscriber_details_required"
		}
		return out
	}
	// Only expose an actionable name after production, payer, and identity checks.
	matched := response.Subscriber
	out.MatchedPatient = &matched
	if assessment.Coverage == "unknown" {
		out.ReviewReason = "general_coverage_unknown"
		return out
	}
	out.Status = assessment.Coverage
	out.ReviewReason = ""
	return out
}
