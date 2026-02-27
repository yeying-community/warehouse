package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// UcanCapability defines an action permitted on a resource.
type UcanCapability struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

type ucanRootProof struct {
	Type string           `json:"type"`
	Iss  string           `json:"iss"`
	Aud  string           `json:"aud"`
	Cap  []UcanCapability `json:"cap"`
	Exp  int64            `json:"exp"`
	Nbf  *int64           `json:"nbf,omitempty"`
	Siwe struct {
		Message   string `json:"message"`
		Signature string `json:"signature"`
	} `json:"siwe"`
}

type ucanStatement struct {
	Aud string           `json:"aud"`
	Cap []UcanCapability `json:"cap"`
	Exp int64            `json:"exp"`
	Nbf *int64           `json:"nbf,omitempty"`
}

type ucanPayload struct {
	Iss string            `json:"iss"`
	Aud string            `json:"aud"`
	Cap []UcanCapability  `json:"cap"`
	Exp int64             `json:"exp"`
	Nbf *int64            `json:"nbf,omitempty"`
	Prf []json.RawMessage `json:"prf"`
}

// UcanVerifier validates UCAN invocation tokens.
type UcanVerifier struct {
	enabled      bool
	audience     string
	requiredCaps []UcanCapability
	logger       *zap.Logger
}

// NewUcanVerifier creates a verifier for UCAN invocations.
func NewUcanVerifier(enabled bool, audience string, requiredCaps []UcanCapability, logger *zap.Logger) *UcanVerifier {
	caps := make([]UcanCapability, 0, len(requiredCaps))
	for _, cap := range requiredCaps {
		if strings.TrimSpace(cap.Resource) == "" && strings.TrimSpace(cap.Action) == "" {
			continue
		}
		caps = append(caps, cap)
	}

	return &UcanVerifier{
		enabled:      enabled,
		audience:     strings.TrimSpace(audience),
		requiredCaps: caps,
		logger:       logger,
	}
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
	if len(v.requiredCaps) > 0 && !capsAllow(payload.Cap, v.requiredCaps) {
		if v.logger != nil {
			v.logger.Warn("ucan capability denied",
				zap.String("required_caps", formatCaps(v.requiredCaps)),
				zap.String("provided_caps", formatCaps(payload.Cap)),
				zap.String("audience", payload.Aud),
				zap.String("issuer", payload.Iss),
			)
		}
		return "", fmt.Errorf("UCAN capability denied")
	}

	iss, err := verifyProofChain(payload.Iss, payload.Cap, exp, payload.Prf)
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
func BuildRequiredUcanCaps(resource, action string) []UcanCapability {
	resource = strings.TrimSpace(resource)
	action = strings.TrimSpace(action)
	if resource == "" && action == "" {
		return nil
	}
	if resource == "" {
		resource = "*"
	}
	if action == "" {
		action = "*"
	}
	return []UcanCapability{{Resource: resource, Action: action}}
}

func parseUcanCaps(token string) ([]UcanCapability, error) {
	_, payload, _, _, err := decodeUcanToken(token)
	if err != nil {
		return nil, err
	}
	return payload.Cap, nil
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
		resource := strings.TrimSpace(cap.Resource)
		if !strings.HasPrefix(resource, prefix) {
			continue
		}
		hasAppCaps = true
		appID := strings.TrimSpace(strings.TrimPrefix(resource, prefix))
		if appID == "" || strings.Contains(appID, "*") {
			invalid = append(invalid, fmt.Sprintf("%s#%s", resource, strings.TrimSpace(cap.Action)))
			continue
		}
		if !isValidAppID(appID) {
			invalid = append(invalid, fmt.Sprintf("%s#%s", resource, strings.TrimSpace(cap.Action)))
			continue
		}
		if _, ok := actionSets[appID]; !ok {
			actionSets[appID] = make(map[string]struct{})
		}
		action := strings.ToLower(strings.TrimSpace(cap.Action))
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
		result[appID] = list
	}
	return appCapExtraction{
		AppCaps:        result,
		HasAppCaps:     hasAppCaps,
		InvalidAppCaps: invalid,
	}
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
		resource := strings.TrimSpace(cap.Resource)
		action := strings.TrimSpace(cap.Action)
		if resource == "" {
			resource = "*"
		}
		if action == "" {
			action = "*"
		}
		parts = append(parts, fmt.Sprintf("%s#%s", resource, action))
	}
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
	if len(available) == 0 {
		return false
	}
	for _, req := range required {
		matched := false
		for _, cap := range available {
			resourceMatched := matchPattern(req.Resource, cap.Resource) || matchPattern(cap.Resource, req.Resource)
			actionMatched := matchPattern(req.Action, cap.Action) || matchPattern(cap.Action, req.Action)
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

	aud := statement.Aud
	if aud == "" {
		aud = root.Aud
	}
	exp := normalizeEpochMillis(statement.Exp)
	if exp == 0 {
		exp = normalizeEpochMillis(root.Exp)
	}
	if aud == "" || exp == 0 || (len(statement.Cap) == 0 && len(root.Cap) == 0) {
		return ucanStatement{}, "", fmt.Errorf("invalid root claims")
	}
	if root.Aud != "" && root.Aud != aud {
		return ucanStatement{}, "", fmt.Errorf("root audience mismatch")
	}

	cap := statement.Cap
	if len(cap) == 0 {
		cap = root.Cap
	}
	statement.Aud = aud
	statement.Exp = exp
	statement.Cap = cap
	if statement.Nbf == nil && root.Nbf != nil {
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
		if !capsAllow(payload.Cap, required) {
			return "", fmt.Errorf("UCAN capability denied")
		}
		if proofExp != 0 && requiredExp != 0 && proofExp < requiredExp {
			return "", fmt.Errorf("UCAN proof expired")
		}
		nextProofs := payload.Prf
		if len(nextProofs) == 0 && len(proofs) > 1 {
			nextProofs = proofs[1:]
		}
		return verifyProofChain(payload.Iss, payload.Cap, proofExp, nextProofs)
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
	if !capsAllow(statement.Cap, required) {
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
