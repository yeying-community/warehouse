package auth

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// UcanCapability is the normalized internal representation for UCAN capabilities.
// It accepts legacy {resource,action}, standard-ish {with,can}, and att-derived forms.
type UcanCapability struct {
	With        string            `json:"with,omitempty"`
	Can         string            `json:"can,omitempty"`
	Resource    string            `json:"resource,omitempty"`
	Action      string            `json:"action,omitempty"`
	Constraints []json.RawMessage `json:"constraints,omitempty"`
}

type ucanRootProof struct {
	Type string                                `json:"type"`
	Iss  string                                `json:"iss"`
	Aud  string                                `json:"aud"`
	Cap  []json.RawMessage                     `json:"cap"`
	Att  map[string]map[string]json.RawMessage `json:"att,omitempty"`
	Exp  int64                                 `json:"exp"`
	Nbf  *int64                                `json:"nbf,omitempty"`
	Siwe struct {
		Message   string `json:"message"`
		Signature string `json:"signature"`
	} `json:"siwe"`
}

type ucanStatement struct {
	Aud  string                                `json:"aud"`
	Cap  []json.RawMessage                     `json:"cap"`
	Att  map[string]map[string]json.RawMessage `json:"att,omitempty"`
	Exp  int64                                 `json:"exp"`
	Nbf  *int64                                `json:"nbf,omitempty"`
	Caps []UcanCapability                      `json:"-"`
}

type ucanPayload struct {
	Iss  string                                `json:"iss"`
	Aud  string                                `json:"aud"`
	Sub  string                                `json:"sub,omitempty"`
	Cap  []json.RawMessage                     `json:"cap"`
	Att  map[string]map[string]json.RawMessage `json:"att,omitempty"`
	Exp  int64                                 `json:"exp"`
	Nbf  *int64                                `json:"nbf,omitempty"`
	Prf  []json.RawMessage                     `json:"prf"`
	Caps []UcanCapability                      `json:"-"`
}

func (c UcanCapability) normalizedResource() string {
	resource := strings.TrimSpace(c.With)
	if resource == "" {
		resource = strings.TrimSpace(c.Resource)
	}
	return resource
}

func (c UcanCapability) normalizedAction() string {
	action := strings.TrimSpace(c.Can)
	if action == "" {
		action = strings.TrimSpace(c.Action)
	}
	return normalizeActionExpression(action)
}

func newNormalizedCapability(resource, action string, constraints []json.RawMessage) UcanCapability {
	resource = strings.TrimSpace(resource)
	action = normalizeActionExpression(action)
	return UcanCapability{
		With:        resource,
		Can:         action,
		Resource:    resource,
		Action:      action,
		Constraints: constraints,
	}
}

func normalizeActionExpression(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "|", ",")
	parts := strings.Split(raw, ",")
	normalized := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		normalized = append(normalized, part)
	}
	if len(normalized) == 0 {
		return ""
	}
	if len(normalized) == 1 {
		return normalized[0]
	}
	sort.Strings(normalized)
	return strings.Join(normalized, ",")
}

func normalizeConstraintList(raw json.RawMessage) []json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '[' {
		var list []json.RawMessage
		if err := json.Unmarshal(trimmed, &list); err == nil {
			return list
		}
	}
	return []json.RawMessage{json.RawMessage(trimmed)}
}

func parseCapabilityFromObject(raw json.RawMessage) (UcanCapability, bool, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return UcanCapability{}, false, err
	}

	readString := func(keys ...string) string {
		for _, key := range keys {
			valueRaw, ok := obj[key]
			if !ok {
				continue
			}
			var value string
			if err := json.Unmarshal(valueRaw, &value); err == nil && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
		return ""
	}

	resource := readString("with", "resource")
	action := readString("can", "action")
	if resource == "" && action == "" {
		return UcanCapability{}, false, nil
	}

	constraints := normalizeConstraintList(obj["nb"])
	return newNormalizedCapability(resource, action, constraints), true, nil
}

func parseCapabilityArray(rawCaps []json.RawMessage) ([]UcanCapability, error) {
	caps := make([]UcanCapability, 0, len(rawCaps))
	for _, raw := range rawCaps {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		capability, ok, err := parseCapabilityFromObject(trimmed)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		caps = append(caps, capability)
	}
	return caps, nil
}

