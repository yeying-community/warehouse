package service

import (
	"testing"

	"github.com/yeying-community/warehouse/internal/domain/s3multipart"
)

func TestMultipartETag(t *testing.T) {
	parts := []*s3multipart.Part{
		{ETag: "d41d8cd98f00b204e9800998ecf8427e"},
		{ETag: "0cc175b9c0f1b6a831c399e269772661"},
	}
	got := multipartETag(parts)
	if len(got) != 34 || got[len(got)-2:] != "-2" {
		t.Fatalf("multipart ETag = %q, want 32 hex chars and -2 suffix", got)
	}
	if got := multipartETag([]*s3multipart.Part{{ETag: "not-md5"}}); got != "" {
		t.Fatalf("invalid part ETag = %q, want empty", got)
	}
}
