package middleware

import (
	"net/http"
	"testing"
)

func TestIsAccessKeyRequestAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		method       string
		path         string
		webdavPrefix string
		headers      map[string]string
		want         bool
	}{
		{
			name:         "webdav prefix path allowed",
			method:       http.MethodGet,
			path:         "/dav/docs/readme.md",
			webdavPrefix: "/dav",
			want:         true,
		},
		{
			name:         "api path denied",
			method:       http.MethodGet,
			path:         "/api/v1/public/webdav/user/info",
			webdavPrefix: "/dav",
			want:         false,
		},
		{
			name:         "root prefix non-webdav request denied",
			method:       http.MethodGet,
			path:         "/api/v1/public/share/list",
			webdavPrefix: "/",
			want:         false,
		},
		{
			name:         "root prefix webdav method allowed",
			method:       "PROPFIND",
			path:         "/docs/",
			webdavPrefix: "/",
			want:         true,
		},
		{
			name:         "root prefix webdav header allowed",
			method:       http.MethodGet,
			path:         "/docs/",
			webdavPrefix: "/",
			headers: map[string]string{
				"Depth": "1",
			},
			want: true,
		},
		{
			name:         "prefix normalization supported",
			method:       http.MethodGet,
			path:         "/dav/docs/readme.md",
			webdavPrefix: "dav/",
			want:         true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequest(tt.method, tt.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			got := isAccessKeyRequestAllowed(req, tt.webdavPrefix)
			if got != tt.want {
				t.Fatalf("isAccessKeyRequestAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}
