package s3

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/s3credential"
	"github.com/yeying-community/warehouse/internal/domain/s3multipart"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
)

// Server is the independently bound S3 HTTP endpoint.
// Object operations are added behind this boundary in the next stage.
type Server struct {
	config     config.S3Config
	httpServer *http.Server
	logger     *zap.Logger
	resolver   CredentialResolver
	objects    *service.ObjectService
	users      user.Repository
	multipart  *service.MultipartService
}

func NewServer(cfg config.S3Config, resolver CredentialResolver, objects *service.ObjectService, users user.Repository, multipart *service.MultipartService, logger *zap.Logger) *Server {
	return &Server{config: cfg, resolver: resolver, objects: objects, users: users, multipart: multipart, logger: logger}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		credential, err := s.authenticate(req)
		if err != nil {
			s.writeError(w, http.StatusForbidden, "AccessDenied", err.Error())
			return
		}
		if req.URL.Path == "/" || req.URL.Path == "" {
			s.handleListBuckets(w, credential)
			return
		}
		s.handleObject(w, req, credential)
	})

	addr := fmt.Sprintf("%s:%d", s.config.Address, s.config.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}
	s.logger.Info("starting s3 http server", zap.String("address", addr), zap.String("region", s.config.Region), zap.Bool("tls", s.config.TLS))
	if s.config.TLS {
		if err := s.httpServer.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("failed to start s3 server: %w", err)
		}
		return nil
	}
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start s3 server: %w", err)
	}
	return nil
}

type listAllMyBucketsResult struct {
	XMLName xml.Name     `xml:"ListAllMyBucketsResult"`
	Buckets []bucketInfo `xml:"Buckets>Bucket"`
}

type bucketInfo struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

func (s *Server) handleListBuckets(w http.ResponseWriter, credential *s3credential.Credential) {
	now := time.Now().UTC().Format(time.RFC3339)
	response := listAllMyBucketsResult{Buckets: make([]bucketInfo, 0, 3)}
	rootPath := "/"
	if credential != nil {
		rootPath = credential.RootPath
	}
	for _, name := range visibleS3Buckets(rootPath) {
		response.Buckets = append(response.Buckets, bucketInfo{Name: name, CreationDate: now})
	}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(response)
}

func visibleS3Buckets(rootPath string) []string {
	rootPath = path.Clean("/" + strings.TrimSpace(strings.ReplaceAll(rootPath, "\\", "/")))
	switch {
	case rootPath == "/":
		return []string{"personal", "apps", "services"}
	case rootPath == "/personal" || strings.HasPrefix(rootPath, "/personal/"):
		return []string{"personal"}
	case rootPath == "/apps" || strings.HasPrefix(rootPath, "/apps/"):
		return []string{"apps"}
	case rootPath == "/services" || strings.HasPrefix(rootPath, "/services/"):
		return []string{"services"}
	default:
		return nil
	}
}

func (s *Server) authenticate(req *http.Request) (*s3credential.Credential, error) {
	if s.resolver == nil {
		return nil, s3credential.ErrNotFound
	}
	accessKeyID, err := AccessKeyIDFromAuthorization(req.Header.Get("Authorization"))
	if err != nil {
		return nil, err
	}
	credential, err := s.resolver.Resolve(req.Context(), accessKeyID)
	if err != nil {
		return nil, err
	}
	result, err := VerifyHeaderSignature(req, credential.Secret, SignatureV4Config{
		Region:               s.config.Region,
		Service:              "s3",
		AllowUnsignedPayload: allowUnsignedPayload(req),
	})
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(result.PayloadHash, "STREAMING-") {
		req.Body = io.NopCloser(newAWSChunkedReader(req.Body, result.SigningKey, req.Header.Get("X-Amz-Date"), result.ScopeDate+"/"+result.Region+"/"+result.Service+"/aws4_request", result.Signature))
	}
	return credential, nil
}

