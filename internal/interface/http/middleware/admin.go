package middleware

import (
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// AdminMiddleware restricts access to configured admin wallet addresses.
type AdminMiddleware struct {
	allowed map[string]struct{}
	logger  *zap.Logger
}

// NewAdminMiddleware creates a new admin middleware.
func NewAdminMiddleware(addresses []string, logger *zap.Logger) *AdminMiddleware {
	allowed := make(map[string]struct{}, len(addresses))
	for _, raw := range addresses {
		addr := strings.ToLower(strings.TrimSpace(raw))
		if addr == "" {
			continue
		}
		allowed[addr] = struct{}{}
	}
	return &AdminMiddleware{
		allowed: allowed,
		logger:  logger,
	}
}

// Handle enforces admin access.
func (m *AdminMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := GetUserFromContext(r.Context())
		if !ok {
			m.logger.Warn("admin access denied: user not in context")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if len(m.allowed) == 0 {
			m.logger.Warn("admin access denied: no admin addresses configured",
				zap.String("username", u.Username))
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		addr := strings.ToLower(strings.TrimSpace(u.WalletAddress))
		if addr == "" {
			m.logger.Warn("admin access denied: missing wallet address",
				zap.String("username", u.Username))
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		if _, ok := m.allowed[addr]; !ok {
			m.logger.Warn("admin access denied: address not allowed",
				zap.String("username", u.Username),
				zap.String("wallet_address", addr))
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
