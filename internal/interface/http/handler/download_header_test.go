package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetAttachmentContentDisposition(t *testing.T) {
	recorder := httptest.NewRecorder()
	setAttachmentContentDisposition(recorder, "示例图片.png")

	got := recorder.Header().Get("Content-Disposition")
	if !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("unexpected disposition: %q", got)
	}
	if !strings.Contains(got, "filename=") {
		t.Fatalf("missing filename in disposition: %q", got)
	}
}

func TestSetInlineContentDisposition(t *testing.T) {
	recorder := httptest.NewRecorder()
	setInlineContentDisposition(recorder, "sample.png")

	got := recorder.Header().Get("Content-Disposition")
	if !strings.HasPrefix(got, "inline;") {
		t.Fatalf("unexpected disposition: %q", got)
	}
	if !strings.Contains(got, "filename=") {
		t.Fatalf("missing filename in disposition: %q", got)
	}
}
