package session

import (
	"strings"
)

type TokenData struct {
	Token       string
	CookieToken string
	XmlrpcURL   string
	RestApiBase string
}

func buildTokenData(token, webserverURL string) *TokenData {
	base := strings.TrimPrefix(webserverURL, "https://")
	return &TokenData{
		Token:       "Bearer " + token,
		CookieToken: "token=" + token,
		XmlrpcURL:   base + "/xmlrpc/processrequest.aspx",
		RestApiBase: strings.Replace(base, "/processrequest/", "/api/", 1),
	}
}
