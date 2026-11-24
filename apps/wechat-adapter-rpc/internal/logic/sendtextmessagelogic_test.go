package logic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/customer-service-platform/apps/wechat-adapter-rpc/internal/config"
	"github.com/example/customer-service-platform/apps/wechat-adapter-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/wechat-adapter-rpc/pb/github.com/example/customer-service-platform/apps/wechat-adapter-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/platform"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/example/customer-service-platform/pkg/wechat"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSendTextMessageUsesWeChatClient(t *testing.T) {
	t.Setenv("TEST_RPC_WECHAT_SECRET", "test-only-secret")
	repository := store.NewMemory()
	if err := repository.SaveAccount(context.Background(), domain.ChannelAccount{ID: "account-id", AccountKey: "support", AppID: "app-id", AppSecretRef: "env://TEST_RPC_WECHAT_SECRET", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	tokens := wechat.NewMemoryTokenStore()
	if err := tokens.Set(context.Background(), "account-id", "cached-token", time.Hour); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cgi-bin/message/custom/send" || r.URL.Query().Get("access_token") != "cached-token" {
			t.Errorf("unexpected WeChat request")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["touser"] != "open-id" || body["msgtype"] != "text" || body["text"].(map[string]any)["content"] != "hello" {
			t.Errorf("unexpected message: %#v", body)
		}
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer server.Close()
	client, err := wechat.NewClient(wechat.Config{BaseURL: server.URL, HTTPClient: server.Client(), Store: repository, Secrets: platform.EnvSecrets{}, Tokens: tokens, Limiter: rate.NewLimiter(rate.Inf, 1)})
	if err != nil {
		t.Fatal(err)
	}
	svcCtx := svc.NewServiceContextWithClient(config.Config{}, repository, client)
	response, err := NewSendTextMessageLogic(context.Background(), svcCtx).SendTextMessage(&pb.SendTextMessageRequest{AccountKey: "support", OpenId: "open-id", Text: "hello", IdempotencyKey: "request-id"})
	if err != nil || response.MessageId != "request-id" {
		t.Fatalf("send message: %+v %v", response, err)
	}
}

func TestSendTextMessageInvalidAndNotFound(t *testing.T) {
	_, svcCtx, _ := wechatService(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	_, err := NewSendTextMessageLogic(context.Background(), svcCtx).SendTextMessage(&pb.SendTextMessageRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	_, err = NewSendTextMessageLogic(context.Background(), svcCtx).SendTextMessage(&pb.SendTextMessageRequest{AccountKey: "missing", OpenId: "open-id", Text: "hello"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}

func wechatService(t *testing.T, handler http.Handler) (*httptest.Server, *svc.ServiceContext, *store.Memory) {
	t.Helper()
	t.Setenv("TEST_RPC_WECHAT_SECRET", "test-only-secret")
	repository := store.NewMemory()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := wechat.NewClient(wechat.Config{BaseURL: server.URL, HTTPClient: server.Client(), Store: repository, Secrets: platform.EnvSecrets{}, Limiter: rate.NewLimiter(rate.Inf, 1)})
	if err != nil {
		t.Fatal(err)
	}
	return server, svc.NewServiceContextWithClient(config.Config{}, repository, client), repository
}
