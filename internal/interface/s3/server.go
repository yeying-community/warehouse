package s3

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/s3credential"
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
}

func NewServer(cfg config.S3Config, resolver CredentialResolver, objects *service.ObjectService, users user.Repository, logger *zap.Logger) *Server {
	return &Server{config: cfg, resolver: resolver, objects: objects, users: users, logger: logger}
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
			s.handleListBuckets(w, req.Context(), credential)
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

func (s *Server) handleListBuckets(w http.ResponseWriter, _ context.Context, _ *s3credential.Credential) {
	now := time.Now().UTC().Format(time.RFC3339)
	response := listAllMyBucketsResult{Buckets: []bucketInfo{{Name: "personal", CreationDate: now}, {Name: "apps", CreationDate: now}}}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(response)
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
	if _, err := VerifyHeaderSignature(req, credential.Secret, SignatureV4Config{
		Region:               s.config.Region,
		Service:              "s3",
		AllowUnsignedPayload: req.TLS != nil,
	}); err != nil {
		return nil, err
	}
	return credential, nil
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
	switch req.Method {
	case http.MethodGet:
		if !hasS3Permission(credential.Permissions, "read") {
			s.writeError(w, http.StatusForbidden, "AccessDenied", "read permission is required")
			return
		}
		if key == "" {
			s.handleList(w, req.Context(), userDirectory, bucket, req.URL.Query().Get("prefix"))
			return
		}
		file, info, err := s.objects.Open(req.Context(), userDirectory, bucket, key)
		if err != nil {
			s.writeObjectError(w, err)
			return
		}
		defer file.Close()
		setObjectHeaders(w, info)
		_, _ = io.Copy(w, file)
	case http.MethodHead:
		if !hasS3Permission(credential.Permissions, "read") {
			s.writeError(w, http.StatusForbidden, "AccessDenied", "read permission is required")
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
		info, err := s.objects.Put(req.Context(), userDirectory, bucket, key, req.Body)
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
		if err := s.objects.Delete(req.Context(), userDirectory, bucket, key); err != nil {
			s.writeObjectError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.writeError(w, http.StatusNotImplemented, "NotImplemented", "operation is not implemented")
	}
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
	XMLName     xml.Name     `xml:"ListBucketResult"`
	Name        string       `xml:"Name"`
	Prefix      string       `xml:"Prefix,omitempty"`
	KeyCount    int          `xml:"KeyCount,omitempty"`
	MaxKeys     int          `xml:"MaxKeys,omitempty"`
	IsTruncated bool         `xml:"IsTruncated"`
	Contents    []listObject `xml:"Contents"`
}

type listObject struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
}

func (s *Server) handleList(w http.ResponseWriter, ctx context.Context, userDirectory, bucket, prefix string) {
	result, err := s.objects.List(ctx, userDirectory, bucket, prefix, 0)
	if err != nil {
		s.writeObjectError(w, err)
		return
	}
	response := listBucketResult{Name: bucket, Prefix: prefix, KeyCount: len(result.Objects), MaxKeys: 1000, Contents: make([]listObject, 0, len(result.Objects))}
	for _, item := range result.Objects {
		response.Contents = append(response.Contents, listObject{Key: item.Key, LastModified: item.ModifiedAt.UTC().Format(time.RFC3339), ETag: fmt.Sprintf("%q", item.ETag), Size: item.Size})
	}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(response)
}

func setObjectHeaders(w http.ResponseWriter, info service.ObjectInfo) {
	w.Header().Set("Content-Length", fmt.Sprint(info.Size))
	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Last-Modified", info.ModifiedAt.UTC().Format(http.TimeFormat))
	w.Header().Set("ETag", fmt.Sprintf("%q", info.ETag))
}

func (s *Server) writeObjectError(w http.ResponseWriter, err error) {
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
