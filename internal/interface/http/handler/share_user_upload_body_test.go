package handler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

func TestOpenSharedUploadBodyAcceptsRawPutBody(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("PUT", "/api/v1/public/share/user/upload", bytes.NewReader([]byte("payload")))
	req.Header.Set("Content-Type", "application/octet-stream")

	body, err := openSharedUploadBody(req)
	if err != nil {
		t.Fatalf("openSharedUploadBody: %v", err)
	}
	defer body.Close()

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("expected payload, got %q", string(got))
	}
}

func TestOpenSharedUploadBodyAcceptsMultipartForm(t *testing.T) {
	t.Parallel()

	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	part, err := writer.CreateFormFile("file", "demo.txt")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("payload")); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest("PUT", "/api/v1/public/share/user/upload", &payload)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	body, err := openSharedUploadBody(req)
	if err != nil {
		t.Fatalf("openSharedUploadBody: %v", err)
	}
	defer body.Close()

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("expected payload, got %q", string(got))
	}
}
