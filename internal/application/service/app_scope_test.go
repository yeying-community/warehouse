package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	domainauth "github.com/yeying-community/webdav/internal/domain/auth"
	"github.com/yeying-community/webdav/internal/infrastructure/config"
	"github.com/yeying-community/webdav/internal/interface/http/middleware"
)

func TestResolveAppScope_RequireAppScopeWithoutAppCaps(t *testing.T) {
	cfg := &config.Config{
		Web3: config.Web3Config{
			UCAN: config.UCANConfig{
				RequiredResource: "app:*",
				RequiredAction:   "read,write",
				AppScope: config.AppScopeConfig{
					PathPrefix: "/apps",
				},
			},
		},
	}

	ctx := middleware.WithUcanContext(context.Background(), &middleware.UcanContext{
		AppCaps:        map[string][]string{},
		HasAppCaps:     false,
		InvalidAppCaps: []string{},
	})

	_, err := resolveAppScope(ctx, cfg)
	if !errors.Is(err, domainauth.ErrAppScopeRequired) {
		t.Fatalf("expected ErrAppScopeRequired, got %v", err)
	}
}

func TestResolveAppScope_NoUcanContextShouldNotBlock(t *testing.T) {
	cfg := &config.Config{
		Web3: config.Web3Config{
			UCAN: config.UCANConfig{
				RequiredResource: "app:*",
				RequiredAction:   "read,write",
				AppScope: config.AppScopeConfig{
					PathPrefix: "/apps",
				},
			},
		},
	}

	scope, err := resolveAppScope(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if scope.active {
		t.Fatalf("expected inactive scope without UCAN context")
	}
}

func TestWebDAVCheckAppScope_NoUcanContextShouldNotBlock(t *testing.T) {
	svc := &WebDAVService{
		config: &config.Config{
			WebDAV: config.WebDAVConfig{Prefix: "/dav"},
			Web3: config.Web3Config{
				UCAN: config.UCANConfig{
					RequiredResource: "app:*",
					RequiredAction:   "read,write",
					AppScope: config.AppScopeConfig{
						PathPrefix: "/apps",
					},
				},
			},
		},
	}

	reqs := []*http.Request{
		{
			Method: http.MethodGet,
			URL:    mustParseURL(t, "/dav/personal/a.txt"),
			Header: make(http.Header),
		},
		{
			Method: http.MethodDelete,
			URL:    mustParseURL(t, "/dav/apps/any.app/a.txt"),
			Header: make(http.Header),
		},
	}

	for _, req := range reqs {
		if err := svc.checkAppScope(context.Background(), req); err != nil {
			t.Fatalf("expected request %s %s not blocked without UCAN context, got %v", req.Method, req.URL.Path, err)
		}
	}
}

func TestResolveAppScope_InvalidAppCapsDenied(t *testing.T) {
	cfg := &config.Config{
		Web3: config.Web3Config{
			UCAN: config.UCANConfig{
				RequiredResource: "app:*",
				RequiredAction:   "read,write",
			},
		},
	}

	ctx := middleware.WithUcanContext(context.Background(), &middleware.UcanContext{
		AppCaps:        map[string][]string{},
		HasAppCaps:     true,
		InvalidAppCaps: []string{"app:*#write"},
	})

	_, err := resolveAppScope(ctx, cfg)
	if !errors.Is(err, domainauth.ErrAppScopeDenied) {
		t.Fatalf("expected ErrAppScopeDenied, got %v", err)
	}
}

func TestWebDAVCheckAppScope_MatchingAppAllowedAndCrossAppDenied(t *testing.T) {
	svc := &WebDAVService{
		config: &config.Config{
			WebDAV: config.WebDAVConfig{Prefix: "/dav"},
			Web3: config.Web3Config{
				UCAN: config.UCANConfig{
					RequiredResource: "app:*",
					RequiredAction:   "read,write",
					AppScope: config.AppScopeConfig{
						PathPrefix: "/apps",
					},
				},
			},
		},
	}

	ctx := middleware.WithUcanContext(context.Background(), &middleware.UcanContext{
		AppCaps: map[string][]string{
			"dapp.example.com": []string{"read", "write"},
		},
		HasAppCaps:     true,
		InvalidAppCaps: []string{},
	})

	allowReq := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL(t, "/dav/apps/dapp.example.com/data.json"),
		Header: make(http.Header),
	}
	if err := svc.checkAppScope(ctx, allowReq); err != nil {
		t.Fatalf("expected allow request, got %v", err)
	}

	denyReq := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL(t, "/dav/apps/another.app/data.json"),
		Header: make(http.Header),
	}
	if err := svc.checkAppScope(ctx, denyReq); !errors.Is(err, domainauth.ErrAppScopeDenied) {
		t.Fatalf("expected ErrAppScopeDenied, got %v", err)
	}
}

func TestWebDAVCheckAppScope_MoveCopyValidateDestination(t *testing.T) {
	svc := &WebDAVService{
		config: &config.Config{
			WebDAV: config.WebDAVConfig{Prefix: "/dav"},
			Web3: config.Web3Config{
				UCAN: config.UCANConfig{
					RequiredResource: "app:*",
					RequiredAction:   "read,write",
					AppScope: config.AppScopeConfig{
						PathPrefix: "/apps",
					},
				},
			},
		},
	}

	ctx := middleware.WithUcanContext(context.Background(), &middleware.UcanContext{
		AppCaps: map[string][]string{
			"dapp.example.com": []string{"read", "write"},
		},
		HasAppCaps:     true,
		InvalidAppCaps: []string{},
	})

	cases := []struct {
		name       string
		method     string
		source     string
		dest       string
		shouldDeny bool
	}{
		{
			name:       "move within same app allowed",
			method:     "MOVE",
			source:     "/dav/apps/dapp.example.com/a.txt",
			dest:       "/dav/apps/dapp.example.com/b.txt",
			shouldDeny: false,
		},
		{
			name:       "move cross app denied",
			method:     "MOVE",
			source:     "/dav/apps/dapp.example.com/a.txt",
			dest:       "/dav/apps/other.app/b.txt",
			shouldDeny: true,
		},
		{
			name:       "copy within same app allowed",
			method:     "COPY",
			source:     "/dav/apps/dapp.example.com/a.txt",
			dest:       "/dav/apps/dapp.example.com/b.txt",
			shouldDeny: false,
		},
		{
			name:       "copy cross app denied",
			method:     "COPY",
			source:     "/dav/apps/dapp.example.com/a.txt",
			dest:       "/dav/apps/other.app/b.txt",
			shouldDeny: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &http.Request{
				Method: tc.method,
				URL:    mustParseURL(t, tc.source),
				Header: make(http.Header),
			}
			req.Header.Set("Destination", tc.dest)

			err := svc.checkAppScope(ctx, req)
			if tc.shouldDeny {
				if !errors.Is(err, domainauth.ErrAppScopeDenied) {
					t.Fatalf("expected ErrAppScopeDenied, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected allow, got %v", err)
			}
		})
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		t.Fatalf("failed to parse url %s: %v", raw, err)
	}
	return u
}