func parseCapabilitiesFromAtt(att map[string]map[string]json.RawMessage) []UcanCapability {
	if len(att) == 0 {
		return nil
	}
	caps := make([]UcanCapability, 0)
	for resource, actionMap := range att {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			continue
		}
		for action, constraintRaw := range actionMap {
			action = strings.TrimSpace(action)
			if action == "" {
				continue
			}
			constraints := normalizeConstraintList(constraintRaw)
			caps = append(caps, newNormalizedCapability(resource, action, constraints))
		}
	}
	return caps
}

func dedupeCapabilities(caps []UcanCapability) []UcanCapability {
	if len(caps) == 0 {
		return nil
	}
	result := make([]UcanCapability, 0, len(caps))
	seen := make(map[string]struct{}, len(caps))
	for _, cap := range caps {
		resource := cap.normalizedResource()
		action := cap.normalizedAction()
		if resource == "" && action == "" {
			continue
		}
		key := resource + "#" + action
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, newNormalizedCapability(resource, action, cap.Constraints))
	}
	return result
}

func extractCapabilities(rawCaps []json.RawMessage, att map[string]map[string]json.RawMessage) ([]UcanCapability, error) {
	capsFromArray, err := parseCapabilityArray(rawCaps)
	if err != nil {
		return nil, err
	}
	caps := append(capsFromArray, parseCapabilitiesFromAtt(att)...)
	return dedupeCapabilities(caps), nil
}

// UcanVerifier validates UCAN invocation tokens.
type UcanVerifier struct {
	enabled        bool
	audience       string
	requiredCaps   []UcanCapability
	trustedIssuers map[string]struct{}
	logger         *zap.Logger
}

// NewUcanVerifier creates a verifier for UCAN invocations.
func NewUcanVerifier(enabled bool, audience string, requiredCaps []UcanCapability, trustedIssuerDIDs []string, logger *zap.Logger) *UcanVerifier {
	caps := make([]UcanCapability, 0, len(requiredCaps))
	for _, cap := range requiredCaps {
		normalized, ok := normalizeRequiredCapability(cap)
		if !ok {
			continue
		}
		caps = append(caps, normalized)
	}

	return &UcanVerifier{
		enabled:        enabled,
		audience:       strings.TrimSpace(audience),
		requiredCaps:   dedupeCapabilities(caps),
		trustedIssuers: normalizeTrustedIssuerDIDs(trustedIssuerDIDs),
		logger:         logger,
	}
}

