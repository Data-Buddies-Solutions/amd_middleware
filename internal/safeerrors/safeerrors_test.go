package safeerrors

import (
	"context"
	"fmt"
	"testing"
)

func TestCategoryRedactsErrorDetails(t *testing.T) {
	sensitive := "patientId=17604634 token=secret-token provider-body=<private>"
	tests := []struct {
		err  error
		want Category
	}{
		{err: context.DeadlineExceeded, want: "timeout"},
		{err: fmt.Errorf("unexpected status 500: %s", sensitive), want: "upstream_status"},
		{err: fmt.Errorf("login failed: %s", sensitive), want: "authentication"},
		{err: fmt.Errorf("advancedmd session unavailable"), want: "unavailable"},
		{err: fmt.Errorf("provider rejected request: %s", sensitive), want: "upstream_error"},
	}

	for _, tt := range tests {
		got := Classify(tt.err)
		if got != tt.want {
			t.Fatalf("Category(%q) = %q, want %q", tt.err, got, tt.want)
		}
	}
}
