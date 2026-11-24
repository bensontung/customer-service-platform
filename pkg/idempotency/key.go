package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
)

var valid = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func Key(agentID, conversationID, raw string) (string, error) {
	if !valid.MatchString(raw) {
		return "", fmt.Errorf("idempotency key must contain 8-128 URL-safe characters")
	}
	hash := sha256.Sum256([]byte(agentID + "\x00" + conversationID + "\x00" + raw))
	return "client:" + hex.EncodeToString(hash[:]), nil
}
