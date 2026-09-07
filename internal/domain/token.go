// Package domain contains core business models and logic.
package domain

import (
	"strings"
	"time"
)

// TokenData represents cached token information.
// Contains both the authentication token and pre-built URLs for all AdvancedMD API types.
type TokenData struct {
	// Token is the AdvancedMD session token pre-formatted with "Bearer " prefix.
	Token string `json:"token"`

	// CookieToken is the token pre-formatted for XMLRPC Cookie header.
	// Format: "token={rawtoken}"
	CookieToken string `json:"cookieToken"`

	// WebserverURL is the base URL returned from AdvancedMD login (without https://).
	WebserverURL string `json:"webserverUrl"`

	// XmlrpcURL is the full XMLRPC endpoint URL (without https://).
	XmlrpcURL string `json:"xmlrpcUrl"`

	// RestApiBase is the base URL for Practice Manager REST API (without https://).
	RestApiBase string `json:"restApiBase"`

	// EhrApiBase is the base URL for EHR REST API (without https://).
	EhrApiBase string `json:"ehrApiBase"`

	// CreatedAt is the RFC3339 timestamp when this token was generated.
	CreatedAt string `json:"createdAt"`
}

// BuildTokenDataAt creates token data with an explicit creation time.
func BuildTokenDataAt(token, webserverURL string, createdAt time.Time) *TokenData {
	base := strings.TrimPrefix(webserverURL, "https://")
	return &TokenData{
		Token:        "Bearer " + token,
		CookieToken:  "token=" + token,
		WebserverURL: base,
		XmlrpcURL:    base + "/xmlrpc/processrequest.aspx",
		RestApiBase:  strings.Replace(base, "/processrequest/", "/api/", 1),
		EhrApiBase:   strings.Replace(base, "/processrequest/", "/ehr-api/", 1),
		CreatedAt:    createdAt.UTC().Format(time.RFC3339),
	}
}
