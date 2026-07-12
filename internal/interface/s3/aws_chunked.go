package s3

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// decodeAWSChunkHeader parses the framing used by SigV4 streaming payloads.
func decodeAWSChunkHeader(line string) (int64, string, error) {
	parts := strings.Split(line, ";")
	if len(parts) != 2 || !strings.HasPrefix(parts[1], "chunk-signature=") {
		return 0, "", fmt.Errorf("malformed aws chunk header")
	}
	size, err := strconv.ParseInt(parts[0], 16, 64)
	sig := strings.TrimPrefix(parts[1], "chunk-signature=")
	if err != nil || size < 0 || len(sig) != 64 {
		return 0, "", fmt.Errorf("malformed aws chunk header")
	}
	if _, err := hex.DecodeString(sig); err != nil {
		return 0, "", err
	}
	return size, sig, nil
}

// verifyAWSChunkSignature validates one payload chunk against the previous signature.
func verifyAWSChunkSignature(key []byte, timestamp, scope, previous, signature string, payload []byte) bool {
	h := sha256.Sum256(payload)
	stringToSign := "AWS4-HMAC-SHA256-PAYLOAD\n" + timestamp + "\n" + scope + "\n" + previous + "\n" + hex.EncodeToString(h[:])
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(stringToSign))
	return hmac.Equal(mac.Sum(nil), mustDecodeHex(signature))
}

func mustDecodeHex(value string) []byte { decoded, _ := hex.DecodeString(value); return decoded }

// readAWSChunk reads one framed chunk, returning its decoded payload.
func readAWSChunk(r *bufio.Reader) ([]byte, string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, "", err
	}
	size, sig, err := decodeAWSChunkHeader(strings.TrimSpace(line))
	if err != nil {
		return nil, "", err
	}
	if size > int64(^uint(0)>>1) {
		return nil, "", fmt.Errorf("aws chunk too large")
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, "", err
	}
	if _, err := io.ReadFull(r, make([]byte, 2)); err != nil {
		return nil, "", err
	}
	return bytes.Clone(payload), sig, nil
}
