package s3

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
)

func TestVerifyHeaderSignatureSurvivesHostPreservingReverseProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, err := VerifyHeaderSignature(req, testSecretKey, testSignatureConfig()); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.Host = request.In.Host
			request.SetXForwarded()
		},
	}
	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	req, err := http.NewRequest(
		http.MethodGet,
		proxyServer.URL+"/personal/folder/%E6%B5%8B%E8%AF%95%20a%2Bb.txt?delimiter=%2F&list-type=2&prefix=folder%2F&x=2&x=1",
		nil,
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Host = "s3.yeying.pub"
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	req.Header.Set("X-Amz-Date", "20260710T010203Z")
	req.Header.Set(
		"Authorization",
		"AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20260710/us-east-1/s3/aws4_request, SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date, Signature=afda99f7107ae4de11cb11642d20f814f4ae1fc1b7aaba6415b99aa71228b43d",
	)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send proxied request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected response status: %d", response.StatusCode)
	}
}
