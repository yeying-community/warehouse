package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponseWriterSupportsFlush(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapped := &responseWriter{ResponseWriter: recorder, statusCode: http.StatusOK}

	flusher, ok := interface{}(wrapped).(http.Flusher)
	if !ok {
		t.Fatal("responseWriter should implement http.Flusher")
	}

	flusher.Flush()

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status after flush: got=%d want=%d", recorder.Code, http.StatusOK)
	}
	if !wrapped.wroteHeader {
		t.Fatal("Flush should write the pending status header")
	}
}
