package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/platform"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/example/customer-service-platform/pkg/wechat"
)

type Handler struct {
	Store   store.Store
	Secrets domain.Secrets
	Now     func() time.Time
}

func NewHandler(repository store.Store) *Handler {
	return &Handler{Store: repository, Secrets: platform.EnvSecrets{}, Now: time.Now}
}

func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	account, err := h.Store.AccountByKey(r.Context(), pathAccount(r.URL.Path))
	if err != nil {
		http.Error(w, "invalid callback", http.StatusForbidden)
		return
	}
	token, err := h.Secrets.Value(r.Context(), account.TokenSecretRef)
	if err != nil || !timestampValid(r.URL.Query().Get("timestamp"), h.Now()) || !wechat.VerifyPlainSignature(token, r.URL.Query().Get("timestamp"), r.URL.Query().Get("nonce"), r.URL.Query().Get("signature")) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(r.URL.Query().Get("echostr")))
}

func (h *Handler) Receive(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	account, err := h.Store.AccountByKey(r.Context(), pathAccount(r.URL.Path))
	if err != nil {
		http.Error(w, "invalid callback", http.StatusForbidden)
		return
	}
	token, err := h.Secrets.Value(r.Context(), account.TokenSecretRef)
	if err != nil {
		http.Error(w, "callback unavailable", http.StatusServiceUnavailable)
		return
	}
	body, err := wechat.ReadBody(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	envelope, err := wechat.Unmarshal(body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if !timestampValid(r.URL.Query().Get("timestamp"), h.Now()) {
		http.Error(w, "invalid timestamp", http.StatusForbidden)
		return
	}
	if envelope.Encrypt != "" {
		if !wechat.VerifyMessageSignature(token, r.URL.Query().Get("timestamp"), r.URL.Query().Get("nonce"), envelope.Encrypt, r.URL.Query().Get("msg_signature")) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
		key, secretErr := h.Secrets.Value(r.Context(), account.AESKeySecretRef)
		if secretErr != nil {
			http.Error(w, "callback unavailable", http.StatusServiceUnavailable)
			return
		}
		plain, decryptErr := wechat.Decrypt(envelope.Encrypt, account.AppID, key)
		if decryptErr != nil {
			http.Error(w, "invalid encryption", http.StatusForbidden)
			return
		}
		envelope, err = wechat.Unmarshal([]byte(plain))
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
	} else if !wechat.VerifyPlainSignature(token, r.URL.Query().Get("timestamp"), r.URL.Query().Get("nonce"), r.URL.Query().Get("signature")) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	if envelope.MsgType == "event" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
		return
	}
	inbound, err := envelope.Inbound(account.AccountKey, account.Channel, h.Now())
	if err != nil {
		http.Error(w, "unsupported message", http.StatusBadRequest)
		return
	}
	inbound.AppID = account.AppID
	hash := sha256.Sum256(body)
	inbound.PayloadHash = hex.EncodeToString(hash[:])
	ctx, cancel := context.WithTimeout(r.Context(), 400*time.Millisecond)
	defer cancel()
	_, err = h.Store.ReceiveInbound(ctx, inbound, account)
	if err != nil {
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Server-Timing", fmt.Sprintf("app;dur=%d", time.Since(started).Milliseconds()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("success"))
}

func pathAccount(path string) string {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func timestampValid(raw string, now time.Time) bool {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return false
	}
	delta := now.UTC().Sub(time.Unix(value, 0).UTC())
	return delta >= -5*time.Minute && delta <= 5*time.Minute
}