func normalizeTrustedIssuerDIDs(input []string) map[string]struct{} {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(input))
	for _, raw := range input {
		normalized := strings.TrimSpace(raw)
		if normalized == "" {
			continue
		}
		result[normalized] = struct{}{}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeEthereumAddress(input string) string {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if len(normalized) != 42 || !strings.HasPrefix(normalized, "0x") {
		return ""
	}
	decoded, err := hexutil.Decode(normalized)
	if err != nil || len(decoded) != 20 {
		return ""
	}
	return normalized
}

func (v *UcanVerifier) isTrustedIssuer(issuer string) bool {
	if v == nil || len(v.trustedIssuers) == 0 {
		return false
	}
	_, ok := v.trustedIssuers[strings.TrimSpace(issuer)]
	return ok
}

// Enabled returns true when UCAN verification is enabled.
func (v *UcanVerifier) Enabled() bool {
	if v == nil {
		return false
	}
	return v.enabled
}

// IsUcanToken checks if the token looks like a UCAN JWS.
func (v *UcanVerifier) IsUcanToken(token string) bool {
	return isUcanToken(token)
}

// VerifyInvocation verifies a UCAN invocation and returns the issuer address.
func (v *UcanVerifier) VerifyInvocation(token string) (string, error) {
	if v == nil || !v.enabled {
		return "", fmt.Errorf("UCAN verification disabled")
	}

	payload, exp, err := verifyUcanJws(token)
	if err != nil {
		v.debug("ucan jws verification failed", zap.Error(err))
		return "", err
	}

	if v.audience != "" && payload.Aud != v.audience {
		return "", fmt.Errorf("UCAN audience mismatch")
	}
	if len(v.requiredCaps) > 0 && !capsAllow(payload.Caps, v.requiredCaps) {
		if v.logger != nil {
			v.logger.Warn("ucan capability denied",
				zap.String("required_caps", formatCaps(v.requiredCaps)),
				zap.String("provided_caps", formatCaps(payload.Caps)),
				zap.String("audience", payload.Aud),
				zap.String("issuer", payload.Iss),
			)
		}
		return "", fmt.Errorf("UCAN capability denied")
	}

	if v.isTrustedIssuer(payload.Iss) {
		address := normalizeEthereumAddress(payload.Sub)
		if address == "" {
			return "", fmt.Errorf("invalid UCAN subject")
		}
		return address, nil
	}

	iss, err := verifyProofChain(payload.Iss, payload.Caps, exp, payload.Prf)
	if err != nil {
		v.debug("ucan proof chain verification failed", zap.Error(err))
		return "", err
	}

	const didPrefix = "did:pkh:eth:"
	if !strings.HasPrefix(iss, didPrefix) {
		return "", fmt.Errorf("UCAN issuer is not an ethereum DID")
	}
	address := strings.TrimPrefix(iss, didPrefix)
	return strings.ToLower(address), nil
}

// BuildRequiredUcanCaps builds a capability list from resource/action settings.
func BuildRequiredUcanCaps(resource, action string, additional []UcanCapability) []UcanCapability {
	caps := make([]UcanCapability, 0, len(additional)+1)

	resource = strings.TrimSpace(resource)
	action = strings.ToLower(strings.TrimSpace(action))
	if resource == "" && action == "" {
		// no-op
	} else {
		if resource == "" {
			resource = "*"
		}
		if action == "" {
			action = "*"
		}
		caps = append(caps, newNormalizedCapability(resource, action, nil))
	}

	for _, cap := range additional {
		normalized, ok := normalizeRequiredCapability(cap)
		if !ok {
			continue
		}
		caps = append(caps, normalized)
	}
	return dedupeCapabilities(caps)
}

func normalizeRequiredCapability(cap UcanCapability) (UcanCapability, bool) {
	resource := cap.normalizedResource()
	action := cap.normalizedAction()
	if resource == "" && action == "" {
		return UcanCapability{}, false
	}
	if resource == "" {
		resource = "*"
	}
	if action == "" {
		action = "*"
	}
	return newNormalizedCapability(resource, action, nil), true
}

func parseUcanCaps(token string) ([]UcanCapability, error) {
	_, payload, _, _, err := decodeUcanToken(token)
	if err != nil {
		return nil, err
	}
	return payload.Caps, nil
}

type appCapExtraction struct {
	AppCaps        map[string][]string
	HasAppCaps     bool
	InvalidAppCaps []string
}

func extractAppCapsFromCaps(caps []UcanCapability, resourcePrefix string) appCapExtraction {
	prefix := strings.TrimSpace(resourcePrefix)
	if prefix == "" {
		prefix = "app:"
	}
	actionSets := make(map[string]map[string]struct{}, len(caps))
	invalid := make([]string, 0)
	hasAppCaps := false
	for _, cap := range caps {
		resource := strings.TrimSpace(cap.normalizedResource())
		if !strings.HasPrefix(resource, prefix) {
			continue
		}
		hasAppCaps = true
		appID, ok := extractAppIDFromResource(resource, prefix)
		action := strings.ToLower(strings.TrimSpace(cap.normalizedAction()))
		if !ok {
			invalid = append(invalid, fmt.Sprintf("%s#%s", resource, action))
			continue
		}
		if _, ok := actionSets[appID]; !ok {
			actionSets[appID] = make(map[string]struct{})
		}
		if action == "" {
			continue
		}
		actionSets[appID][action] = struct{}{}
	}

	result := make(map[string][]string, len(actionSets))
	for appID, actions := range actionSets {
		list := make([]string, 0, len(actions))
		for action := range actions {
			list = append(list, action)
		}
		sort.Strings(list)
		result[appID] = list
	}
	sort.Strings(invalid)
	return appCapExtraction{
		AppCaps:        result,
		HasAppCaps:     hasAppCaps,
		InvalidAppCaps: invalid,
	}
}

func extractAppIDFromResource(resource, prefix string) (string, bool) {
	if !strings.HasPrefix(resource, prefix) {
		return "", false
	}
	remainder := strings.TrimSpace(strings.TrimPrefix(resource, prefix))
	if remainder == "" {
		return "", false
	}

	parts := strings.Split(remainder, ":")
	switch len(parts) {
	case 1:
		appID := strings.TrimSpace(parts[0])
		if !isValidAppResourceAppID(appID) {
			return "", false
		}
		return appID, true
	case 2:
		scope := strings.ToLower(strings.TrimSpace(parts[0]))
		appID := strings.TrimSpace(parts[1])
		if scope != "all" {
			return "", false
		}
		if !isValidAppResourceAppID(appID) {
			return "", false
		}
		return appID, true
	default:
		return "", false
	}
}

func isValidAppResourceAppID(appID string) bool {
	if appID == "" || strings.Contains(appID, "*") {
		return false
	}
	return isValidAppID(appID)
}

func isValidAppID(appID string) bool {
	if appID == "" {
		return false
	}
	for i := 0; i < len(appID); i++ {
		c := appID[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.':
		default:
			return false
		}
	}
	return true
}

func formatCaps(caps []UcanCapability) string {
	if len(caps) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(caps))
	for _, cap := range caps {
		resource := strings.TrimSpace(cap.normalizedResource())
		action := strings.TrimSpace(cap.normalizedAction())
		if resource == "" {
			resource = "*"
		}
		if action == "" {
			action = "*"
		}
		parts = append(parts, fmt.Sprintf("%s#%s", resource, action))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func (v *UcanVerifier) debug(msg string, fields ...zap.Field) {
	if v == nil || v.logger == nil {
		return
	}
	v.logger.Debug(msg, fields...)
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}

func base64UrlDecode(input string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(input)
}

func base58Decode(input string) ([]byte, error) {
	bytes := []byte{0}
	for _, r := range input {
		index := strings.IndexRune(base58Alphabet, r)
		if index < 0 {
			return nil, fmt.Errorf("invalid base58 character")
		}
		carry := index
		for i := 0; i < len(bytes); i++ {
			carry += int(bytes[i]) * 58
			bytes[i] = byte(carry & 0xff)
			carry >>= 8
		}
		for carry > 0 {
			bytes = append(bytes, byte(carry&0xff))
			carry >>= 8
		}
	}
	zeros := 0
	for zeros < len(input) && input[zeros] == '1' {
		zeros++
	}
	output := make([]byte, zeros+len(bytes))
	for i := 0; i < zeros; i++ {
		output[i] = 0
	}
	for i := 0; i < len(bytes); i++ {
		output[len(output)-1-i] = bytes[i]
	}
	return output, nil
}

func didKeyToPublicKey(did string) ([]byte, error) {
	if !strings.HasPrefix(did, "did:key:z") {
		return nil, fmt.Errorf("invalid did:key format")
	}
	decoded, err := base58Decode(strings.TrimPrefix(did, "did:key:z"))
	if err != nil {
		return nil, err
	}
	if len(decoded) < 3 || decoded[0] != 0xed || decoded[1] != 0x01 {
		return nil, fmt.Errorf("unsupported did:key type")
	}
	key := decoded[2:]
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid ed25519 public key size")
	}
	return key, nil
}

func normalizeEpochMillis(value int64) int64 {
	if value == 0 {
		return 0
	}
	if value < 1e12 {
		return value * 1000
	}
	return value
}

func matchPattern(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return value == ""
	}
	pattern = strings.ReplaceAll(pattern, "|", ",")
	parts := strings.Split(pattern, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if matchSinglePattern(part, value) {
			return true
		}
	}
	return false
}

