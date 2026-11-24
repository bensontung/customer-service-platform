package dingtalk_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/example/customer-service-platform/pkg/dingtalk"
	"github.com/example/customer-service-platform/pkg/domain"
)

type callbackSecrets map[string]string

func (s callbackSecrets) Value(_ context.Context, reference string) (string, error) {
	return s[reference], nil
}

func TestCardSignature(t *testing.T) {
	secret := "test-only-api-secret" // #nosec G101 -- non-production fixture.
	now := time.UnixMilli(1700000000000)
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if dingtalk.CardSignature(secret, timestamp) != signature {
		t.Fatal("signature algorithm differs from HMAC-SHA256")
	}
	headers := make(http.Header)
	headers.Set(dingtalk.SignatureTimestampHeader, timestamp)
	headers.Set(dingtalk.SignatureHeader, signature)
	if err := dingtalk.VerifyCardHeaders(headers, secret, now); err != nil {
		t.Fatal(err)
	}
	if !dingtalk.VerifyCardSignature(secret, timestamp, signature, now.Add(5*time.Minute)) {
		t.Fatal("signature at five-minute boundary should be accepted")
	}
	for _, test := range []struct {
		name      string
		headers   http.Header
		secret    string
		timestamp time.Time
	}{
		{"wrong signature", headers.Clone(), secret, now},
		{"expired", headers.Clone(), secret, now.Add(5*time.Minute + time.Millisecond)},
		{"too far in future", headers.Clone(), secret, now.Add(-5*time.Minute - time.Millisecond)},
		{"wrong secret", headers.Clone(), "different-secret", now},
		{"empty secret", headers.Clone(), "", now},
		{"missing headers", nil, secret, now},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "wrong signature" {
				test.headers.Set(dingtalk.SignatureHeader, "invalid-signature")
			}
			if err := dingtalk.VerifyCardHeaders(test.headers, test.secret, test.timestamp); !errors.Is(err, dingtalk.ErrInvalidSignature) || !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("expected rejected signature, got %v", err)
			}
		})
	}
	badTimestamp := headers.Clone()
	badTimestamp.Set(dingtalk.SignatureTimestampHeader, "not-milliseconds")
	if dingtalk.VerifyCardSignature(secret, "not-milliseconds", signature, now) {
		t.Fatal("invalid timestamp accepted")
	}
	if err := dingtalk.VerifyCardHeaders(badTimestamp, secret, now); err == nil {
		t.Fatal("invalid header timestamp accepted")
	}
}

func TestDecryptEventWithCorpID(t *testing.T) {
	keyBytes := []byte("01234567890123456789012345678901")
	key := base64.StdEncoding.EncodeToString(keyBytes)[:43]
	message := `{"EventType":"conversation_created"}`
	ciphertext := encryptEventForTest(t, message, "corp-id", keyBytes)
	plain, err := dingtalk.DecryptEvent(ciphertext, "corp-id", key)
	if err != nil || plain != message {
		t.Fatalf("unexpected decrypted event %q: %v", plain, err)
	}
	if _, err = dingtalk.DecryptEvent(ciphertext, "other-corp", key); err == nil {
		t.Fatal("wrong CorpID accepted")
	}
	modified := []byte(ciphertext)
	modified[len(modified)/2] = '!'
	if _, err = dingtalk.DecryptEvent(string(modified), "corp-id", key); err == nil {
		t.Fatal("invalid ciphertext accepted")
	}
	if _, err = dingtalk.DecryptEvent(ciphertext, "corp-id", "not-a-valid-key"); err == nil {
		t.Fatal("invalid AES key accepted")
	}
}

func TestClientCallbackSecrets(t *testing.T) {
	keyBytes := []byte("01234567890123456789012345678901")
	key := base64.StdEncoding.EncodeToString(keyBytes)[:43]
	secretValues := callbackSecrets{
		"env://TEST_DINGTALK_APP_KEY":    "test-app-key",
		"env://TEST_DINGTALK_APP_SECRET": "test-app-secret",
		"env://TEST_DINGTALK_API_SECRET": "test-only-api-secret",
		"env://TEST_DINGTALK_AES_KEY":    key,
		"env://TEST_DINGTALK_CORP_ID":    "corp-id",
	}
	client, err := dingtalk.NewClient(dingtalk.Config{
		Secrets: secretValues, AppKeySecretRef: "env://TEST_DINGTALK_APP_KEY", AppSecretRef: "env://TEST_DINGTALK_APP_SECRET",
		CardSecretRef: "env://TEST_DINGTALK_API_SECRET", AESKeySecretRef: "env://TEST_DINGTALK_AES_KEY", CorpIDSecretRef: "env://TEST_DINGTALK_CORP_ID",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	headers := make(http.Header)
	headers.Set(dingtalk.SignatureTimestampHeader, timestamp)
	headers.Set(dingtalk.SignatureHeader, dingtalk.CardSignature(secretValues["env://TEST_DINGTALK_API_SECRET"], timestamp))
	if err = client.VerifyCardSignature(context.Background(), headers); err != nil {
		t.Fatal(err)
	}
	encrypted := encryptEventForTest(t, "event", "corp-id", keyBytes)
	message, err := client.DecryptEvent(context.Background(), encrypted)
	if err != nil || message != "event" {
		t.Fatalf("unexpected decryption %q: %v", message, err)
	}
}

func encryptEventForTest(t *testing.T, message, corpID string, key []byte) string {
	t.Helper()
	random := make([]byte, aes.BlockSize)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	plain := make([]byte, 20+len(message)+len(corpID))
	copy(plain, random)
	binary.BigEndian.PutUint32(plain[16:20], uint32(len(message)))
	copy(plain[20:], message)
	copy(plain[20+len(message):], corpID)
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(plain, make([]byte, padding)...)
	for i := len(plain) - padding; i < len(plain); i++ {
		plain[i] = byte(padding)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(ciphertext, plain)
	return base64.StdEncoding.EncodeToString(ciphertext)
}
