package bootstrap

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/pkg/dingtalk"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/wechat"
)

type DingTalkEvent struct {
	SVC                            *svc.ServiceContext
	Secrets                        domain.Secrets
	TokenRef, AESKeyRef, CorpIDRef string
}

func (h DingTalkEvent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, err := h.Secrets.Value(r.Context(), h.TokenRef)
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	key, err := h.Secrets.Value(r.Context(), h.AESKeyRef)
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	corpID, err := h.Secrets.Value(r.Context(), h.CorpIDRef)
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var request struct {
		Encrypt string `json:"encrypt"`
	}
	if json.Unmarshal(body, &request) != nil || !wechat.VerifyMessageSignature(token, r.URL.Query().Get("timestamp"), r.URL.Query().Get("nonce"), request.Encrypt, r.URL.Query().Get("signature")) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	plain, err := dingtalk.DecryptEvent(request.Encrypt, corpID, key)
	if err != nil {
		http.Error(w, "invalid encryption", http.StatusForbidden)
		return
	}
	var event map[string]any
	if json.Unmarshal([]byte(plain), &event) != nil {
		http.Error(w, "unknown event", http.StatusBadRequest)
		return
	}
	kind, _ := event["EventType"].(string)
	switch kind {
	case "check_url", "org_user_modify", "user_leave_org", "user_active_org":
	default:
		http.Error(w, "unknown event", http.StatusBadRequest)
		return
	}
	if kind == "user_leave_org" || kind == "org_user_modify" {
		_ = kind
	}
	encrypted, err := wechat.Encrypt("success", corpID, key)
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	timestamp := time.Now().UTC().Format("20060102150405")
	nonce := domain.NewID()
	_ = json.NewEncoder(w).Encode(map[string]string{"msg_signature": wechat.MessageSignature(token, timestamp, nonce, encrypted), "timeStamp": timestamp, "nonce": nonce, "encrypt": encrypted})
}
