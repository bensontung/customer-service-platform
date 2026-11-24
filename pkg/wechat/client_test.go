package wechat_test

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
	"github.com/example/customer-service-platform/pkg/wechat"
	"golang.org/x/time/rate"
)

type accountStore struct{ account domain.ChannelAccount }

func (s accountStore) AccountByKey(context.Context, string) (domain.ChannelAccount, error) {
	return s.account, nil
}

func (s accountStore) AccountByID(context.Context, string) (domain.ChannelAccount, error) {
	return s.account, nil
}

type secrets map[string]string

func (s secrets) Value(_ context.Context, reference string) (string, error) {
	value, ok := s[reference]
	if !ok {
		return "", errors.New("unresolved secret reference")
	}
	return value, nil
}

func testAccount() domain.ChannelAccount {
	return domain.ChannelAccount{ID: "account-id", AccountKey: "service", AppID: "app-id", AppSecretRef: "env://TEST_WECHAT_SECRET"}
}

func testClient(t *testing.T, server *httptest.Server, tokens wechat.TokenStore) *wechat.Client {
	t.Helper()
	client, err := wechat.NewClient(wechat.Config{
		BaseURL: server.URL, HTTPClient: server.Client(), Store: accountStore{testAccount()},
		Secrets: secrets{"env://TEST_WECHAT_SECRET": "test-only-secret"}, Tokens: tokens,
		Limiter: rate.NewLimiter(rate.Inf, 1), Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestSendMessageTypes(t *testing.T) {
	tokens := wechat.NewMemoryTokenStore()
	if err := tokens.Set(context.Background(), "account-id", "cached-token", time.Hour); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/cgi-bin/message/custom/send" || r.URL.Query().Get("access_token") != "cached-token" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["touser"] != "open-id" || body["msgtype"] == nil {
			t.Errorf("unexpected message identity")
		}
		switch body["msgtype"] {
		case "text":
			if body["text"].(map[string]any)["content"] != "hello" {
				t.Errorf("unexpected text body")
			}
		case "image", "voice":
			if body[body["msgtype"].(string)].(map[string]any)["media_id"] != "media-id" {
				t.Errorf("unexpected media body")
			}
		default:
			t.Errorf("unexpected message type")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer server.Close()
	client := testClient(t, server, tokens)
	for _, item := range []struct{ kind, text, media string }{{"text", "hello", ""}, {"image", "", "media-id"}, {"voice", "", "media-id"}} {
		if err := client.SendMessage(context.Background(), "service", "open-id", item.kind, item.text, item.media); err != nil {
			t.Fatalf("%s: %v", item.kind, err)
		}
	}
}

func TestTokenInvalidForcesRefreshOnce(t *testing.T) {
	tokens := wechat.NewMemoryTokenStore()
	if err := tokens.Set(context.Background(), "account-id", "expired-token", time.Hour); err != nil {
		t.Fatal(err)
	}
	var tokenCalls atomic.Int32
	var sendCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cgi-bin/stable_token":
			tokenCalls.Add(1)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["grant_type"] != "client_credential" || body["appid"] != "app-id" || body["secret"] != "test-only-secret" || body["force_refresh"] != true {
				t.Errorf("unexpected stable token request")
			}
			_, _ = io.WriteString(w, `{"access_token":"new-token","expires_in":7200}`)
		case "/cgi-bin/message/custom/send":
			call := sendCalls.Add(1)
			switch r.URL.Query().Get("access_token") {
			case "expired-token":
				if call != 1 {
					t.Errorf("cached token was reused after failure")
				}
				_, _ = io.WriteString(w, `{"errcode":40001,"errmsg":"invalid credential"}`)
			case "new-token":
				_, _ = io.WriteString(w, `{"errcode":0}`)
			default:
				t.Errorf("unexpected access token")
			}
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := testClient(t, server, tokens)
	if err := client.SendMessage(context.Background(), "service", "open-id", "text", "hello", ""); err != nil {
		t.Fatal(err)
	}
	if tokenCalls.Load() != 1 || sendCalls.Load() != 2 {
		t.Fatalf("token calls %d, send calls %d", tokenCalls.Load(), sendCalls.Load())
	}
}

func TestForceRefreshDoesNotReturnNormalFlightToken(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/cgi-bin/stable_token" {
			call := tokenCalls.Add(1)
			_, _ = io.WriteString(w, `{"access_token":"token-`+string(rune('0'+call))+`","expires_in":7200}`)
			return
		}
		if call := tokenCalls.Load(); call == 1 {
			_, _ = io.WriteString(w, `{"errcode":40001,"errmsg":"expired"}`)
			return
		}
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer server.Close()
	client := testClient(t, server, nil)
	if err := client.SendMessage(context.Background(), "service", "open-id", "text", "hello", ""); err != nil {
		t.Fatal(err)
	}
	if tokenCalls.Load() != 2 {
		t.Fatalf("expected force refresh, got %d fetches", tokenCalls.Load())
	}
}

func TestPersistentInvalidTokenStopsAfterOneRetry(t *testing.T) {
	tokens := wechat.NewMemoryTokenStore()
	_ = tokens.Set(context.Background(), "account-id", "expired-token", time.Hour)
	var sendCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/cgi-bin/stable_token" {
			_, _ = io.WriteString(w, `{"access_token":"still-invalid","expires_in":7200}`)
			return
		}
		sendCalls.Add(1)
		_, _ = io.WriteString(w, `{"errcode":40001,"errmsg":"invalid token"}`)
	}))
	defer server.Close()
	client := testClient(t, server, tokens)
	err := client.SendMessage(context.Background(), "service", "open-id", "text", "hello", "")
	var apiErr *wechat.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 40001 || sendCalls.Load() != 2 {
		t.Fatalf("expected exactly one refresh/retry, calls=%d err=%v", sendCalls.Load(), err)
	}
}

func TestNetworkErrorDoesNotExposeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()
	tokens := wechat.NewMemoryTokenStore()
	_ = tokens.Set(context.Background(), "account-id", "sensitive-token", time.Hour)
	client := testClient(t, server, tokens)
	err := client.SendMessage(context.Background(), "service", "open-id", "text", "hello", "")
	if err == nil || strings.Contains(err.Error(), "sensitive-token") || !wechat.IsRetryable(err) {
		t.Fatalf("unexpected sanitized network error %v", err)
	}
}

func TestTokenCacheAndSingleFlight(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/cgi-bin/stable_token" {
			tokenCalls.Add(1)
			time.Sleep(20 * time.Millisecond)
			_, _ = io.WriteString(w, `{"access_token":"cached-token","expires_in":7200}`)
			return
		}
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer server.Close()
	client := testClient(t, server, nil)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := client.SendMessage(context.Background(), "service", "open-id", "text", "hello", ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if tokenCalls.Load() != 1 {
		t.Fatalf("expected one token fetch, got %d", tokenCalls.Load())
	}
}

func TestRetryableErrorsAndRateLimiter(t *testing.T) {
	tokens := wechat.NewMemoryTokenStore()
	_ = tokens.Set(context.Background(), "account-id", "token", time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"errcode":45009,"errmsg":"rate limit"}`)
	}))
	client := testClient(t, server, tokens)
	err := client.SendMessage(context.Background(), "service", "open-id", "text", "hello", "")
	server.Close()
	if !wechat.IsRetryable(err) {
		t.Fatalf("expected retryable error, got %v", err)
	}
	if strings.Contains(err.Error(), "cached-token") || strings.Contains(err.Error(), "test-only-secret") {
		t.Fatal("credentials leaked into error")
	}
	if err := wechatErrorForTest(45015); !errors.Is(err, domain.ErrReplyExpired) {
		t.Fatal("reply window error was not mapped")
	}

	limiter := rate.NewLimiter(0, 1)
	reservation := limiter.Reserve()
	if !reservation.OK() {
		t.Fatal("could not initialize limited reservation")
	}
	client, err = wechat.NewClient(wechat.Config{
		BaseURL: "http://127.0.0.1", Store: accountStore{testAccount()}, Secrets: secrets{"env://TEST_WECHAT_SECRET": "test-only-secret"},
		Tokens: tokens, Limiter: limiter, Timeout: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.SendMessage(context.Background(), "service", "open-id", "text", "hello", "")
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected rate limit wait failure, got %v", err)
	}
}

func wechatErrorForTest(code int) error {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"errcode": code, "errmsg": "platform error"})
	}))
	defer server.Close()
	tokens := wechat.NewMemoryTokenStore()
	_ = tokens.Set(context.Background(), "account-id", "token", time.Hour)
	client, err := wechat.NewClient(wechat.Config{
		BaseURL: server.URL, HTTPClient: server.Client(), Store: accountStore{testAccount()}, Secrets: secrets{"env://TEST_WECHAT_SECRET": "test-only-secret"},
		Tokens: tokens, Limiter: rate.NewLimiter(rate.Inf, 1),
	})
	if err != nil {
		return err
	}
	return client.SendMessage(context.Background(), "service", "open-id", "text", "hello", "")
}

func TestRequestTimeout(t *testing.T) {
	tokens := wechat.NewMemoryTokenStore()
	_ = tokens.Set(context.Background(), "account-id", "token", time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	client, err := wechat.NewClient(wechat.Config{
		BaseURL: server.URL, HTTPClient: server.Client(), Store: accountStore{testAccount()},
		Secrets: secrets{"env://TEST_WECHAT_SECRET": "test-only-secret"}, Tokens: tokens,
		Limiter: rate.NewLimiter(rate.Inf, 1), Timeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.SendMessage(context.Background(), "service", "open-id", "text", "hello", "")
	if !errors.Is(err, context.DeadlineExceeded) || !wechat.IsRetryable(err) {
		t.Fatalf("expected retryable deadline, got %v", err)
	}
}

func TestCommentsAndActions(t *testing.T) {
	tokens := wechat.NewMemoryTokenStore()
	_ = tokens.Set(context.Background(), "account-id", "token", time.Hour)
	var actions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/cgi-bin/comment/list" {
			if body["msg_data_id"] != float64(123456) || body["index"] != float64(0) || body["begin"] != float64(49) || body["count"] != float64(49) {
				t.Errorf("unexpected list body %#v", body)
			}
			_, _ = io.WriteString(w, `{"errcode":0,"total":1,"comment":[{"user_comment_id":77,"openid":"reader","content":"useful","comment_type":0,"create_time":1700000000,"reply":{"content":"thanks","create_time":1700000010}}]}`)
			return
		}
		actions = append(actions, r.URL.Path)
		if body["msg_data_id"] != float64(123456) || body["index"] != float64(0) || body["user_comment_id"] != float64(77) {
			t.Errorf("unexpected comment action body %#v", body)
		}
		if r.URL.Path == "/cgi-bin/comment/reply/add" && body["content"] != "thanks" {
			t.Errorf("missing reply content")
		}
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer server.Close()
	client := testClient(t, server, tokens)
	comments, err := client.SyncComments(context.Background(), "service", "123456", 0, 49)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments %#v: %v", comments, err)
	}
	comment := comments[0]
	comment.ChannelAccountID = "service"
	if comment.CommentID != "77" || comment.ReplyContent != "thanks" || comment.ArticleIndex != 0 {
		t.Fatalf("unexpected comment %#v", comment)
	}
	for _, action := range []string{"reply", "feature", "unfeature", "delete"} {
		if err := client.CommentAction(context.Background(), comment, action, "thanks"); err != nil {
			t.Fatal(err)
		}
	}
	expected := []string{"/cgi-bin/comment/reply/add", "/cgi-bin/comment/markelect", "/cgi-bin/comment/unmarkelect", "/cgi-bin/comment/delete"}
	for i := range expected {
		if actions[i] != expected[i] {
			t.Fatalf("unexpected action paths %#v", actions)
		}
	}
}
