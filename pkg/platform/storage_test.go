package platform

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestStoragePublicPresignedUpload(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_PUBLIC_ENDPOINT", "https://storage.example.com")
	storage, err := NewStorage("http://minio:9000", "access", "secret", "customer-service", mapSecrets{"access": "test-access", "secret": "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	upload, err := storage.PresignPut(context.Background(), "image.png", "image/png", 4, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(upload.UploadURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "storage.example.com" || parsed.Path != "/customer-service/"+upload.ObjectKey {
		t.Fatalf("public upload URL = %s", upload.UploadURL)
	}
	if parsed.Query().Get("X-Amz-Signature") == "" || parsed.Query().Get("X-Amz-Credential") == "" || parsed.Query().Get("X-Amz-SignedHeaders") != "host" {
		t.Fatalf("upload signature missing: %s", upload.UploadURL)
	}
	if !strings.HasPrefix(upload.ObjectKey, "agent-media/") || !strings.HasSuffix(upload.ObjectKey, ".png") || upload.ExpiresAt.IsZero() {
		t.Fatalf("upload = %#v", upload)
	}
	if !strings.HasPrefix(storage.ObjectURL(upload.ObjectKey), "http://minio:9000/customer-service/agent-media/") {
		t.Fatal("internal object URL was changed to the public endpoint")
	}
}

func TestStorageRejectsInvalidConfigurationAndMedia(t *testing.T) {
	for _, endpoint := range []string{"http://storage.example.com", "https://storage.example.com/storage", "https://storage.example.com?token=secret", "https://user:secret@storage.example.com"} {
		t.Setenv("OBJECT_STORAGE_PUBLIC_ENDPOINT", endpoint)
		if _, err := NewStorage("http://minio:9000", "access", "secret", "customer-service", mapSecrets{"access": "test", "secret": "test"}); err == nil {
			t.Fatalf("accepted public endpoint %q", endpoint)
		}
	}
	t.Setenv("OBJECT_STORAGE_PUBLIC_ENDPOINT", "")
	for _, endpoint := range []string{"", "minio:9000", "ftp://minio:9000", "https://user:secret@minio:9000"} {
		if _, err := NewStorage(endpoint, "access", "secret", "customer-service", mapSecrets{"access": "test", "secret": "test"}); err == nil {
			t.Fatalf("accepted internal endpoint %q", endpoint)
		}
	}
	storage, err := NewStorage("http://minio:9000", "access", "secret", "customer-service", mapSecrets{"access": "test", "secret": "test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		name, contentType string
		size              int64
	}{
		{"image.gif", "image/gif", 4},
		{"image.png", "image/png", 20<<20 + 1},
		{"image.png", "image/png", 0},
		{"image.png", "image/jpeg", 4},
	} {
		if _, err := storage.PresignPut(context.Background(), input.name, input.contentType, input.size, time.Minute); err == nil {
			t.Fatalf("accepted media %+v", input)
		}
	}
}
