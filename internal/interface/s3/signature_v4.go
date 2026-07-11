package s3

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	signatureV4Algorithm  = "AWS4-HMAC-SHA256"
	signatureV4Terminator = "aws4_request"
	unsignedPayload       = "UNSIGNED-PAYLOAD"
)

var (
	ErrMissingAuthorization     = errors.New("missing authorization")
	ErrUnsupportedAlgorithm     = errors.New("unsupported signature algorithm")
	ErrMalformedAuthorization   = errors.New("malformed authorization")
	ErrInvalidCredentialScope   = errors.New("invalid credential scope")
	ErrInvalidSignedHeaders     = errors.New("invalid signed headers")
	ErrMissingSignedHeader      = errors.New("missing signed header")
	ErrInvalidRequestTime       = errors.New("invalid request time")
	ErrRequestTimeTooSkewed     = errors.New("request time too skewed")
	ErrInvalidPayloadHash       = errors.New("invalid payload hash")
	ErrUnsupportedPayloadHash   = errors.New("unsupported payload hash")
	ErrSignatureMismatch        = errors.New("signature mismatch")
	ErrInvalidCanonicalEncoding = errors.New("invalid canonical encoding")
)

// SignatureV4Config controls validation of header-based AWS Signature Version 4.
type SignatureV4Config struct {
	Region               string
	Service              string
	MaxClockSkew         time.Duration
	AllowUnsignedPayload bool
	Now                  func() time.Time
}

// SignatureV4Result contains the authenticated credential scope and canonical
// values used to validate a request.
type SignatureV4Result struct {
	AccessKeyID      string
	ScopeDate        string
	Region           string
	Service          string
	SignedHeaders    []string
	PayloadHash      string
	RequestTime      time.Time
	CanonicalRequest string
	StringToSign     string
}

type parsedAuthorization struct {
	accessKeyID   string
	scopeDate     string
	region        string
	service       string
	signedHeaders []string
	signature     string
}

