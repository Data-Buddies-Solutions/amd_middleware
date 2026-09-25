package domain

import (
	"testing"
)

func TestColumnsForRouting(t *testing.T) {
	office := DefaultOffice()

	tests := []struct {
		name    string
		rule    RoutingRule
		wantLen int
		wantIDs []string
	}{
		{"not accepted returns nil", RoutingNotAccepted, 0, nil},
		{"bach only returns 1513,1598", RoutingBachOnly, 2, []string{"1513", "1598"}},
		{"bach+licht returns 1513,1598,1551", RoutingBachLicht, 3, []string{"1513", "1598", "1551"}},
		{"all returns all", RoutingAll, 4, []string{"1513", "1598", "1551", "1550"}},
		{"optical only returns routine vision", RoutingOpticalOnly, 1, []string{"1600"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := office.ColumnsForRouting(tt.rule)
			if tt.wantLen == 0 {
				if cols != nil {
					t.Errorf("ColumnsForRouting(%q) = %v, want nil", tt.rule, cols)
				}
				return
			}
			if len(cols) != tt.wantLen {
				t.Errorf("ColumnsForRouting(%q) len = %d, want %d", tt.rule, len(cols), tt.wantLen)
			}
			for _, id := range tt.wantIDs {
				if !cols[id] {
					t.Errorf("ColumnsForRouting(%q) missing column %q", tt.rule, id)
				}
			}
		})
	}
}

func TestProvidersForRouting(t *testing.T) {
	office := DefaultOffice()

	tests := []struct {
		name      string
		rule      RoutingRule
		wantNames []string
	}{
		{"not accepted returns nil", RoutingNotAccepted, nil},
		{"bach only", RoutingBachOnly, []string{"Dr. Bach"}},
		{"bach+licht", RoutingBachLicht, []string{"Dr. Bach", "Dr. Licht"}},
		{"all", RoutingAll, []string{"Dr. Bach", "Dr. Licht", "Dr. Noel"}},
		{"optical only", RoutingOpticalOnly, []string{"Dr. Otero"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names := office.ProvidersForRouting(tt.rule)
			if tt.wantNames == nil {
				if names != nil {
					t.Errorf("ProvidersForRouting(%q) = %v, want nil", tt.rule, names)
				}
				return
			}
			if len(names) != len(tt.wantNames) {
				t.Fatalf("ProvidersForRouting(%q) len = %d, want %d", tt.rule, len(names), len(tt.wantNames))
			}
			for i, name := range tt.wantNames {
				if names[i] != name {
					t.Errorf("ProvidersForRouting(%q)[%d] = %q, want %q", tt.rule, i, names[i], name)
				}
			}
		})
	}
}

func TestParseRoutingRule(t *testing.T) {
	tests := []struct {
		input string
		want  RoutingRule
	}{
		{"not_accepted", RoutingNotAccepted},
		{"bach_only", RoutingBachOnly},
		{"bach_licht", RoutingBachLicht},
		{"all_three", RoutingAll},
		{"optical_only", RoutingOpticalOnly},
		{"", RoutingAll},          // default
		{"invalid", RoutingAll},   // default
		{"BACH_ONLY", RoutingAll}, // case sensitive, doesn't match
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseRoutingRule(tt.input)
			if got != tt.want {
				t.Errorf("ParseRoutingRule(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
