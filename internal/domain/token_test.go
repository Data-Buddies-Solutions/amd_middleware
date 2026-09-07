package domain

import (
	"testing"
	"time"
)

func TestBuildTokenDataAt(t *testing.T) {
	createdAt := time.Date(2026, 3, 3, 9, 30, 0, 0, time.FixedZone("EST", -5*60*60))
	for _, prefix := range []string{"https://", "http://", ""} {
		t.Run(prefix, func(t *testing.T) {
			retainedPrefix := prefix
			if prefix == "https://" {
				retainedPrefix = ""
			}
			host := retainedPrefix + "providerapi.advancedmd.com"
			got := BuildTokenDataAt("test-token", prefix+"providerapi.advancedmd.com/processrequest/api-801/myapp", createdAt)
			want := TokenData{
				Token:        "Bearer test-token",
				CookieToken:  "token=test-token",
				WebserverURL: host + "/processrequest/api-801/myapp",
				XmlrpcURL:    host + "/processrequest/api-801/myapp/xmlrpc/processrequest.aspx",
				RestApiBase:  host + "/api/api-801/myapp",
				EhrApiBase:   host + "/ehr-api/api-801/myapp",
				CreatedAt:    "2026-03-03T14:30:00Z",
			}
			if *got != want {
				t.Errorf("BuildTokenDataAt() = %+v, want %+v", *got, want)
			}
		})
	}
}
