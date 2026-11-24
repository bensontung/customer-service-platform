package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/customer-service-platform/apps/console-api/internal/config"
	consolehandler "github.com/example/customer-service-platform/apps/console-api/internal/handler"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/pkg/auth"
	worker "github.com/example/customer-service-platform/pkg/conversation"
	"github.com/example/customer-service-platform/pkg/dingtalk"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/example/customer-service-platform/pkg/wechat"
	"github.com/example/customer-service-platform/pkg/wechat/callback"
	"github.com/zeromicro/go-zero/rest"
)

type integrationSecrets map[string]string

func (s integrationSecrets) Value(_ context.Context, ref string) (string, error) { return s[ref], nil }

type integrationEnvironment struct {
	store         *store.Memory
	worker        *worker.Worker
	console       *httptest.Server
	notifications atomic.Int32
	sentMu        sync.Mutex
	sent          []map[string]any
	token         string
	agent         domain.Agent
}

func newIntegrationEnvironment(t *testing.T) *integrationEnvironment {
	t.Helper()
	ConfigureErrorHandler()
	repository := store.NewMemory()
	agent := domain.Agent{ID: "agent-1", DingTalkUserID: "ding-1", Name: "测试客服", Role: domain.RoleAgent, Active: true}
	_ = repository.SaveAgent(context.Background(), agent)
	_ = repository.SaveTeamAgent(context.Background(), "team-1", agent.ID)
	_ = repository.SaveShift(context.Background(), domain.Shift{ID: "shift-1", AgentID: agent.ID, StartAt: time.Now().Add(-time.Hour), EndAt: time.Now().Add(time.Hour), State: "active"})
	_ = repository.SaveRoutingRule(context.Background(), domain.RoutingRule{ID: "rule-1", Priority: 1, TeamID: "team-1", Active: true})
	account := domain.ChannelAccount{ID: "account-1", AccountKey: "service", Channel: domain.ChannelService, AppID: "wx-test-app", TokenSecretRef: "token", AESKeySecretRef: "aes", AppSecretRef: "secret", Status: "active"}
	_ = repository.SaveAccount(context.Background(), account)
	environment := &integrationEnvironment{store: repository, agent: agent}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			_, _ = io.WriteString(w, `{"accessToken":"ding-token","expireIn":7200}`)
		case "/topapi/v2/user/getuserinfo":
			_, _ = io.WriteString(w, `{"errcode":0,"result":{"userid":"ding-1","name":"测试客服"}}`)
		case "/v1.0/card/instances/createAndDeliver":
			environment.notifications.Add(1)
			_, _ = io.WriteString(w, `{"errcode":0,"result":{}}`)
		case "/cgi-bin/stable_token", "/cgi-bin/token":
			_, _ = io.WriteString(w, `{"access_token":"wechat-token","expires_in":7200}`)
		case "/cgi-bin/message/custom/send":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			environment.sentMu.Lock()
			environment.sent = append(environment.sent, body)
			environment.sentMu.Unlock()
			_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
		default:
			t.Errorf("unexpected external endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	secrets := integrationSecrets{"token": "wechat-callback-token", "secret": "wechat-secret", "app-key": "ding-key", "app-secret": "ding-secret", "card-secret": "card-secret"}
	wechatClient, err := wechat.NewClient(wechat.Config{BaseURL: server.URL, Store: repository, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	dingtalkClient, err := dingtalk.NewClient(dingtalk.Config{BaseURL: server.URL, LegacyBaseURL: server.URL, Secrets: secrets, Store: repository, AppKeySecretRef: "app-key", AppSecretRef: "app-secret", CardTemplateID: "template", CallbackRouteKey: "route"})
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	cfg.Auth.AccessSecret = "integration-test-secret-at-least-32-bytes"
	cfg.Auth.AccessExpire = 900
	service := svc.NewServiceContextWithDependencies(cfg, &svc.Dependencies{Store: repository, DingTalk: dingtalkClient, WeChat: wechatClient})
	mux := http.NewServeMux()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	serverHandler := rest.MustNewServer(rest.RestConf{Host: "127.0.0.1", Port: port})
	consolehandler.RegisterHandlers(serverHandler, service)
	go serverHandler.Start()
	t.Cleanup(serverHandler.Stop)
	backend := "http://127.0.0.1:" + strconv.Itoa(port)
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req, _ := http.NewRequestWithContext(r.Context(), r.Method, backend+r.URL.RequestURI(), bytes.NewReader(body))
		req.Header = r.Header.Clone()
		resp, requestErr := http.DefaultClient.Do(req)
		if requestErr != nil {
			http.Error(w, "backend unavailable", http.StatusServiceUnavailable)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
	mux.Handle("/api/v1/callbacks/dingtalk/card", DingTalkCallback{SVC: service, Secrets: secrets, SecretRef: "card-secret"})
	environment.console = httptest.NewServer(mux)
	t.Cleanup(environment.console.Close)
	environment.worker = &worker.Worker{Store: repository, Sender: wechatClient, Notifier: dingtalkClient, ConsoleURL: "https://console.example.test"}
	environment.token, err = auth.Issue(cfg.Auth.AccessSecret, agent, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return environment
}
func (e *integrationEnvironment) receive(t *testing.T, messageID string) domain.Conversation {
	t.Helper()
	body := `<xml><ToUserName>wx-test-app</ToUserName><FromUserName>customer-openid</FromUserName><CreateTime>` + strconv.FormatInt(time.Now().Unix(), 10) + `</CreateTime><MsgType>text</MsgType><Content>需要帮助</Content><MsgId>` + messageID + `</MsgId></xml>`
	query := url.Values{"timestamp": {strconv.FormatInt(time.Now().Unix(), 10)}, "nonce": {"nonce"}}
	query.Set("signature", wechat.PlainSignature("wechat-callback-token", query.Get("timestamp"), "nonce"))
	recorder := httptest.NewRecorder()
	callbackHandler := callback.NewHandler(e.store)
	callbackHandler.Secrets = integrationSecrets{"token": "wechat-callback-token"}
	callbackHandler.Receive(recorder, httptest.NewRequest(http.MethodPost, "/callbacks/wechat/service?"+query.Encode(), strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("callback status %d %s", recorder.Code, recorder.Body.String())
	}
	return e.conversation(t)
}
func (e *integrationEnvironment) conversation(t *testing.T) domain.Conversation {
	t.Helper()
	items, err := e.store.Conversations(context.Background(), store.ConversationFilter{})
	if err != nil || len(items) == 0 {
		t.Fatalf("no conversation: %v", err)
	}
	return items[0]
}
func (e *integrationEnvironment) request(t *testing.T, method, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	reader := bytes.NewReader(nil)
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, e.console.URL+path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	var decoded map[string]any
	_ = json.NewDecoder(response.Body).Decode(&decoded)
	return response.StatusCode, decoded
}
func TestEndToEndWeChatDingTalkClaimReply(t *testing.T) {
	e := newIntegrationEnvironment(t)
	conversation := e.receive(t, "message-1")
	e.receive(t, "message-1")
	if err := e.worker.ProcessOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.notifications.Load() != 1 {
		t.Fatalf("duplicate notification count = %d", e.notifications.Load())
	}
	conversation, _ = e.store.Conversation(context.Background(), conversation.ID)
	if conversation.AssigneeID != e.agent.ID {
		t.Fatalf("routing did not assign agent: %#v", conversation)
	}
	status, _ := e.request(t, http.MethodPost, "/api/v1/auth/dingtalk/login", map[string]string{"code": "single-use-code"}, "")
	if status != http.StatusOK {
		t.Fatalf("login failed %d", status)
	}
	status, _ = e.request(t, http.MethodGet, "/api/v1/conversations/"+conversation.ID, nil, e.token)
	if status != http.StatusOK {
		t.Fatalf("detail failed %d", status)
	}
	status, response := e.request(t, http.MethodPost, "/api/v1/conversations/"+conversation.ID+"/messages", map[string]string{"type": "text", "text": "您好，已收到您的反馈"}, e.token)
	if status != http.StatusOK {
		t.Fatalf("queue failed %d %#v", status, response)
	}
	if err := e.worker.ProcessOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.sentMu.Lock()
	defer e.sentMu.Unlock()
	if len(e.sent) != 1 || e.sent[0]["touser"] != "customer-openid" {
		t.Fatalf("unexpected WeChat send %#v", e.sent)
	}
	message, _ := e.store.Message(context.Background(), response["id"].(string))
	if message.Status != domain.StatusSent {
		t.Fatalf("message not sent %#v", message)
	}
}
func TestUnassignedAndUnauthorizedCannotReply(t *testing.T) {
	e := newIntegrationEnvironment(t)
	conversation := e.receive(t, "message-unassigned")
	status, _ := e.request(t, http.MethodPost, "/api/v1/conversations/"+conversation.ID+"/messages", map[string]string{"type": "text", "text": "not allowed"}, e.token)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("unassigned reply status = %d", status)
	}
	other := domain.Agent{ID: "agent-other", DingTalkUserID: "other", Role: domain.RoleAgent, Active: true}
	_ = e.store.SaveAgent(context.Background(), other)
	_ = e.store.SaveShift(context.Background(), domain.Shift{ID: "s2", AgentID: other.ID, StartAt: time.Now().Add(-time.Hour), EndAt: time.Now().Add(time.Hour), State: "active"})
	if err := e.worker.ProcessOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	otherToken, _ := auth.Issue("integration-test-secret-at-least-32-bytes", other, 10*time.Minute)
	status, _ = e.request(t, http.MethodGet, "/api/v1/conversations/"+conversation.ID, nil, otherToken)
	if status != http.StatusForbidden {
		t.Fatalf("other agent access = %d", status)
	}
}
func TestCardClaimRequiresSignature(t *testing.T) {
	e := newIntegrationEnvironment(t)
	conversation := e.receive(t, "message-card")
	content := `{"cardPrivateData":{"actionIds":["claim"],"params":{"conversationId":"` + conversation.ID + `"}}}`
	body := `{"type":"actionCallback","outTrackId":"track-1","userId":"ding-1","content":` + strconv.Quote(content) + `}`
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	req, _ := http.NewRequest(http.MethodPost, e.console.URL+"/api/v1/callbacks/dingtalk/card", strings.NewReader(body))
	req.Header.Set(dingtalk.SignatureTimestampHeader, timestamp)
	req.Header.Set(dingtalk.SignatureHeader, dingtalk.CardSignature("card-secret", timestamp))
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("card claim failed %d", response.StatusCode)
	}
	updated, _ := e.store.Conversation(context.Background(), conversation.ID)
	if updated.AssigneeID != e.agent.ID {
		t.Fatal("card claim did not persist")
	}
}
