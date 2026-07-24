package handler

import "testing"

func TestS3RootPathAllowsServicesSpace(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		allowed bool
	}{
		{name: "default", raw: "", allowed: true},
		{name: "personal", raw: "/personal/backups", allowed: true},
		{name: "apps", raw: "/apps/demo", allowed: true},
		{name: "services", raw: "/services/reporting", allowed: true},
		{name: "services backslashes", raw: `services\reporting`, allowed: true},
		{name: "unsupported", raw: "/other", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAllowedS3RootPath(normalizeS3RootPath(tt.raw))
			if got != tt.allowed {
				t.Fatalf("isAllowedS3RootPath(%q) = %v, want %v", tt.raw, got, tt.allowed)
			}
		})
	}
}
