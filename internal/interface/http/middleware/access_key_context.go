package middleware

import "context"

const (
	accessKeyContextKey contextKey = "webdav_access_key"
)

type AccessKeyContext struct {
	KeyID string
}

func WithAccessKeyContext(ctx context.Context, info *AccessKeyContext) context.Context {
	return context.WithValue(ctx, accessKeyContextKey, info)
}

func GetAccessKeyContext(ctx context.Context) (*AccessKeyContext, bool) {
	info, ok := ctx.Value(accessKeyContextKey).(*AccessKeyContext)
	return info, ok
}
