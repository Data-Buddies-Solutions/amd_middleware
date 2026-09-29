package session

import "testing"

func TestBuildTokenData(t *testing.T) {
	for _, prefix := range []string{"https://", "http://", ""} {
		t.Run(prefix, func(t *testing.T) {
			retainedPrefix := prefix
			if prefix == "https://" {
				retainedPrefix = ""
			}
			host := retainedPrefix + "providerapi.advancedmd.com"
			got := buildTokenData("test-token", prefix+"providerapi.advancedmd.com/processrequest/api-801/myapp")
			want := TokenData{
				Token:       "Bearer test-token",
				CookieToken: "token=test-token",
				XmlrpcURL:   host + "/processrequest/api-801/myapp/xmlrpc/processrequest.aspx",
				RestApiBase: host + "/api/api-801/myapp",
			}
			if *got != want {
				t.Errorf("BuildTokenData() = %+v, want %+v", *got, want)
			}
		})
	}
}
