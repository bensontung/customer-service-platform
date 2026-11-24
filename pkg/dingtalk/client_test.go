package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
	"golang.org/x/time/rate"
)

type secrets map[string]string

func (s secrets) Value(_ context.Context, reference string) (string, error) {
	value, ok := s[reference]
	if !ok {
		return "", errors.New("unresolved secret reference")
	}
	return value, nil
}

type agentStore struct{ agent domain.Agent }

func (s agentStore) AgentByID(context.Context, string) (domain.Agent, error) {
	return s.agent, nil
}

func testSecrets() secrets {
	return secrets{
		"env://TEST_DINGTALK_APP_KEY":    "test-app-key",
		"env://TEST_DINGTALK_APP_SECRET": "test-app-secret",
	}
}

func testConfig(server *httptest.Server) Config {
	return Config{
		BaseURL: server.URL, LegacyBaseURL: server.URL, HTTPClient: server.Client(),
		Secrets: testSecrets(), AppKeySecretRef: "env://TEST_DINGTALK_APP_KEY", AppSecretRef: "env://TEST_DINGTALK_APP_SECRET",
		Store:          agentStore{domain.Agent{ID: "agent-id", DingTalkUserID: "ding-user", Active: true}},
		CardTemplateID: "template-id", CallbackRouteKey: "notification-route",
		Limiter: rate.NewLimiter(rate.Inf, 1), Timeout: time.Second, RetryDelay: time.Millisecond,
	}
}

func TestExchangeLoginCode(t *testing.T) {
	var tokenCalls atomic.Int32
	var userCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			tokenCalls.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("access_token") != "" {
				t.Errorf("unexpected token request")
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["appKey"] != "test-app-key" || body["appSecret"] != "test-app-secret" {
				t.Errorf("unexpected token body")
			}
			_, _ = io.WriteString(w, `{"accessToken":"ding-token","expireIn":7200}`)
		case "/topapi/v2/user/getuserinfo":
			userCalls.Add(1)
			if r.URL.Query().Get("access_token") != "ding-token" {
				t.Errorf("missing access token")
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["code"] != "temporary-code" {
				t.Errorf("unexpected login code body")
			}
			_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok","result":{"userid":"user-1","unionid":"union-1","name":"Agent"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	user, err := client.ExchangeLoginCode(context.Background(), "temporary-code")
	if err != nil || user.UserID != "user-1" || user.UnionID != "union-1" || user.Name != "Agent" {
		t.Fatalf("unexpected user %#v: %v", user, err)
	}
	if _, err = client.ExchangeLoginCode(context.Background(), "temporary-code"); err != nil {
		t.Fatal(err)
	}
	if tokenCalls.Load() != 1 || userCalls.Load() != 2 {
		t.Fatalf("unexpected call counts token=%d user=%d", tokenCalls.Load(), userCalls.Load())
	}
}

func TestNotifyCardAndInvalidToken(t *testing.T) {
	var tokenCalls atomic.Int32
	var cardCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			if tokenCalls.Add(1) == 1 {
				_, _ = io.WriteString(w, `{"accessToken":"expired-token","expireIn":7200}`)
			} else {
				_, _ = io.WriteString(w, `{"accessToken":"fresh-token","expireIn":7200}`)
			}
			return
		}
		if r.URL.Path != "/v1.0/card/instances/createAndDeliver" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		call := cardCalls.Add(1)
		if (call == 1 && r.Header.Get("x-acs-dingtalk-access-token") != "expired-token") || (call == 2 && r.Header.Get("x-acs-dingtalk-access-token") != "fresh-token") || r.URL.Query().Get("access_token") != "" {
			t.Errorf("token refresh did not happen")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["cardTemplateId"] != "template-id" || body["openSpaceId"] != "dtv1.card//IM_SINGLE.ding-user" || body["callbackType"] != "HTTP" || body["callbackRouteKey"] != "notification-route" || body["outTrackId"] == "" {
			t.Errorf("unexpected card fields %#v", body)
		}
		if body["userIdModel"].(map[string]any)["userId"] != "ding-user" {
			t.Errorf("unexpected user model")
		}
		cardData := body["cardData"].(map[string]any)["cardParamMap"].(map[string]any)
		if cardData["title"] != "New conversation" || cardData["conversationId"] != "conversation-id" || cardData["url"] != "https://console.example.test/conversation" {
			t.Errorf("unexpected card data")
		}
		if call == 1 {
			_, _ = io.WriteString(w, `{"errcode":40001,"errmsg":"invalid credential"}`)
		} else {
			_, _ = io.WriteString(w, `{"errcode":0,"result":{"processQueryKey":"instance-id"}}`)
		}
	}))
	defer server.Close()
	client, err := NewClient(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	notification := domain.Notification{
		AgentID: "agent-id", ConversationID: "conversation-id", Kind: "new", Title: "New conversation",
		Text: "Customer message", URL: "https://console.example.test/conversation",
	}
	if err = client.Notify(context.Background(), notification); err != nil {
		t.Fatal(err)
	}
	if tokenCalls.Load() != 2 || cardCalls.Load() != 2 {
		t.Fatalf("unexpected call counts token=%d card=%d", tokenCalls.Load(), cardCalls.Load())
	}
}