func allowUnsignedPayload(req *http.Request) bool {
	if req == nil {
		return false
	}
	if req.TLS != nil {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(req.Header.Get("X-Forwarded-Proto")), "https") {
		return true
	}
	contentLength := req.ContentLength
	if contentLength < 0 {
		contentLength = 0
	}
	return contentLength == 0
}

func (s *Server) handleObject(w http.ResponseWriter, req *http.Request, credential *s3credential.Credential) {
	if s.objects == nil {
		s.writeError(w, http.StatusNotImplemented, "NotImplemented", "object service is not configured")
		return
	}
	bucket, key, ok := splitObjectPath(req.URL.Path)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "InvalidURI", "invalid bucket or object path")
		return
	}
	if s.users == nil {
		s.writeError(w, http.StatusInternalServerError, "InternalError", "user repository is not configured")
		return
	}
	owner, err := s.users.FindByID(req.Context(), credential.OwnerUserID)
	if err != nil {
		s.writeError(w, http.StatusForbidden, "AccessDenied", "credential owner not found")
		return
	}
	userDirectory := owner.Directory
	requestedPath := "/" + bucket
	if key != "" {
		requestedPath += "/" + key
	}
	if !s.pathAllowed(credential.RootPath, requestedPath) {
		s.writeError(w, http.StatusForbidden, "AccessDenied", "credential is not bound to this path")
		return
	}
	query := req.URL.Query()
	if req.Method == http.MethodPost && query.Has("uploads") {
		s.handleCreateMultipart(w, req, credential, owner, bucket, key)
		return
	}
	if req.Method == http.MethodPost && query.Has("delete") {
		s.handleDeleteObjects(w, req, credential, owner, bucket, key)
		return
	}
	if req.Method == http.MethodPost && query.Get("uploadId") != "" {
		s.handleCompleteMultipart(w, req, credential, owner, query.Get("uploadId"))
		return
	}
	if req.Method == http.MethodPut && query.Get("uploadId") != "" && query.Get("partNumber") != "" {
		s.handleUploadPart(w, req, credential, owner, query.Get("uploadId"), query.Get("partNumber"))
		return
	}
	if req.Method == http.MethodDelete && query.Get("uploadId") != "" {
		s.handleAbortMultipart(w, req, owner, query.Get("uploadId"))
		return
	}
	switch req.Method {
	case http.MethodGet:
		if !hasS3Permission(credential.Permissions, "read") {
			s.writeError(w, http.StatusForbidden, "AccessDenied", "read permission is required")
			return
		}
		if key == "" {
			s.handleList(w, req, credential, userDirectory, bucket)
			return
		}
		file, info, err := s.objects.Open(req.Context(), userDirectory, bucket, key)
		if err != nil {
			s.writeObjectError(w, err)
			return
		}
		defer file.Close()
		setObjectHeaders(w, info)
		http.ServeContent(w, req, key, info.ModifiedAt, file)
	case http.MethodHead:
		if !hasS3Permission(credential.Permissions, "read") {
			s.writeError(w, http.StatusForbidden, "AccessDenied", "read permission is required")
			return
		}
		if key == "" {
			if _, err := s.objects.Stat(req.Context(), userDirectory, bucket, ""); err != nil {
				s.writeObjectError(w, err)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		info, err := s.objects.Stat(req.Context(), userDirectory, bucket, key)
		if err != nil {
			s.writeObjectError(w, err)
			return
		}
		setObjectHeaders(w, info)
	case http.MethodPut:
		permission := "create"
		if _, statErr := s.objects.Stat(req.Context(), userDirectory, bucket, key); statErr == nil {
			permission = "update"
		}
		if !hasS3Permission(credential.Permissions, permission) {
			s.writeError(w, http.StatusForbidden, "AccessDenied", permission+" permission is required")
			return
		}
		if key == "" {
			if err := s.objects.EnsureBucket(req.Context(), userDirectory, bucket); err != nil {
				s.writeObjectError(w, err)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		info, err := s.objects.PutForUserWithOptions(req.Context(), owner, bucket, key, req.Body, service.ObjectWriteOptions{
			ExpectedMD5:    req.Header.Get("Content-MD5"),
			ExpectedSHA256: req.Header.Get("X-Amz-Checksum-Sha256"),
			ExpectedCRC32:  req.Header.Get("X-Amz-Checksum-Crc32"),
			ContentType:    req.Header.Get("Content-Type"),
		})
		if err != nil {
			s.writeObjectError(w, err)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf("%q", info.ETag))
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		if !hasS3Permission(credential.Permissions, "delete") {
			s.writeError(w, http.StatusForbidden, "AccessDenied", "delete permission is required")
			return
		}
		if err := s.objects.DeleteForUser(req.Context(), owner, bucket, key); err != nil {
			s.writeObjectError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.writeError(w, http.StatusNotImplemented, "NotImplemented", "operation is not implemented")
	}
}

func (s *Server) handleCreateMultipart(w http.ResponseWriter, req *http.Request, credential *s3credential.Credential, owner *user.User, bucket, key string) {
	if s.multipart == nil || !hasS3Permission(credential.Permissions, "create") {
		s.writeError(w, http.StatusForbidden, "AccessDenied", "create permission is required")
		return
	}
	upload, err := s.multipart.Create(req.Context(), owner, bucket, key, req.Header.Get("Content-Type"))
	if err != nil {
		s.writeObjectError(w, err)
		return
	}
	response := struct {
		XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
		Bucket   string   `xml:"Bucket"`
		Key      string   `xml:"Key"`
		UploadID string   `xml:"UploadId"`
	}{Bucket: bucket, Key: key, UploadID: upload.ID}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(response)
}

type deleteObjectsRequest struct {
	Quiet   bool                  `xml:"Quiet"`
	Objects []deleteObjectRequest `xml:"Object"`
}

type deleteObjectRequest struct {
	Key string `xml:"Key"`
}

type deleteObjectsResult struct {
	XMLName xml.Name            `xml:"DeleteResult"`
	Deleted []deletedObject     `xml:"Deleted,omitempty"`
	Errors  []deleteObjectError `xml:"Error,omitempty"`
}

type deletedObject struct {
	Key string `xml:"Key"`
}

type deleteObjectError struct {
	Key     string `xml:"Key"`
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func (s *Server) handleDeleteObjects(w http.ResponseWriter, req *http.Request, credential *s3credential.Credential, owner *user.User, bucket, key string) {
	if key != "" {
		s.writeError(w, http.StatusBadRequest, "InvalidRequest", "DeleteObjects must target a bucket")
		return
	}
	if !hasS3Permission(credential.Permissions, "delete") {
		s.writeError(w, http.StatusForbidden, "AccessDenied", "delete permission is required")
		return
	}
	var payload deleteObjectsRequest
	if err := xml.NewDecoder(req.Body).Decode(&payload); err != nil {
		s.writeError(w, http.StatusBadRequest, "MalformedXML", "invalid delete request body")
		return
	}
	result := deleteObjectsResult{}
	for _, item := range payload.Objects {
		objectKey := strings.TrimSpace(item.Key)
		if objectKey == "" {
			result.Errors = append(result.Errors, deleteObjectError{Code: "InvalidRequest", Message: "object key is required"})
			continue
		}
		requestedPath := "/" + bucket + "/" + strings.TrimPrefix(objectKey, "/")
		if !s.pathAllowed(credential.RootPath, requestedPath) {
			result.Errors = append(result.Errors, deleteObjectError{Key: objectKey, Code: "AccessDenied", Message: "credential is not bound to this path"})
			continue
		}
		err := s.objects.DeleteForUser(req.Context(), owner, bucket, objectKey)
		if err != nil && !os.IsNotExist(err) {
			result.Errors = append(result.Errors, deleteObjectError{Key: objectKey, Code: "InternalError", Message: err.Error()})
			continue
		}
		if !payload.Quiet {
			result.Deleted = append(result.Deleted, deletedObject{Key: objectKey})
		}
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_ = xml.NewEncoder(w).Encode(result)
}

func (s *Server) handleUploadPart(w http.ResponseWriter, req *http.Request, credential *s3credential.Credential, owner *user.User, uploadID, rawPartNumber string) {
	if s.multipart == nil || !hasS3Permission(credential.Permissions, "create") {
		s.writeError(w, http.StatusForbidden, "AccessDenied", "create permission is required")
		return
	}
	partNumber, err := strconv.Atoi(rawPartNumber)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "InvalidPart", "invalid part number")
		return
	}
	part, err := s.multipart.UploadPart(req.Context(), owner, uploadID, partNumber, req.Header.Get("x-amz-checksum-sha256"), req.Body)
	if err != nil {
		s.writeObjectError(w, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("%q", part.ETag))
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleAbortMultipart(w http.ResponseWriter, req *http.Request, owner *user.User, uploadID string) {
	if s.multipart == nil {
		s.writeError(w, http.StatusNotImplemented, "NotImplemented", "multipart is not configured")
		return
	}
	if err := s.multipart.Abort(req.Context(), owner, uploadID); err != nil {
		s.writeObjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCompleteMultipart(w http.ResponseWriter, req *http.Request, credential *s3credential.Credential, owner *user.User, uploadID string) {
	if s.multipart == nil || !hasS3Permission(credential.Permissions, "create") {
		s.writeError(w, http.StatusForbidden, "AccessDenied", "create permission is required")
		return
	}
	var request struct {
		Parts []struct {
			PartNumber int    `xml:"PartNumber"`
			ETag       string `xml:"ETag"`
		} `xml:"Part"`
	}
	if err := xml.NewDecoder(req.Body).Decode(&request); err != nil {
		s.writeError(w, http.StatusBadRequest, "MalformedXML", "invalid complete multipart request")
		return
	}
	parts := make([]service.CompletePart, 0, len(request.Parts))
	for _, part := range request.Parts {
		parts = append(parts, service.CompletePart{PartNumber: part.PartNumber, ETag: strings.Trim(part.ETag, `"`)})
	}
	info, err := s.multipart.Complete(req.Context(), owner, uploadID, parts)
	if err != nil {
		s.writeObjectError(w, err)
		return
	}
	scheme := "http"
	if req.TLS != nil || strings.EqualFold(strings.TrimSpace(req.Header.Get("X-Forwarded-Proto")), "https") {
		scheme = "https"
	}
	response := struct {
		XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
		Location string   `xml:"Location,omitempty"`
		Bucket   string   `xml:"Bucket"`
		Key      string   `xml:"Key"`
		ETag     string   `xml:"ETag"`
	}{
		Location: scheme + "://" + req.Host + "/" + info.Bucket + "/" + info.Key,
		Bucket:   info.Bucket,
		Key:      info.Key,
		ETag:     fmt.Sprintf("%q", info.ETag),
	}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(response)
}

func hasS3Permission(value, permission string) bool {
	for _, item := range strings.Split(value, ",") {
		if strings.TrimSpace(item) == permission {
			return true
		}
	}
	return false
}

func (s *Server) pathAllowed(rootPath, requestedPath string) bool {
	rootPath = path.Clean("/" + strings.TrimSpace(rootPath))
	requestedPath = path.Clean("/" + strings.TrimSpace(requestedPath))
	return rootPath == "/" || requestedPath == rootPath || strings.HasPrefix(requestedPath, rootPath+"/")
}

type listBucketResult struct {
	XMLName               xml.Name       `xml:"ListBucketResult"`
	Name                  string         `xml:"Name"`
	Prefix                string         `xml:"Prefix,omitempty"`
	KeyCount              int            `xml:"KeyCount,omitempty"`
	MaxKeys               int            `xml:"MaxKeys,omitempty"`
	IsTruncated           bool           `xml:"IsTruncated"`
	NextMarker            string         `xml:"NextMarker,omitempty"`
	NextContinuationToken string         `xml:"NextContinuationToken,omitempty"`
	CommonPrefixes        []commonPrefix `xml:"CommonPrefixes,omitempty"`
	Contents              []listObject   `xml:"Contents"`
}

type listObject struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
}

type commonPrefix struct {
	Prefix string `xml:"Prefix"`
}

func (s *Server) handleList(w http.ResponseWriter, req *http.Request, credential *s3credential.Credential, userDirectory, bucket string) {
	query := req.URL.Query()
	prefix := query.Get("prefix")
	result, err := s.objects.List(req.Context(), userDirectory, bucket, prefix, 0)
	if err != nil {
		s.writeObjectError(w, err)
		return
	}
	maxKeys := 1000
	if raw := query.Get("max-keys"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed >= 0 && parsed <= 1000 {
			maxKeys = parsed
		}
	}
	marker := query.Get("marker")
	if query.Get("list-type") == "2" && query.Get("continuation-token") != "" {
		var token continuationToken
		if err := decodeContinuationToken(query.Get("continuation-token"), credential.Secret, &token); err != nil || token.Bucket != bucket || token.Prefix != prefix {
			s.writeError(w, http.StatusBadRequest, "InvalidToken", "invalid continuation token")
			return
		}
		marker = token.Key
	}
	items := result.Objects
	if marker != "" {
		start := 0
		for start < len(items) && items[start].Key <= marker {
			start++
		}
		items = items[start:]
	}
	truncated := len(items) > maxKeys
	if truncated {
		items = items[:maxKeys]
	}
	response := listBucketResult{Name: bucket, Prefix: prefix, KeyCount: len(items), MaxKeys: maxKeys, IsTruncated: truncated, Contents: make([]listObject, 0, len(items))}
	if truncated && len(items) > 0 {
		if query.Get("list-type") == "2" {
			next, err := encodeContinuationToken(continuationToken{Bucket: bucket, Prefix: prefix, Key: items[len(items)-1].Key}, credential.Secret)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, "InternalError", "failed to create continuation token")
				return
			}
			response.NextContinuationToken = next
		} else {
			response.NextMarker = items[len(items)-1].Key
		}
	}
	for _, item := range items {
		response.Contents = append(response.Contents, listObject{Key: item.Key, LastModified: item.ModifiedAt.UTC().Format(time.RFC3339), ETag: fmt.Sprintf("%q", item.ETag), Size: item.Size})
	}
	for _, item := range result.Prefixes {
		response.CommonPrefixes = append(response.CommonPrefixes, commonPrefix{Prefix: item})
	}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(response)
}

type continuationToken struct {
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
	Key    string `json:"key"`
}

func encodeContinuationToken(token continuationToken, secret string) (string, error) {
	payload, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	data := append(payload, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeContinuationToken(raw, secret string, token *continuationToken) error {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) < sha256.Size {
		return fmt.Errorf("invalid token")
	}
	payload, signature := data[:len(data)-sha256.Size], data[len(data)-sha256.Size:]
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return fmt.Errorf("invalid token signature")
	}
	return json.Unmarshal(payload, token)
}

func setObjectHeaders(w http.ResponseWriter, info service.ObjectInfo) {
	w.Header().Set("Content-Length", fmt.Sprint(info.Size))
	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Last-Modified", info.ModifiedAt.UTC().Format(http.TimeFormat))
	w.Header().Set("ETag", fmt.Sprintf("%q", info.ETag))
}

func (s *Server) writeObjectError(w http.ResponseWriter, err error) {
	if errors.Is(err, s3multipart.ErrChecksumMismatch) {
		s.writeError(w, http.StatusBadRequest, "BadDigest", "the provided checksum does not match the object")
		return
	}
	if os.IsNotExist(err) {
		s.writeError(w, http.StatusNotFound, "NoSuchKey", "object not found")
		return
	}
	s.writeError(w, http.StatusBadRequest, "InvalidRequest", err.Error())
}

func splitObjectPath(raw string) (string, string, bool) {
	clean := path.Clean("/" + raw)
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "." || parts[0] == ".." {
		return "", "", false
	}
	return parts[0], strings.Join(parts[1:], "/"), true
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code><Message>%s</Message></Error>", code, message)
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.httpServer == nil {
		return nil
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown s3 server: %w", err)
	}
	s.logger.Info("s3 http server stopped")
	return nil
}
