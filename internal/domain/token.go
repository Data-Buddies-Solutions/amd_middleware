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

// stripProtocol removes the https:// prefix from a URL.
func stripProtocol(url string) string {
	return strings.TrimPrefix(url, "https://")
}

// buildXmlrpcURL constructs the XMLRPC API endpoint path from the webserver URL.
// Input:  https://providerapi.advancedmd.com/processrequest/api-801/YOURAPP
// Output: providerapi.advancedmd.com/processrequest/api-801/YOURAPP/xmlrpc/processrequest.aspx
func buildXmlrpcURL(webserverURL string) string {
	return stripProtocol(webserverURL + "/xmlrpc/processrequest.aspx")
}

// buildRestApiBase constructs the Practice Manager REST API base path.
// Input:  https://providerapi.advancedmd.com/processrequest/api-801/YOURAPP
// Output: providerapi.advancedmd.com/api/api-801/YOURAPP
func buildRestApiBase(webserverURL string) string {
	return stripProtocol(strings.Replace(webserverURL, "/processrequest/", "/api/", 1))
}

// buildEhrApiBase constructs the EHR REST API base path.
// Input:  https://providerapi.advancedmd.com/processrequest/api-801/YOURAPP
// Output: providerapi.advancedmd.com/ehr-api/api-801/YOURAPP
func buildEhrApiBase(webserverURL string) string {
	return stripProtocol(strings.Replace(webserverURL, "/processrequest/", "/ehr-api/", 1))
}

// BuildTokenData creates a complete TokenData struct with all pre-built URLs.
func BuildTokenData(token, webserverURL string) *TokenData {
	return &TokenData{
		Token:        "Bearer " + token,
		CookieToken:  "token=" + token,
		WebserverURL: stripProtocol(webserverURL),
		XmlrpcURL:    buildXmlrpcURL(webserverURL),
		RestApiBase:  buildRestApiBase(webserverURL),
		EhrApiBase:   buildEhrApiBase(webserverURL),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
}