func TestNotifyRateLimitRetriesAndClassifies(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			_, _ = io.WriteString(w, `{"accessToken":"test-token","expireIn":7200}`)
			return
		}
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"code":"Throttling.User","message":"test-token should not be reflected"}`)
	}))
	defer server.Close()
	config := testConfig(server)
	config.MaxAttempts = 2
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Notify(context.Background(), domain.Notification{AgentID: "agent-id", Kind: "new"})
	if !IsRetryable(err) || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rate limit error, got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected two attempts, got %d", calls.Load())
	}
	if strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), "test-app-secret") {
		t.Fatal("credential leaked in error")
	}
}

func TestTokenConcurrentCacheAndLocalRateLimit(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			tokenCalls.Add(1)
			time.Sleep(15 * time.Millisecond)
			_, _ = io.WriteString(w, `{"accessToken":"cached-token","expireIn":7200}`)
			return
		}
		_, _ = io.WriteString(w, `{"errcode":0,"result":{"userid":"user-1"}}`)
	}))
	defer server.Close()
	client, err := NewClient(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.ExchangeLoginCode(context.Background(), "code"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if tokenCalls.Load() != 1 {
		t.Fatalf("expected one token fetch, got %d", tokenCalls.Load())
	}

	var userCalls atomic.Int32
	limitedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			_, _ = io.WriteString(w, `{"accessToken":"cached-token","expireIn":7200}`)
			return
		}
		userCalls.Add(1)
		_, _ = io.WriteString(w, `{"errcode":0,"result":{"userid":"user-1"}}`)
	}))
	defer limitedServer.Close()
	config := testConfig(limitedServer)
	limiter := rate.NewLimiter(0, 1)
	if !limiter.Reserve().OK() {
		t.Fatal("could not initialize limiter")
	}
	config.Limiter = limiter
	config.Timeout = 30 * time.Millisecond
	limited, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	limited.mu.Lock()
	limited.token = "cached-token"
	limited.expiresAt = time.Now().Add(time.Hour)
	limited.mu.Unlock()
	if _, err = limited.ExchangeLoginCode(context.Background(), "code"); err == nil {
		t.Fatal("expected local rate limit error")
	}
	if _, err = limited.ExchangeLoginCode(context.Background(), "code"); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected rate limit wait error, got %v", err)
	}
	if userCalls.Load() != 0 {
		t.Fatalf("rate limited request was sent: %d", userCalls.Load())
	}
}

func TestTimeoutAndNoLoginRetry(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			_, _ = io.WriteString(w, `{"accessToken":"test-token","expireIn":7200}`)
			return
		}
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()
	config := testConfig(server)
	config.Timeout = 20 * time.Millisecond
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExchangeLoginCode(context.Background(), "code")
	if !errors.Is(err, context.DeadlineExceeded) || !IsRetryable(err) {
		t.Fatalf("expected retryable timeout, got %v", err)
	}
}

func TestNetworkErrorDoesNotExposeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()
	client, err := NewClient(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExchangeLoginCode(context.Background(), "code")
	if err == nil || strings.Contains(err.Error(), "access_token") {
		t.Fatalf("unexpected token error %v", err)
	}
	client.token = "sensitive-token"
	client.expiresAt = time.Now().Add(time.Hour)
	_, err = client.ExchangeLoginCode(context.Background(), "code")
	if err == nil || strings.Contains(err.Error(), "sensitive-token") || !IsRetryable(err) {
		t.Fatalf("unexpected sanitized network error %v", err)
	}
}

func TestTokenInvalidLoginRefreshesOnce(t *testing.T) {
	var tokenCalls atomic.Int32
	var userCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			tokenCalls.Add(1)
			_, _ = io.WriteString(w, `{"accessToken":"new-token","expireIn":7200}`)
			return
		}
		userCalls.Add(1)
		_, _ = io.WriteString(w, `{"errcode":40001,"errmsg":"invalid token"}`)
	}))
	defer server.Close()
	client, err := NewClient(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExchangeLoginCode(context.Background(), "one-time-code")
	if err == nil || tokenCalls.Load() != 2 || userCalls.Load() != 2 {
		t.Fatalf("expected one refresh and one replay, token=%d user=%d err=%v", tokenCalls.Load(), userCalls.Load(), err)
	}
}
