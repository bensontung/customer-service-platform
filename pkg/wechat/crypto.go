package wechat

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- SHA-1 is mandated by the WeChat callback signature protocol, not password storage.
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

func PlainSignature(token, timestamp, nonce string) string {
	return signature([]string{token, timestamp, nonce})
}

func MessageSignature(token, timestamp, nonce, encrypted string) string {
	return signature([]string{token, timestamp, nonce, encrypted})
}

func signature(parts []string) string {
	sort.Strings(parts)
	digest := sha1.Sum([]byte(strings.Join(parts, ""))) // #nosec G401 -- required by the external WeChat signature specification.
	return hex.EncodeToString(digest[:])
}

func VerifyPlainSignature(token, timestamp, nonce, actual string) bool {
	return token != "" && secureEqual(PlainSignature(token, timestamp, nonce), actual)
}

func VerifyMessageSignature(token, timestamp, nonce, encrypted, actual string) bool {
	return token != "" && encrypted != "" && secureEqual(MessageSignature(token, timestamp, nonce, encrypted), actual)
}

func secureEqual(expected, actual string) bool {
	return len(expected) == len(actual) && subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func Decrypt(encrypted, appID, encodingAESKey string) (string, error) {
	block, err := aesCipher(encodingAESKey)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil || len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid encrypted wechat message")
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, blockKey(encodingAESKey)[:aes.BlockSize]).CryptBlocks(plain, ciphertext)
	plain, err = unpad(plain)
	if err != nil {
		return "", err
	}
	if len(plain) < 20+len(appID) {
		return "", fmt.Errorf("invalid wechat plaintext")
	}
	length := int(binary.BigEndian.Uint32(plain[16:20]))
	if length < 0 || length > len(plain)-20-len(appID) || string(plain[20+length:]) != appID {
		return "", fmt.Errorf("wechat message AppID mismatch")
	}
	return string(plain[20 : 20+length]), nil
}

func Encrypt(message, appID, encodingAESKey string) (string, error) {
	block, err := aesCipher(encodingAESKey)
	if err != nil {
		return "", err
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return "", err
	}
	plain := make([]byte, 20+len(message)+len(appID))
	copy(plain, random)
	binary.BigEndian.PutUint32(plain[16:20], uint32(len(message)))
	copy(plain[20:], message)
	copy(plain[20+len(message):], appID)
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(plain, strings.Repeat(string(rune(padding)), padding)...)
	ciphertext := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, blockKey(encodingAESKey)[:aes.BlockSize]).CryptBlocks(ciphertext, plain)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func aesCipher(encodingAESKey string) (cipher.Block, error) {
	key, err := decodeKey(encodingAESKey)
	if err != nil {
		return nil, err
	}
	return aes.NewCipher(key)
}

func blockKey(encodingAESKey string) []byte {
	key, _ := decodeKey(encodingAESKey)
	return key
}

func decodeKey(key string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(key + "=")
	if err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("invalid AES key")
	}
	return decoded, nil
}

func unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("invalid padded data")
	}
	padding := int(data[len(data)-1])
	if padding < 1 || padding > 32 || padding > len(data) {
		return nil, fmt.Errorf("invalid PKCS7 padding")
	}
	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return nil, fmt.Errorf("invalid PKCS7 padding")
		}
	}
	return data[:len(data)-padding], nil
}
