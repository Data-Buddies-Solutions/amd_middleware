package eligibility

import "strings"

type planDescription struct {
	Description string `json:"planDescription"`
}

type Response struct {
	PlanInformation planDescription `json:"planInformation"`
	Meta            struct {
		Mode string `json:"applicationMode"`
	} `json:"meta"`
	ID         string   `json:"id"`
	Subscriber Person   `json:"subscriber"`
	Dependents []Person `json:"dependents"`
	Errors     []struct {
		Code string `json:"code"`
	} `json:"errors"`
	Benefits []struct {
		Code       string          `json:"code"`
		Plan       string          `json:"planCoverage"`
		Additional planDescription `json:"benefitsAdditionalInformation"`
		Services   []string        `json:"serviceTypeCodes"`
	} `json:"benefitsInformation"`
	SearchID string `json:"eligibilitySearchId"`
}

type responseAssessment struct {
	Recognized bool
	Coverage   string
	Match      MatchResult
	ErrorCodes []string
}

// assessResponse interprets an already-decoded payer response. It neither plans
// retries nor depends on replay inputs or transport details.
func assessResponse(response Response, expected Person, dependent bool) responseAssessment {
	// Empty/null entries are malformed evidence, even alongside valid benefits.
	for _, benefit := range response.Benefits {
		if strings.TrimSpace(benefit.Code) == "" {
			return responseAssessment{Coverage: "unknown"}
		}
	}
	for _, rejection := range response.Errors {
		if strings.TrimSpace(rejection.Code) == "" {
			return responseAssessment{Coverage: "unknown"}
		}
	}
	patient := response.Subscriber
	if len(response.Dependents) == 1 {
		patient = response.Dependents[0]
	}
	assessment := responseAssessment{
		Recognized: len(response.Errors) > 0 || len(response.Benefits) > 0,
		Coverage:   generalCoverage(response),
		Match:      Match(expected, patient),
	}
	if len(response.Dependents) > 1 {
		assessment.Match.Status = "ambiguous_dependents"
		assessment.Match.ReviewRequired = true
	}
	if (dependent && len(response.Dependents) != 1) || (!dependent && len(response.Dependents) != 0) {
		assessment.Match.ReviewRequired = true
		assessment.Match.Reasons = append(assessment.Match.Reasons, "patient_role_uncertain")
	}
	for _, e := range response.Errors {
		assessment.ErrorCodes = append(assessment.ErrorCodes, e.Code)
	}
	if len(assessment.ErrorCodes) > 0 {
		assessment.Match.Status = "payer_rejected"
		assessment.Match.ReviewRequired = true
	}
	return assessment
}

// generalCoverage is shared by saved replay and live checks. Contradictory,
// non-production and service-specific benefits never establish plan activity.
func generalCoverage(response Response) string {
	if response.Meta.Mode != "production" || len(response.Errors) > 0 {
		return "unknown"
	}
	active, inactive := false, false
	for _, b := range response.Benefits {
		general := false
		for _, code := range b.Services {
			if code == "30" {
				general = true
			}
		}
		if !general {
			continue
		}
		switch b.Code {
		case "1", "2", "3", "4":
			active = true
		case "6", "7", "8":
			inactive = true
		}
	}
	if active == inactive {
		return "unknown"
	}
	if active {
		return "active"
	}
	return "inactive"
}
