package share

import "testing"

func TestNormalizeMode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to download", input: "", want: ModeDownload},
		{name: "download", input: ModeDownload, want: ModeDownload},
		{name: "preview", input: ModePreview, want: ModePreview},
		{name: "invalid", input: "inline", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeMode(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("unexpected mode: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestShareItemIsPreviewMode(t *testing.T) {
	preview := NewShareItem("u1", "alice", "/docs/a.png", "a.png", ModePreview, nil)
	if !preview.IsPreviewMode() {
		t.Fatal("expected preview item to report preview mode")
	}

	download := NewShareItem("u1", "alice", "/docs/a.png", "a.png", ModeDownload, nil)
	if download.IsPreviewMode() {
		t.Fatal("expected download item to report non-preview mode")
	}
}
