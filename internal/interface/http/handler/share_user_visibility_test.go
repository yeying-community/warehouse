package handler

import "testing"

func TestIsIgnoredShareName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want bool
	}{
		{name: "temp upload", in: "._upload-123", want: true},
		{name: "finder sidecar", in: "._note.txt", want: true},
		{name: "ds store", in: ".DS_Store", want: true},
		{name: "normal file", in: "note.txt", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isIgnoredShareName(tc.in); got != tc.want {
				t.Fatalf("isIgnoredShareName(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsIgnoredSharePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path string
		want bool
	}{
		{path: "/tmp/shared/._upload-1", want: true},
		{path: "/tmp/shared/folder/._asset", want: true},
		{path: "/tmp/shared/folder/note.txt", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if got := isIgnoredSharePath(tc.path); got != tc.want {
				t.Fatalf("isIgnoredSharePath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
