package clients

import "testing"

func TestParseWorkweek(t *testing.T) {
	tests := []struct {
		name string
		ww   string
		want int
	}{
		// AMD format: 7 chars for Mon-Sun (1=works, 0=off)
		// Our bitmask: 1=Sun, 2=Mon, 4=Tue, 8=Wed, 16=Thu, 32=Fri, 64=Sat
		{"Mon-Fri", "1111100", 2 + 4 + 8 + 16 + 32},            // 62
		{"Wed-Thu", "0011000", 8 + 16},                         // 24
		{"Every day", "1111111", 1 + 2 + 4 + 8 + 16 + 32 + 64}, // 127
		{"No days", "0000000", 0},
		{"Mon only", "1000000", 2},
		{"Sun only", "0000001", 1},
		{"Sat only", "0000010", 64},
		{"invalid length", "111", 0},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWorkweek(tt.ww)
			if got != tt.want {
				t.Errorf("parseWorkweek(%q) = %d, want %d", tt.ww, got, tt.want)
			}
		})
	}
}
