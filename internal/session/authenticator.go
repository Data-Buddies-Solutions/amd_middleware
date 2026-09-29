package session

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

type Credentials struct {
	Username  string
	Password  string
	OfficeKey string
	AppName   string
}

type loginResponse struct {
	XMLName xml.Name     `xml:"PPMDResults"`
	Results loginResults `xml:"Results"`
	Error   loginError   `xml:"Error"`
}

type loginResults struct {
	Success     string           `xml:"success,attr"`
	UserContext loginUserContext `xml:"usercontext"`
}

type loginUserContext struct {
	Webserver string `xml:"webserver,attr"`
	Token     string `xml:",chardata"`
}

type loginError struct {
	Fault loginFault `xml:"Fault"`
}

type loginFault struct {
	Code        string `xml:"detail>code"`
	Description string `xml:"detail>description"`
}

type advancedMDLogin struct {
	creds  Credentials
	client *http.Client
}

func newAdvancedMDLogin(creds Credentials, client *http.Client) *advancedMDLogin {
	return &advancedMDLogin{
		creds:  creds,
		client: client,
	}
}

func (a *advancedMDLogin) buildLoginXML() string {
	now := time.Now().Format("1/2/2006 3:04:05 PM")
	return fmt.Sprintf(
		`<ppmdmsg action="login" class="login" msgtime="%s" username="%s" psw="%s" officecode="%s" appname="%s"/>`,
		now,
		escapeXMLAttr(a.creds.Username),
		escapeXMLAttr(a.creds.Password),
		escapeXMLAttr(a.creds.OfficeKey),
		escapeXMLAttr(a.creds.AppName),
	)
}

func escapeXMLAttr(value string) string {
	var escaped strings.Builder
	xml.EscapeText(&escaped, []byte(value))
	return escaped.String()
}

func parseXMLResponse(body []byte) (*loginResponse, error) {
	var result loginResponse
	decoder := xml.NewDecoder(bytes.NewReader(body))
	decoder.CharsetReader = charset.NewReaderLabel
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse XML: %w", err)
	}
	return &result, nil
}

func (a *advancedMDLogin) postLogin(ctx context.Context, url string) (*loginResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(a.buildLoginXML()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/xml")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	result, err := parseXMLResponse(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse XML response: %w", err)
	}
	return result, nil
}

func (a *advancedMDLogin) getWebserver(ctx context.Context) (string, error) {
	const url = "https://partnerlogin.advancedmd.com/practicemanager/xmlrpc/processrequest.aspx"
	result, err := a.postLogin(ctx, url)
	if err != nil {
		return "", err
	}

	webserver := result.Results.UserContext.Webserver
	if webserver == "" {
		return "", fmt.Errorf("no webserver URL in response. Error: %s", result.Error.Fault.Description)
	}

	return webserver, nil
}

func (a *advancedMDLogin) getAuthToken(ctx context.Context, webserverURL string) (string, error) {
	url := webserverURL + "/xmlrpc/processrequest.aspx"
	result, err := a.postLogin(ctx, url)
	if err != nil {
		return "", err
	}

	if result.Results.Success != "1" {
		return "", fmt.Errorf("login failed: success=%s, error=%s",
			result.Results.Success, result.Error.Fault.Description)
	}

	token := strings.TrimSpace(result.Results.UserContext.Token)
	if token == "" {
		return "", fmt.Errorf("no token in response")
	}

	return token, nil
}

func (a *advancedMDLogin) Authenticate(ctx context.Context) (token, webserverURL string, err error) {
	webserverURL, err = a.getWebserver(ctx)
	if err != nil {
		return "", "", fmt.Errorf("step 1 (get webserver) failed: %w", err)
	}

	token, err = a.getAuthToken(ctx, webserverURL)
	if err != nil {
		return "", "", fmt.Errorf("step 2 (get token) failed: %w", err)
	}

	return token, webserverURL, nil
}
