package eligibility

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const endpoint = "https://healthcare.us.stedi.com/2024-04-01/change/medicalnetwork/eligibility/v3"

// Scope binds every attempt and result to one booked appointment and service day.
type Scope struct {
	OfficeID      string `json:"officeId"`
	PatientID     string `json:"patientId"`
	AppointmentID string `json:"appointmentId"`
	ServiceDate   string `json:"serviceDate"` // YYYYMMDD, appointment date in office timezone
}
type Provider struct {
	OrganizationName string `json:"organizationName,omitempty"`
	FirstName        string `json:"firstName,omitempty"`
	LastName         string `json:"lastName,omitempty"`
	NPI              string `json:"npi"`
}
type CheckInput struct {
	Scope         Scope          `json:"scope"`
	Plan          string         `json:"plan"`
	Subscriber    Person         `json:"subscriber"`
	Dependent     *Person        `json:"dependent,omitempty"`
	RecordedNames []RecordedName `json:"recordedNames,omitempty"`
	History       []Result       `json:"history,omitempty"`
}

// Result is the durable owner's receipt. Persist it before dispatching a retry.
// Coverage is STC 30 activity, never a network, visit coverage or copay decision.
type Result struct {
	Scope        Scope        `json:"scope"`
	PayerID      string       `json:"payerId,omitempty"`
	InputID      string       `json:"inputId"`
	Attempt      int          `json:"attempt"`
	Status       string       `json:"status"`
	Coverage     string       `json:"coverage"`
	ReviewReason string       `json:"reviewReason,omitempty"`
	Request      Request      `json:"request"`
	SearchID     string       `json:"eligibilitySearchId,omitempty"`
	CheckID      string       `json:"checkId,omitempty"`
	ErrorCodes   []string     `json:"errorCodes,omitempty"`
	Match        *MatchResult `json:"identity,omitempty"`
	CheckedAt    time.Time    `json:"checkedAt"`
}

type Service struct {
	key       string
	providers map[string]Provider
	client    *http.Client
	url       string
}

func New(key string, providers map[string]Provider) (*Service, error) {
	if strings.TrimSpace(key) == "" || len(providers) == 0 {
		return nil, errors.New("eligibility_configuration_required")
	}
	copyProviders := make(map[string]Provider, len(providers))
	for office, p := range providers {
		if office == "" || strings.TrimSpace(p.OrganizationName) == "" || !validNPI(p.NPI) {
			return nil, errors.New("invalid_eligibility_provider")
		}
		copyProviders[office] = p
	}
	return &Service{key: key, providers: copyProviders, url: endpoint, client: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
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

// Check sends at most one request. The authenticated durable caller must claim
// (scope, input, attempt) exclusively and journal dispatch BEFORE calling. A lost
// response is unknown, not permission to resubmit. History alone is not a lock.
func (s *Service) Check(ctx context.Context, in CheckInput) (Result, error) {
	if in.Scope.OfficeID == "" || in.Scope.PatientID == "" || in.Scope.AppointmentID == "" || !validDOB(in.Scope.ServiceDate) || !validPerson(in.Subscriber) || strings.TrimSpace(in.Subscriber.MemberID) == "" || (in.Dependent != nil && !validPerson(*in.Dependent)) || len(in.History) > maxAttempts || len(in.RecordedNames) > 8 {
		return Result{}, errors.New("invalid_eligibility_input")
	}
	original := in
	original.History = nil
	payer, reason := Route(in.Plan, in.Scope.ServiceDate)
	provider, providerOK := s.providers[in.Scope.OfficeID]
	data, _ := json.Marshal(struct {
		Input    CheckInput
		Payer    string
		Provider Provider
	}{original, payer, provider})
	hash := sha256.Sum256(data)
	out := Result{Scope: in.Scope, PayerID: payer, InputID: hex.EncodeToString(hash[:]), Attempt: len(in.History) + 1, Status: "review", Coverage: "unknown", CheckedAt: time.Now().UTC()}
	out.ReviewReason = reason
	if reason != "" {
		return out, nil
	}
	if !providerOK {
		out.ReviewReason = "provider_not_configured"
		return out, nil
	}
	base := Request{Payer: payer, Provider: provider, Subscriber: in.Subscriber,
		Encounter: Encounter{ServiceTypeCodes: []string{"30"}, DateOfService: in.Scope.ServiceDate}}
	expected := in.Subscriber
	if in.Dependent != nil {
		expected = *in.Dependent
		base.Dependents = []Person{expected}
	}
	prior := make([]Request, 0, len(in.History))
	for i, h := range in.History {
		if h.Scope != in.Scope || h.InputID != out.InputID || h.Attempt != i+1 {
			return Result{}, errors.New("eligibility_history_scope_mismatch")
		}
		if h.Status != "payer_rejected" || !CanRetryNames(h.ErrorCodes) || h.SearchID == "" {
			out.ReviewReason = "history_terminal_or_unknown"
			return out, nil
		}
		if i > 0 && h.SearchID != in.History[0].SearchID {
			return Result{}, errors.New("eligibility_search_chain_mismatch")
		}
		prior = append(prior, h.Request)
	}
	reqBody := base
	if len(prior) > 0 {
		last := in.History[len(prior)-1]
		plan := PlanRetries(base, in.RecordedNames, prior, last.ErrorCodes, true)
		if len(plan.Attempts) == 0 {
			out.ReviewReason = plan.Reason
			return out, nil
		}
		reqBody = plan.Attempts[0].Request
		reqBody.SearchID = last.SearchID
	}
	for _, p := range prior {
		if Fingerprint(p) == Fingerprint(reqBody) {
			out.ReviewReason = "duplicate_attempt"
			return out, nil
		}
	}
	out.Request = reqBody
	out.Status = "unknown"
	out.ReviewReason = "request_outcome_unknown"
	data, _ = json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(data))
	if err != nil {
		return out, nil
	}
	req.Header.Set("Authorization", s.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return out, nil
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
	if err != nil || len(raw) > 8*1024*1024 {
		return out, nil
	}
	if resp.StatusCode != http.StatusOK {
		out.ReviewReason = "stedi_http_failure"
		return out, nil
	}
	var response Response
	if json.Unmarshal(raw, &response) != nil {
		out.ReviewReason = "unrecognized_response"
		return out, nil
	}
	out.SearchID, out.CheckID = response.SearchID, response.ID
	if reqBody.SearchID != "" && response.SearchID != reqBody.SearchID {
		out.ReviewReason = "search_chain_mismatch"
		return out, nil
	}
	assessment := assessResponse(response, expected, in.Dependent != nil)
	if !assessment.Recognized {
		out.ReviewReason = "unrecognized_response"
		return out, nil
	}
	out.Match = &assessment.Match
	if response.Meta.Mode != "production" {
		out.ReviewReason = "nonproduction_or_unknown_mode"
		return out, nil
	}
	out.ErrorCodes = assessment.ErrorCodes
	if len(out.ErrorCodes) > 0 {
		out.Status = "payer_rejected"
		out.ReviewReason = "payer_rejected"
		return out, nil
	}
	out.Coverage = assessment.Coverage
	out.Status = "review"
	out.ReviewReason = "identity_uncertain"
	if assessment.Match.ReviewRequired {
		return out, nil
	}
	if out.Coverage == "unknown" {
		out.ReviewReason = "general_coverage_unknown"
		return out, nil
	}
	out.Status = "completed"
	out.ReviewReason = ""
	return out, nil
}