// VerifyHeaderSignature validates a header-based Signature V4 request with the
// supplied secret. Credential lookup and authorization are handled separately.
func VerifyHeaderSignature(req *http.Request, secret string, cfg SignatureV4Config) (*SignatureV4Result, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is nil", ErrMalformedAuthorization)
	}
	cfg = normalizeSignatureV4Config(cfg)

	authorization, err := parseAuthorization(req.Header.Get("Authorization"))
	if err != nil {
		return nil, err
	}
	if authorization.region != cfg.Region ||
		authorization.service != cfg.Service ||
		authorization.scopeDate == "" {
		return nil, fmt.Errorf(
			"%w: expected region=%q service=%q, got region=%q service=%q",
			ErrInvalidCredentialScope,
			cfg.Region,
			cfg.Service,
			authorization.region,
			authorization.service,
		)
	}

	requestTime, err := parseRequestTime(req.Header.Get("X-Amz-Date"))
	if err != nil {
		return nil, err
	}
	if authorization.scopeDate != requestTime.Format("20060102") {
		return nil, fmt.Errorf(
			"%w: credential date %q does not match request date %q",
			ErrInvalidCredentialScope,
			authorization.scopeDate,
			requestTime.Format("20060102"),
		)
	}
	if cfg.MaxClockSkew > 0 {
		skew := cfg.Now().UTC().Sub(requestTime)
		if skew < 0 {
			skew = -skew
		}
		if skew > cfg.MaxClockSkew {
			return nil, fmt.Errorf("%w: skew=%s maximum=%s", ErrRequestTimeTooSkewed, skew, cfg.MaxClockSkew)
		}
	}

	payloadHash := strings.TrimSpace(req.Header.Get("X-Amz-Content-Sha256"))
	if err := validatePayloadHash(payloadHash, cfg.AllowUnsignedPayload); err != nil {
		return nil, err
	}

	canonicalRequest, err := buildCanonicalRequest(req, authorization.signedHeaders, payloadHash)
	if err != nil {
		return nil, err
	}
	scope := strings.Join([]string{
		authorization.scopeDate,
		authorization.region,
		authorization.service,
		signatureV4Terminator,
	}, "/")
	stringToSign := strings.Join([]string{
		signatureV4Algorithm,
		requestTime.Format("20060102T150405Z"),
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	expectedSignature := calculateSignature(secret, authorization.scopeDate, authorization.region, authorization.service, stringToSign)
	providedSignature, err := hex.DecodeString(authorization.signature)
	if err != nil || len(providedSignature) != sha256.Size {
		return nil, fmt.Errorf("%w: signature must be 64 hexadecimal characters", ErrMalformedAuthorization)
	}
	expectedBytes, _ := hex.DecodeString(expectedSignature)
	if subtle.ConstantTimeCompare(providedSignature, expectedBytes) != 1 {
		return nil, ErrSignatureMismatch
	}

	return &SignatureV4Result{
		AccessKeyID:      authorization.accessKeyID,
		ScopeDate:        authorization.scopeDate,
		Region:           authorization.region,
		Service:          authorization.service,
		SignedHeaders:    append([]string(nil), authorization.signedHeaders...),
		PayloadHash:      payloadHash,
		RequestTime:      requestTime,
		CanonicalRequest: canonicalRequest,
		StringToSign:     stringToSign,
	}, nil
}

func normalizeSignatureV4Config(cfg SignatureV4Config) SignatureV4Config {
	cfg.Region = strings.TrimSpace(cfg.Region)
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	cfg.Service = strings.TrimSpace(cfg.Service)
	if cfg.Service == "" {
		cfg.Service = "s3"
	}
	if cfg.MaxClockSkew == 0 {
		cfg.MaxClockSkew = 15 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return cfg
}

func parseAuthorization(raw string) (*parsedAuthorization, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrMissingAuthorization
	}
	algorithm, attributes, found := strings.Cut(raw, " ")
	if !found {
		return nil, ErrMalformedAuthorization
	}
	if algorithm != signatureV4Algorithm {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedAlgorithm, algorithm)
	}

	values := make(map[string]string, 3)
	for _, item := range strings.Split(attributes, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return nil, ErrMalformedAuthorization
		}
		key = strings.TrimSpace(key)
		if _, exists := values[key]; exists {
			return nil, ErrMalformedAuthorization
		}
		values[key] = strings.TrimSpace(value)
	}

	credential := values["Credential"]
	signedHeadersRaw := values["SignedHeaders"]
	signature := strings.ToLower(values["Signature"])
	if credential == "" || signedHeadersRaw == "" || signature == "" {
		return nil, ErrMalformedAuthorization
	}

	credentialParts := strings.Split(credential, "/")
	if len(credentialParts) != 5 ||
		credentialParts[0] == "" ||
		credentialParts[1] == "" ||
		credentialParts[2] == "" ||
		credentialParts[3] == "" ||
		credentialParts[4] != signatureV4Terminator {
		return nil, ErrInvalidCredentialScope
	}

	if signedHeadersRaw != strings.ToLower(signedHeadersRaw) {
		return nil, ErrInvalidSignedHeaders
	}
	signedHeaders := strings.Split(signedHeadersRaw, ";")
	if err := validateSignedHeaders(signedHeaders); err != nil {
		return nil, err
	}
	if len(signature) != sha256.Size*2 {
		return nil, fmt.Errorf("%w: signature must be 64 hexadecimal characters", ErrMalformedAuthorization)
	}
	if _, err := hex.DecodeString(signature); err != nil {
		return nil, fmt.Errorf("%w: invalid signature encoding", ErrMalformedAuthorization)
	}

	return &parsedAuthorization{
		accessKeyID:   credentialParts[0],
		scopeDate:     credentialParts[1],
		region:        credentialParts[2],
		service:       credentialParts[3],
		signedHeaders: signedHeaders,
		signature:     signature,
	}, nil
}

func validateSignedHeaders(headers []string) error {
	if len(headers) == 0 {
		return ErrInvalidSignedHeaders
	}
	seen := make(map[string]struct{}, len(headers))
	for _, header := range headers {
		if header == "" || header != strings.ToLower(header) {
			return ErrInvalidSignedHeaders
		}
		if _, ok := seen[header]; ok {
			return ErrInvalidSignedHeaders
		}
		seen[header] = struct{}{}
	}
	if _, ok := seen["host"]; !ok {
		return fmt.Errorf("%w: host is required", ErrInvalidSignedHeaders)
	}
	if _, ok := seen["x-amz-date"]; !ok {
		return fmt.Errorf("%w: x-amz-date is required", ErrInvalidSignedHeaders)
	}
	sorted := append([]string(nil), headers...)
	sort.Strings(sorted)
	for i := range headers {
		if headers[i] != sorted[i] {
			return fmt.Errorf("%w: headers must be sorted", ErrInvalidSignedHeaders)
		}
	}
	return nil
}

func parseRequestTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, ErrInvalidRequestTime
	}
	parsed, err := time.Parse("20060102T150405Z", raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrInvalidRequestTime, err)
	}
	return parsed.UTC(), nil
}

func validatePayloadHash(payloadHash string, allowUnsigned bool) error {
	if payloadHash == "" {
		return ErrInvalidPayloadHash
	}
	if payloadHash == unsignedPayload {
		if !allowUnsigned {
			return fmt.Errorf("%w: %s is not enabled", ErrUnsupportedPayloadHash, unsignedPayload)
		}
		return nil
	}
	if strings.HasPrefix(payloadHash, "STREAMING-") {
		return fmt.Errorf("%w: %s", ErrUnsupportedPayloadHash, payloadHash)
	}
	if len(payloadHash) != sha256.Size*2 {
		return ErrInvalidPayloadHash
	}
	if _, err := hex.DecodeString(payloadHash); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPayloadHash, err)
	}
	return nil
}