func matchSinglePattern(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == value
}

func capsAllow(available []UcanCapability, required []UcanCapability) bool {
	if len(required) == 0 {
		return true
	}
	if len(available) == 0 {
		return false
	}
	for _, req := range required {
		reqResource := strings.TrimSpace(req.normalizedResource())
		reqAction := strings.TrimSpace(req.normalizedAction())
		if reqResource == "" {
			reqResource = "*"
		}
		if reqAction == "" {
			reqAction = "*"
		}

		matched := false
		for _, cap := range available {
			capResource := strings.TrimSpace(cap.normalizedResource())
			capAction := strings.TrimSpace(cap.normalizedAction())
			resourceMatched := matchPattern(reqResource, capResource) || matchPattern(capResource, reqResource)
			actionMatched := matchPattern(reqAction, capAction) || matchPattern(capAction, reqAction)
			if resourceMatched && actionMatched {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func extractUcanStatement(message string) (*ucanStatement, error) {
	lines := strings.Split(message, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), "UCAN-AUTH") {
			jsonPart := strings.TrimSpace(strings.TrimPrefix(trimmed, "UCAN-AUTH"))
			jsonPart = strings.TrimSpace(strings.TrimPrefix(jsonPart, ":"))
			var statement ucanStatement
			if err := json.Unmarshal([]byte(jsonPart), &statement); err != nil {
				return nil, err
			}
			return &statement, nil
		}
	}
	return nil, fmt.Errorf("missing UCAN statement")
}

func recoverAddress(message string, signature string) (string, error) {
	sig, err := hexutil.Decode(signature)
	if err != nil {
		return "", err
	}
	if len(sig) != 65 {
		return "", fmt.Errorf("invalid signature length")
	}
	if sig[64] >= 27 {
		sig[64] -= 27
	}

	hash := accounts.TextHash([]byte(message))
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return "", err
	}
	return strings.ToLower(crypto.PubkeyToAddress(*pubKey).Hex()), nil
}

func verifyRootProof(root ucanRootProof) (ucanStatement, string, error) {
	if root.Type != "siwe" || root.Siwe.Message == "" || root.Siwe.Signature == "" {
		return ucanStatement{}, "", fmt.Errorf("invalid root proof")
	}

	recovered, err := recoverAddress(root.Siwe.Message, root.Siwe.Signature)
	if err != nil {
		return ucanStatement{}, "", err
	}
	iss := "did:pkh:eth:" + recovered
	if root.Iss != "" && root.Iss != iss {
		return ucanStatement{}, "", fmt.Errorf("root issuer mismatch")
	}

	statement, err := extractUcanStatement(root.Siwe.Message)
	if err != nil {
		return ucanStatement{}, "", err
	}
	statementClaimsDeclared := len(statement.Cap) > 0 || len(statement.Att) > 0
	statementCaps, err := extractCapabilities(statement.Cap, statement.Att)
	if err != nil {
		return ucanStatement{}, "", err
	}
	rootCaps, err := extractCapabilities(root.Cap, root.Att)
	if err != nil {
		return ucanStatement{}, "", err
	}
	effectiveCaps := statementCaps
	if !statementClaimsDeclared {
		effectiveCaps = rootCaps
	}

	aud := statement.Aud
	if aud == "" {
		aud = root.Aud
	}
	exp := normalizeEpochMillis(statement.Exp)
	if exp == 0 {
		exp = normalizeEpochMillis(root.Exp)
	}
	if aud == "" || exp == 0 || len(effectiveCaps) == 0 {
		return ucanStatement{}, "", fmt.Errorf("invalid root claims")
	}
	if root.Aud != "" && root.Aud != aud {
		return ucanStatement{}, "", fmt.Errorf("root audience mismatch")
	}

	statement.Aud = aud
	statement.Exp = exp
	statement.Caps = effectiveCaps
	if statement.Nbf != nil {
		nbf := normalizeEpochMillis(*statement.Nbf)
		statement.Nbf = &nbf
	} else if root.Nbf != nil {
		nbf := normalizeEpochMillis(*root.Nbf)
		statement.Nbf = &nbf
	}

	nowMs := nowMillis()
	if statement.Nbf != nil && nowMs < *statement.Nbf {
		return ucanStatement{}, "", fmt.Errorf("root not active")
	}
	if nowMs > exp {
		return ucanStatement{}, "", fmt.Errorf("root expired")
	}

	return *statement, iss, nil
}

func decodeUcanToken(token string) (map[string]interface{}, ucanPayload, []byte, string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ucanPayload{}, nil, "", fmt.Errorf("invalid UCAN token")
	}
	headerBytes, err := base64UrlDecode(parts[0])
	if err != nil {
		return nil, ucanPayload{}, nil, "", err
	}
	payloadBytes, err := base64UrlDecode(parts[1])
	if err != nil {
		return nil, ucanPayload{}, nil, "", err
	}
	sig, err := base64UrlDecode(parts[2])
	if err != nil {
		return nil, ucanPayload{}, nil, "", err
	}

	var header map[string]interface{}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, ucanPayload{}, nil, "", err
	}
	var payload ucanPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, ucanPayload{}, nil, "", err
	}
	caps, err := extractCapabilities(payload.Cap, payload.Att)
	if err != nil {
		return nil, ucanPayload{}, nil, "", err
	}
	payload.Caps = caps
	return header, payload, sig, parts[0] + "." + parts[1], nil
}

