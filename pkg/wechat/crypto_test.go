package wechat

import (
	"encoding/base64"
	"testing"
)

func TestEncryptedRoundTripAndWrongApp(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))[:43]
	encrypted, err := Encrypt("plain message", "app-1", key)
	if err != nil {
		t.Fatal(err)
	}
	message, err := Decrypt(encrypted, "app-1", key)
	if err != nil || message != "plain message" {
		t.Fatalf("unexpected decryption %q %v", message, err)
	}
	if _, err = Decrypt(encrypted, "app-2", key); err == nil {
		t.Fatal("expected wrong AppID to be rejected")
	}
}

func TestSignature(t *testing.T) {
	if !VerifyPlainSignature("token", "1700000000", "nonce", PlainSignature("token", "1700000000", "nonce")) {
		t.Fatal("valid signature rejected")
	}
	if VerifyPlainSignature("token", "1700000000", "nonce", "invalid") {
		t.Fatal("invalid signature accepted")
	}
	if VerifyMessageSignature("token", "1", "2", "ciphertext", "bad") {
		t.Fatal("invalid signature accepted")
	}
}
