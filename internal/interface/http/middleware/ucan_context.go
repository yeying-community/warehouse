package middleware

import "context"

// UcanContext holds UCAN-related scope information for a request.
type UcanContext struct {
	AppCaps        map[string][]string
	HasAppCaps     bool
	InvalidAppCaps []string
}

// UcanContextKey UCAN 上下文键
const UcanContextKey contextKey = "ucan"

// WithUcanContext adds UCAN context to request context.
func WithUcanContext(ctx context.Context, ucan *UcanContext) context.Context {
	if ctx == nil || ucan == nil {
		return ctx
	}
	return context.WithValue(ctx, UcanContextKey, ucan)
}

// GetUcanContext retrieves UCAN context from request context.
func GetUcanContext(ctx context.Context) (*UcanContext, bool) {
	ucan, ok := ctx.Value(UcanContextKey).(*UcanContext)
	return ucan, ok
}
