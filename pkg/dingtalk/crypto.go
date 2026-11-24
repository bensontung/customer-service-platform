package dingtalk

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
)

const (
	SignatureTimestampHeader = "x-ddpaas-signature-timestamp"
	SignatureHeader          = "x-ddpaas-signature"
	SignatureMaxAge          = 5 * time.Minute
)

var ErrInvalidSignature = errors.New("invalid DingTalk callback signature")

func CardSignature(apiSecret, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(apiSecret))
	_, _ = mac.Write([]byte(timestamp))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func VerifyCardSignature(apiSecret, timestamp, actual string, now time.Time) bool {
	if apiSecret == "" || timestamp == "" || actual == "" {
		return false
	}
	milliseconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || milliseconds <= 0 {
		return false
	}
	signedAt := time.UnixMilli(milliseconds)
	if now.Sub(signedAt) > SignatureMaxAge || signedAt.Sub(now) > SignatureMaxAge {
		return false
	}
	return hmac.Equal([]byte(CardSignature(apiSecret, timestamp)), []byte(actual))
}

func VerifyCardHeaders(headers http.Header, apiSecret string, now time.Time) error {
	if headers == nil || !VerifyCardSignature(apiSecret, headers.Get(SignatureTimestampHeader), headers.Get(SignatureHeader), now) {
		return errors.Join(domain.ErrUnauthorized, ErrInvalidSignature)
	}
	return nil
}

func DecryptEvent(encrypted, corpID, encodingAESKey string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil || len(key) != 32 {
		return "", errors.New("invalid DingTalk AES key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", errors.New("invalid DingTalk AES key")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil || len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", errors.New("invalid encrypted DingTalk event")
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, ciphertext)
	plain, err = eventUnpad(plain)
	if err != nil {
		return "", err
	}
	if len(plain) < 20+len(corpID) {
		return "", errors.New("invalid DingTalk plaintext")
	}
	length := uint64(binary.BigEndian.Uint32(plain[16:20]))
	if length > uint64(len(plain)-20-len(corpID)) || !hmac.Equal(plain[20+int(length):], []byte(corpID)) {
		return "", errors.New("DingTalk event CorpID mismatch")
	}
	return string(plain[20 : 20+int(length)]), nil
}

func eventUnpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("invalid DingTalk padding")
	}
	padding := int(data[len(data)-1])
	if padding < 1 || padding > 32 || padding > len(data) {
		return nil, fmt.Errorf("invalid DingTalk PKCS7 padding")
	}
	for _, value := range data[len(data)-padding:] {
		if int(value) != padding {
			return nil, fmt.Errorf("invalid DingTalk PKCS7 padding")
		}
	}
	return data[:len(data)-padding], nil
}
