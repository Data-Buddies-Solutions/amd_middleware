package eligibility

import (
	"encoding/json"
	"errors"
)

type Case struct {
	Ref           string          `json:"ref"`
	Expected      Person          `json:"expected"`
	Response      json.RawMessage `json:"response"`
	Base          Request         `json:"base"`
	RecordedNames []RecordedName  `json:"recordedNames"`
	Prior         []Request       `json:"prior"`
	Supported     bool            `json:"supported"`
	Source        string          `json:"source"`
}

type Assessment struct {
	Ref            string      `json:"ref"`
	ActiveResponse bool        `json:"activeResponse"`
	Coverage       string      `json:"coverage"`
	Match          MatchResult `json:"match"`
	RetryPlan      RetryPlan   `json:"retryPlan"`
}

// Assess adapts private saved cases to the same interpretation used by live checks.
func Assess(c Case) (Assessment, error) {
	if c.Source != "eligibility" && c.Source != "discovery" {
		return Assessment{}, errors.New("unsupported response source")
	}
	var response Response
	if err := json.Unmarshal(c.Response, &response); err != nil {
		return Assessment{}, err
	}
	evaluated := assessResponse(response, c.Expected, len(c.Base.Dependents) == 1)
	if c.Source == "eligibility" && !evaluated.Recognized {
		return Assessment{}, errors.New("unrecognized eligibility outcome")
	}
	if c.Source == "discovery" {
		evaluated.Match.Status = "discovery_review"
		evaluated.Match.ReviewRequired = true
		evaluated.Coverage = "unknown"
	}
	return Assessment{
		Ref: c.Ref, Coverage: evaluated.Coverage, ActiveResponse: evaluated.Coverage == "active", Match: evaluated.Match,
		RetryPlan: PlanRetries(c.Base, c.RecordedNames, c.Prior, evaluated.ErrorCodes, c.Supported && c.Source == "eligibility"),
	}, nil
}
