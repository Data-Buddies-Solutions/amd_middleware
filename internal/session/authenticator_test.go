package session

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdvancedMDLoginGetsAuthToken(t *testing.T) {
	// Mock server for step 2 - returns token
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
			<PPMDResults>
				<Results success="1">
					<usercontext>mock-session-token-12345</usercontext>
				</Results>
			</PPMDResults>`))
	}))
	defer server.Close()

	login := &advancedMDLogin{
		creds: Credentials{
			Username:  "testuser",
			Password:  "testpass",
			OfficeKey: "991TEST",
			AppName:   "testapp",
		},
		client: server.Client(),
	}

	token, err := login.getAuthToken(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("GetAuthToken failed: %v", err)
	}

	if token != "mock-session-token-12345" {
		t.Errorf("Expected token 'mock-session-token-12345', got '%s'", token)
	}
}

func TestAdvancedMDLoginEscapesCredentialAttributes(t *testing.T) {
	creds := Credentials{
		Username:  `user<"name">`,
		Password:  `p&ss"w<rd>'`,
		OfficeKey: "991&TEST",
		AppName:   `app"name`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message struct {
			Username  string `xml:"username,attr"`
			Password  string `xml:"psw,attr"`
			OfficeKey string `xml:"officecode,attr"`
			AppName   string `xml:"appname,attr"`
		}
		if err := xml.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Errorf("login message is not valid XML: %v", err)
		}
		got := Credentials{
			Username:  message.Username,
			Password:  message.Password,
			OfficeKey: message.OfficeKey,
			AppName:   message.AppName,
		}
		if got != creds {
			t.Errorf("login credentials were altered in transit")
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<PPMDResults><Results success="1"><usercontext>token</usercontext></Results></PPMDResults>`))
	}))
	defer server.Close()

	login := &advancedMDLogin{creds: creds, client: server.Client()}
	if _, err := login.getAuthToken(context.Background(), server.URL); err != nil {
		t.Fatalf("getAuthToken failed: %v", err)
	}
}

func TestAdvancedMDLoginRejectsFailedAuthToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
			<PPMDResults>
				<Results success="0">
					<usercontext></usercontext>
				</Results>
				<Error>
					<Fault>
						<detail>
							<code>-1</code>
							<description>Invalid credentials</description>
						</detail>
					</Fault>
				</Error>
			</PPMDResults>`))
	}))
	defer server.Close()

	login := &advancedMDLogin{
		creds: Credentials{
			Username:  "baduser",
			Password:  "badpass",
			OfficeKey: "991TEST",
			AppName:   "testapp",
		},
		client: server.Client(),
	}

	_, err := login.getAuthToken(context.Background(), server.URL)
	if err == nil {
		t.Error("Expected error for failed auth, got nil")
	}
}

func TestParseXMLResponse(t *testing.T) {
	tests := []struct {
		name        string
		xml         string
		wantSuccess string
		wantToken   string
		wantError   bool
	}{
		{
			name: "success response with token",
			xml: `<?xml version="1.0" encoding="utf-8"?>
				<PPMDResults>
					<Results success="1">
						<usercontext>test-token</usercontext>
					</Results>
				</PPMDResults>`,
			wantSuccess: "1",
			wantToken:   "test-token",
			wantError:   false,
		},
		{
			name: "step 1 response with webserver",
			xml: `<?xml version="1.0" encoding="utf-8"?>
				<PPMDResults>
					<Results success="0">
						<usercontext webserver="https://test.com/api"></usercontext>
					</Results>
				</PPMDResults>`,
			wantSuccess: "0",
			wantError:   false,
		},
		{
			name:      "invalid XML",
			xml:       `not valid xml`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseXMLResponse([]byte(tt.xml))
			if tt.wantError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if result.Results.Success != tt.wantSuccess {
				t.Errorf("Expected success=%s, got %s", tt.wantSuccess, result.Results.Success)
			}
		})
	}
}
