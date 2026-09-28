package domain

import (
	"strings"
	"time"
)

type TokenData struct {
	Token string `json:"token"`

	CookieToken string `json:"cookieToken"`

	WebserverURL string `json:"webserverUrl"`

	XmlrpcURL string `json:"xmlrpcUrl"`

	RestApiBase string `json:"restApiBase"`

	EhrApiBase string `json:"ehrApiBase"`

	CreatedAt string `json:"createdAt"`
}

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
