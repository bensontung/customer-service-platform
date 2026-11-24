package platform

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestMinIOPresignedUploadAndRead(t *testing.T) {
	endpoint := os.Getenv("TEST_OBJECT_STORAGE_ENDPOINT")
	if endpoint == "" {
		if os.Getenv("REQUIRE_INTEGRATION") == "1" {
			t.Fatal("TEST_OBJECT_STORAGE_ENDPOINT is required by REQUIRE_INTEGRATION=1")
		}
		t.Skip("set TEST_OBJECT_STORAGE_ENDPOINT to run isolated MinIO integration tests")
	}
	secrets := mapSecrets{"access": os.Getenv("TEST_OBJECT_STORAGE_ACCESS_KEY"), "secret": os.Getenv("TEST_OBJECT_STORAGE_SECRET_KEY")}
	bucket := "integration-" + time.Now().UTC().Format("150405000000000")
	storage, err := NewStorage(endpoint, "access", "secret", bucket, secrets)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err = storage.Client().MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = storage.Client().RemoveBucket(context.Background(), bucket) }()
	upload, err := storage.PresignPut(ctx, "image.png", "image/png", 4, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, upload.UploadURL, strings.NewReader("test"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d", response.StatusCode)
	}
	object, err := storage.ReadObject(ctx, upload.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = object.Close() }()
	data, err := io.ReadAll(object)
	if err != nil || string(data) != "test" {
		t.Fatalf("ReadObject() = %q, %v", data, err)
	}
	_ = object.Close()
	if err = storage.Client().RemoveObject(ctx, bucket, upload.ObjectKey, minio.RemoveObjectOptions{}); err != nil {
		t.Fatal(err)
	}
}

type mapSecrets map[string]string

func (m mapSecrets) Value(_ context.Context, reference string) (string, error) {
	return m[reference], nil
}
