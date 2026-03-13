package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
)

const (
	InternalNodeIDHeader        = "X-Warehouse-Node-Id"
	InternalTimestampHeader     = "X-Warehouse-Timestamp"
	InternalSignatureHeader     = "X-Warehouse-Signature"
	InternalContentSHA256Header = "X-Warehouse-Content-SHA256"
	InternalAssignmentGenerationHeader = "X-Warehouse-Assignment-Generation"
	unsignedPayloadMarker       = "UNSIGNED-PAYLOAD"
)

// InternalAuthMiddleware authenticates internal service-to-service requests.
type InternalAuthMiddleware struct {
	config config.InternalReplicationConfig
	logger *zap.Logger
	now    func() time.Time
}

// NewInternalAuthMiddleware creates a new internal auth middleware.
func NewInternalAuthMiddleware(cfg config.InternalReplicationConfig, logger *zap.Logger) *InternalAuthMiddleware {
	return &InternalAuthMiddleware{
		config: cfg,
		logger: logger,
		now:    time.Now,
	}
}

// Handle validates HMAC-signed internal requests.
func (m *InternalAuthMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := m.validateRequest(r); err != nil {
			m.logger.Warn("internal auth denied",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Error(err))
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (m *InternalAuthMiddleware) validateRequest(r *http.Request) error {
	if !m.config.Enabled {
		return fmt.Errorf("internal replication is disabled")
	}

	nodeID := strings.TrimSpace(r.Header.Get(InternalNodeIDHeader))
	if nodeID == "" {
		return fmt.Errorf("missing %s", InternalNodeIDHeader)
	}

	timestampRaw := strings.TrimSpace(r.Header.Get(InternalTimestampHeader))
	if timestampRaw == "" {
		return fmt.Errorf("missing %s", InternalTimestampHeader)
	}
	timestamp, err := time.Parse(time.RFC3339, timestampRaw)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	if skew := m.now().Sub(timestamp); skew > m.config.AllowedClockSkew || skew < -m.config.AllowedClockSkew {
		return fmt.Errorf("timestamp skew exceeds allowed clock skew")
	}

	signature := strings.TrimSpace(r.Header.Get(InternalSignatureHeader))
	if signature == "" {
		return fmt.Errorf("missing %s", InternalSignatureHeader)
	}

	payloadHash := normalizedPayloadHash(r.Header.Get(InternalContentSHA256Header))
	expected := SignInternalRequest(r.Method, r.URL.Path, nodeID, timestampRaw, payloadHash, m.config.SharedSecret)
	if !strings.EqualFold(signature, expected) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}

func normalizedPayloadHash(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		if strings.EqualFold(trimmed, unsignedPayloadMarker) {
			return unsignedPayloadMarker
		}
		return strings.ToLower(trimmed)
	}
	return unsignedPayloadMarker
}

// SignInternalRequest builds the HMAC signature for an internal request.
func SignInternalRequest(method, path, nodeID, timestamp, payloadHash, secret string) string {
	normalizedHash := normalizedPayloadHash(payloadHash)
	payload := strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(method)),
		strings.TrimSpace(path),
		strings.TrimSpace(nodeID),
		strings.TrimSpace(timestamp),
		normalizedHash,
	}, "\n")

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
