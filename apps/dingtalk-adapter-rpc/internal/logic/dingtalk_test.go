package logic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/internal/config"
	"github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/pb/github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/pb"
	"github.com/example/customer-service-platform/pkg/dingtalk"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/platform"
	"github.com/example/customer-service-platform/pkg/store"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func dingtalkService(t *testing.T, handler http.Handler) (*svc.ServiceContext, *store.Memory) {
	t.Helper()
	t.Setenv("TEST_RPC_DINGTALK_KEY", "test-app-key")
	t.Setenv("TEST_RPC_DINGTALK_SECRET", "test-only-secret")
	repository := store.NewMemory()
	if err := repository.SaveAgent(context.Background(), domain.Agent{ID: "agent-id", DingTalkUserID: "ding-user", Active: true}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := dingtalk.NewClient(dingtalk.Config{
		BaseURL: server.URL, LegacyBaseURL: server.URL, HTTPClient: server.Client(), Secrets: platform.EnvSecrets{}, Store: repository,
		AppKeySecretRef: "env://TEST_RPC_DINGTALK_KEY", AppSecretRef: "env://TEST_RPC_DINGTALK_SECRET",
		CardTemplateID: "template-id", CallbackRouteKey: "notification-route", Limiter: rate.NewLimiter(rate.Inf, 1), MaxAttempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc.NewServiceContextWithClient(config.Config{}, repository, client), repository
}

func TestExchangeLoginCode(t *testing.T) {
	svcCtx, _ := dingtalkService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			_, _ = io.WriteString(w, `{"accessToken":"ding-token","expireIn":7200}`)
		case "/topapi/v2/user/getuserinfo":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["code"] != "temporary-code" {
				t.Errorf("unexpected code")
			}
			_, _ = io.WriteString(w, `{"errcode":0,"result":{"userid":"ding-user"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	response, err := NewExchangeLoginCodeLogic(context.Background(), svcCtx).ExchangeLoginCode(&pb.ExchangeLoginCodeRequest{Code: "temporary-code", CorpId: "corp-id"})
	if err != nil || response.DingTalkUserId != "ding-user" {
		t.Fatalf("exchange login code: %+v %v", response, err)
	}
}

func TestSendNotification(t *testing.T) {
	svcCtx, _ := dingtalkService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			_, _ = io.WriteString(w, `{"accessToken":"ding-token","expireIn":7200}`)
			return
		}
		if r.URL.Path != "/v1.0/card/instances/createAndDeliver" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		parameters := body["cardData"].(map[string]any)["cardParamMap"].(map[string]any)
		if parameters["conversationId"] != "conversation-id" || parameters["title"] != "New conversation" || parameters["text"] != "Customer message" {
			t.Errorf("unexpected notification: %#v", parameters)
		}
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	response, err := NewSendNotificationLogic(context.Background(), svcCtx).SendNotification(&pb.SendNotificationRequest{AgentId: "agent-id", ConversationId: "conversation-id", Kind: "new", Title: "New conversation", Text: "Customer message", Url: "https://console.example.test/conversation"})
	if err != nil || response.NotificationId == "" {
		t.Fatalf("send notification: %+v %v", response, err)
	}
	duplicate, err := NewSendNotificationLogic(context.Background(), svcCtx).SendNotification(&pb.SendNotificationRequest{AgentId: "agent-id", ConversationId: "conversation-id", Kind: "new", Title: "New conversation", Text: "Customer message", Url: "https://console.example.test/conversation"})
	if err != nil || duplicate.NotificationId != response.NotificationId {
		t.Fatalf("notification is not idempotent: %+v %v", duplicate, err)
	}
}

func TestDingTalkInvalidInput(t *testing.T) {
	svcCtx, _ := dingtalkService(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	_, err := NewExchangeLoginCodeLogic(context.Background(), svcCtx).ExchangeLoginCode(&pb.ExchangeLoginCodeRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}