func verifyUcanJws(token string) (ucanPayload, int64, error) {
	header, payload, sig, signingInput, err := decodeUcanToken(token)
	if err != nil {
		return ucanPayload{}, 0, err
	}
	if alg, ok := header["alg"].(string); ok && alg != "EdDSA" {
		return ucanPayload{}, 0, fmt.Errorf("unsupported UCAN alg")
	}

	rawKey, err := didKeyToPublicKey(payload.Iss)
	if err != nil {
		return ucanPayload{}, 0, err
	}
	if !ed25519.Verify(rawKey, []byte(signingInput), sig) {
		return ucanPayload{}, 0, fmt.Errorf("invalid UCAN signature")
	}

	exp := normalizeEpochMillis(payload.Exp)
	nbf := int64(0)
	if payload.Nbf != nil {
		nbf = normalizeEpochMillis(*payload.Nbf)
	}
	nowMs := nowMillis()
	if nbf != 0 && nowMs < nbf {
		return ucanPayload{}, 0, fmt.Errorf("UCAN not active")
	}
	if exp != 0 && nowMs > exp {
		return ucanPayload{}, 0, fmt.Errorf("UCAN expired")
	}

	return payload, exp, nil
}

func verifyProofChain(currentDid string, required []UcanCapability, requiredExp int64, proofs []json.RawMessage) (string, error) {
	if len(proofs) == 0 {
		return "", fmt.Errorf("missing UCAN proof chain")
	}
	first := proofs[0]
	if len(first) > 0 && first[0] == '"' {
		var token string
		if err := json.Unmarshal(first, &token); err != nil {
			return "", err
		}
		payload, proofExp, err := verifyUcanJws(token)
		if err != nil {
			return "", err
		}
		if payload.Aud != currentDid {
			return "", fmt.Errorf("UCAN audience mismatch")
		}
		if !capsAllow(payload.Caps, required) {
			return "", fmt.Errorf("UCAN capability denied")
		}
		if proofExp != 0 && requiredExp != 0 && proofExp < requiredExp {
			return "", fmt.Errorf("UCAN proof expired")
		}
		nextProofs := payload.Prf
		if len(nextProofs) == 0 && len(proofs) > 1 {
			nextProofs = proofs[1:]
		}
		return verifyProofChain(payload.Iss, payload.Caps, proofExp, nextProofs)
	}

	var root ucanRootProof
	if err := json.Unmarshal(first, &root); err != nil {
		return "", err
	}
	statement, iss, err := verifyRootProof(root)
	if err != nil {
		return "", err
	}
	if statement.Aud != currentDid {
		return "", fmt.Errorf("root audience mismatch")
	}
	if !capsAllow(statement.Caps, required) {
		return "", fmt.Errorf("root capability denied")
	}
	if requiredExp != 0 && statement.Exp < requiredExp {
		return "", fmt.Errorf("root expired")
	}
	return iss, nil
}

func isUcanToken(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return false
	}
	headerBytes, err := base64UrlDecode(parts[0])
	if err != nil {
		return false
	}
	var header map[string]interface{}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return false
	}
	if typ, ok := header["typ"].(string); ok && typ == "UCAN" {
		return true
	}
	if alg, ok := header["alg"].(string); ok && alg == "EdDSA" {
		return true
	}
	return false
}