func buildCanonicalRequest(req *http.Request, signedHeaders []string, payloadHash string) (string, error) {
	canonicalURI, err := canonicalURI(req)
	if err != nil {
		return "", err
	}
	canonicalQuery, err := canonicalQuery(req.URL.RawQuery)
	if err != nil {
		return "", err
	}
	canonicalHeaders, err := canonicalHeaders(req, signedHeaders)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		payloadHash,
	}, "\n"), nil
}

func canonicalURI(req *http.Request) (string, error) {
	rawPath := req.URL.EscapedPath()
	if rawPath == "" {
		rawPath = "/"
	}
	decoded, err := percentDecode(rawPath)
	if err != nil {
		return "", fmt.Errorf("%w: path: %v", ErrInvalidCanonicalEncoding, err)
	}
	return awsURIEncode(decoded, false), nil
}

func canonicalQuery(rawQuery string) (string, error) {
	if rawQuery == "" {
		return "", nil
	}
	type pair struct {
		key   string
		value string
	}
	items := make([]pair, 0, strings.Count(rawQuery, "&")+1)
	for _, rawItem := range strings.Split(rawQuery, "&") {
		rawKey, rawValue, found := strings.Cut(rawItem, "=")
		if !found {
			rawValue = ""
		}
		key, err := percentDecode(rawKey)
		if err != nil {
			return "", fmt.Errorf("%w: query key: %v", ErrInvalidCanonicalEncoding, err)
		}
		value, err := percentDecode(rawValue)
		if err != nil {
			return "", fmt.Errorf("%w: query value: %v", ErrInvalidCanonicalEncoding, err)
		}
		items = append(items, pair{
			key:   awsURIEncode(key, true),
			value: awsURIEncode(value, true),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].key == items[j].key {
			return items[i].value < items[j].value
		}
		return items[i].key < items[j].key
	})
	encoded := make([]string, 0, len(items))
	for _, item := range items {
		encoded = append(encoded, item.key+"="+item.value)
	}
	return strings.Join(encoded, "&"), nil
}

func canonicalHeaders(req *http.Request, signedHeaders []string) (string, error) {
	var builder strings.Builder
	for _, name := range signedHeaders {
		value, ok := signedHeaderValue(req, name)
		if !ok {
			return "", fmt.Errorf("%w: %s", ErrMissingSignedHeader, name)
		}
		builder.WriteString(name)
		builder.WriteByte(':')
		builder.WriteString(value)
		builder.WriteByte('\n')
	}
	return builder.String(), nil
}

func signedHeaderValue(req *http.Request, name string) (string, bool) {
	switch name {
	case "host":
		host := strings.TrimSpace(req.Host)
		if host == "" && req.URL != nil {
			host = strings.TrimSpace(req.URL.Host)
		}
		if host == "" {
			return "", false
		}
		return normalizeHeaderValue(host), true
	case "content-length":
		if req.ContentLength < 0 {
			return "", false
		}
		return strconv.FormatInt(req.ContentLength, 10), true
	}

	values := req.Header.Values(name)
	if len(values) == 0 {
		return "", false
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		normalized = append(normalized, normalizeHeaderValue(value))
	}
	return strings.Join(normalized, ","), true
}

func normalizeHeaderValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func percentDecode(raw string) ([]byte, error) {
	decoded := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] != '%' {
			decoded = append(decoded, raw[i])
			continue
		}
		if i+2 >= len(raw) {
			return nil, fmt.Errorf("incomplete escape at byte %d", i)
		}
		value, err := hex.DecodeString(raw[i+1 : i+3])
		if err != nil {
			return nil, fmt.Errorf("invalid escape at byte %d", i)
		}
		decoded = append(decoded, value[0])
		i += 2
	}
	return decoded, nil
}

func awsURIEncode(raw []byte, encodeSlash bool) string {
	const upperHex = "0123456789ABCDEF"
	var builder strings.Builder
	builder.Grow(len(raw))
	for _, value := range raw {
		if isUnreserved(value) || (!encodeSlash && value == '/') {
			builder.WriteByte(value)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(upperHex[value>>4])
		builder.WriteByte(upperHex[value&0x0f])
	}
	return builder.String()
}

func isUnreserved(value byte) bool {
	return value >= 'A' && value <= 'Z' ||
		value >= 'a' && value <= 'z' ||
		value >= '0' && value <= '9' ||
		value == '-' || value == '.' || value == '_' || value == '~'
}

func calculateSignature(secret, date, region, service, stringToSign string) string {
	dateKey := hmacSHA256([]byte("AWS4"+secret), date)
	regionKey := hmacSHA256(dateKey, region)
	serviceKey := hmacSHA256(regionKey, service)
	signingKey := hmacSHA256(serviceKey, signatureV4Terminator)
	return hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
