package platform

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Storage struct {
	client       *minio.Client
	uploadClient *minio.Client
	bucket       string
}

func NewStorage(endpoint, accessKeyRef, secretKeyRef, bucket string, secrets domain.Secrets) (*Storage, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("valid OBJECT_STORAGE_ENDPOINT is required")
	}
	accessKey, err := secrets.Value(context.Background(), accessKeyRef)
	if err != nil {
		return nil, err
	}
	secretKey, err := secrets.Value(context.Background(), secretKeyRef)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(parsed.Host, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: parsed.Scheme == "https", Region: "us-east-1"})
	if err != nil {
		return nil, err
	}
	if bucket == "" {
		return nil, fmt.Errorf("OBJECT_STORAGE_BUCKET is required")
	}
	storage := &Storage{client: client, uploadClient: client, bucket: bucket}
	if publicEndpoint := os.Getenv("OBJECT_STORAGE_PUBLIC_ENDPOINT"); publicEndpoint != "" {
		publicClient, publicErr := storageEndpoint(publicEndpoint, accessKey, secretKey)
		if publicErr != nil {
			return nil, publicErr
		}
		storage.uploadClient = publicClient
	}
	return storage, nil
}
func (s *Storage) PresignPut(ctx context.Context, filename, contentType string, size int64, lifetime time.Duration) (domain.ObjectUpload, error) {
	if size <= 0 || size > 20<<20 {
		return domain.ObjectUpload{}, domain.ErrInvalidRequest
	}
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "audio/amr" && contentType != "audio/mpeg" {
		return domain.ObjectUpload{}, domain.ErrInvalidRequest
	}
	extension := strings.ToLower(path.Ext(path.Base(filename)))
	contentTypes := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".amr": "audio/amr", ".mp3": "audio/mpeg"}
	if contentTypes[extension] != contentType {
		return domain.ObjectUpload{}, domain.ErrInvalidRequest
	}
	key := "agent-media/" + time.Now().UTC().Format("2006/01/02") + "/" + domain.NewID() + extension
	upload, err := s.uploadClient.PresignedPutObject(ctx, s.bucket, key, lifetime)
	if err != nil {
		return domain.ObjectUpload{}, fmt.Errorf("presign object upload: %w", err)
	}
	return domain.ObjectUpload{ObjectKey: key, UploadURL: upload.String(), Headers: map[string]string{}, ExpiresAt: time.Now().UTC().Add(lifetime)}, nil
}
func storageEndpoint(endpoint, accessKey, secretKey string) (*minio.Client, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" {
		return nil, fmt.Errorf("valid OBJECT_STORAGE_PUBLIC_ENDPOINT is required: an HTTPS origin without a path")
	}
	client, err := minio.New(parsed.Host, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: true, Region: "us-east-1"})
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (s *Storage) ObjectURL(objectKey string) string {
	return s.client.EndpointURL().String() + "/" + s.bucket + "/" + objectKey
}
func (s *Storage) ReadObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucket, objectKey, minio.GetObjectOptions{})
}
func (s *Storage) Client() *minio.Client { return s.client }
func (s *Storage) Bucket() string        { return s.bucket }

var _ domain.ObjectStorage = (*Storage)(nil)
