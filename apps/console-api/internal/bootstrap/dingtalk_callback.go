package bootstrap

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/pkg/dingtalk"
	"github.com/example/customer-service-platform/pkg/domain"
)

type DingTalkCallback struct {
	SVC       *svc.ServiceContext
	Secrets   domain.Secrets
	SecretRef string
	Now       func() time.Time
}
type cardRequest struct {
	Type       string `json:"type"`
	OutTrackID string `json:"outTrackId"`
	UserID     string `json:"userId"`
	Content    string `json:"content"`
}
type cardContent struct {
	CardPrivateData struct {
		ActionIDs []string       `json:"actionIds"`
		Params    map[string]any `json:"params"`
	} `json:"cardPrivateData"`
}

func (h DingTalkCallback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	secret, err := h.Secrets.Value(r.Context(), h.SecretRef)
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	if err = dingtalk.VerifyCardHeaders(r.Header, secret, now); err != nil {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	var request cardRequest
	if err = json.Unmarshal(body, &request); err != nil || request.Type != "actionCallback" || request.OutTrackID == "" || request.UserID == "" {
		http.Error(w, "unknown event", http.StatusBadRequest)
		return
	}
	var content cardContent
	if err = json.Unmarshal([]byte(request.Content), &content); err != nil || len(content.CardPrivateData.ActionIDs) == 0 {
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	agent, err := h.SVC.Store.AgentByDingTalkID(r.Context(), request.UserID)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	conversationID, _ := content.CardPrivateData.Params["conversationId"].(string)
	action := content.CardPrivateData.ActionIDs[0]
	switch action {
	case "claim":
		if _, err = h.SVC.Store.ClaimConversation(r.Context(), conversationID, agent); err != nil {
			http.Error(w, "claim failed", http.StatusConflict)
			return
		}
	case "view":
	case "transfer":
		if agent.Role != domain.RoleAdmin && agent.Role != domain.RoleSupervisor {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		targetID, _ := content.CardPrivateData.Params["assigneeId"].(string)
		teamID, _ := content.CardPrivateData.Params["teamId"].(string)
		if _, err = h.SVC.Store.TransferConversation(r.Context(), conversationID, targetID, teamID, agent); err != nil {
			http.Error(w, "transfer failed", http.StatusConflict)
			return
		}
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"cardData": map[string]any{"cardParamMap": map[string]string{"status": action + " completed"}}, "cardUpdateOptions": map[string]any{"updateCardDataByKey": true}})
}
