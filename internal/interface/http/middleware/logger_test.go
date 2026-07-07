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

func TestResponseWriterTracksImplicitWriteHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapped := &responseWriter{ResponseWriter: recorder, statusCode: http.StatusOK}

	if _, err := wrapped.Write([]byte("ok")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	wrapped.WriteHeader(http.StatusInternalServerError)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", recorder.Code, http.StatusOK)
	}
	if wrapped.statusCode != http.StatusOK {
		t.Fatalf("unexpected wrapped status: got=%d want=%d", wrapped.statusCode, http.StatusOK)
	}
	if !wrapped.wroteHeader {
		t.Fatal("Write should mark header as written")
	}
}
