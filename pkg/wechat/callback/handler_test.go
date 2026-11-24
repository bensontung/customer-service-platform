package callback

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/example/customer-service-platform/pkg/wechat"
)

type testSecrets map[string]string

func (s testSecrets) Value(_ context.Context, ref string) (string, error) { return s[ref], nil }

func callbackFixture(t *testing.T) (*Handler, *store.Memory, domain.ChannelAccount) {
	t.Helper()
	repository := store.NewMemory()
	account := domain.ChannelAccount{ID: "account-1", AccountKey: "service", Channel: domain.ChannelService, AppID: "wx-test-app", TokenSecretRef: "token", AESKeySecretRef: "aes", AppSecretRef: "secret", Status: "active"}
	if err := repository.SaveAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	key := "0123456789012345678901234567890123456789012"
	return &Handler{Store: repository, Secrets: testSecrets{"token": "callback-token", "aes": key}, Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}, repository, account
}
func TestVerifyAndReceiveEncryptedIdempotently(t *testing.T) {
	handler, repository, account := callbackFixture(t)
	plain := `<xml><ToUserName>wx-test-app</ToUserName><FromUserName>customer-openid</FromUserName><CreateTime>1700000000</CreateTime><MsgType>text</MsgType><Content>你好</Content><MsgId>message-1</MsgId></xml>`
	encrypted, err := wechat.Encrypt(plain, account.AppID, "0123456789012345678901234567890123456789012")
	if err != nil {
		t.Fatal(err)
	}
	body := `<xml><Encrypt>` + encrypted + `</Encrypt></xml>`
	query := url.Values{"timestamp": {"1700000000"}, "nonce": {"nonce"}, "msg_signature": {wechat.MessageSignature("callback-token", "1700000000", "nonce", encrypted)}}
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/callbacks/wechat/service?"+query.Encode(), strings.NewReader(body))
		recorder := httptest.NewRecorder()
		handler.Receive(recorder, req)
		if recorder.Code != http.StatusOK || recorder.Body.String() != "success" {
			t.Fatalf("callback %d: %d %s", i, recorder.Code, recorder.Body.String())
		}
	}
	conversations, err := repository.Conversations(context.Background(), store.ConversationFilter{})
	if err != nil || len(conversations) != 1 {
		t.Fatalf("unexpected conversations %#v %v", conversations, err)
	}
	messages, _ := repository.Messages(context.Background(), conversations[0].ID)
	if len(messages) != 1 {
		t.Fatalf("duplicate message: %#v", messages)
	}
	query = url.Values{"timestamp": {"1700000000"}, "nonce": {"nonce"}, "signature": {wechat.PlainSignature("callback-token", "1700000000", "nonce")}, "echostr": {"echo"}}
	verify := httptest.NewRequest(http.MethodGet, "/callbacks/wechat/service?"+query.Encode(), nil)
	recorder := httptest.NewRecorder()
	handler.Verify(recorder, verify)
	if recorder.Body.String() != "echo" {
		t.Fatalf("verify body = %q", recorder.Body.String())
	}
}
func TestRejectsInvalidSignature(t *testing.T) {
	handler, repository, _ := callbackFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/callbacks/wechat/service?timestamp=1700000000&nonce=n&signature=bad", strings.NewReader(`<xml><ToUserName>wx-test-app</ToUserName><FromUserName>u</FromUserName><MsgType>text</MsgType><Content>x</Content><MsgId>m</MsgId></xml>`))
	recorder := httptest.NewRecorder()
	handler.Receive(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatal(recorder.Code)
	}
	conversations, _ := repository.Conversations(context.Background(), store.ConversationFilter{})
	if len(conversations) != 0 {
		t.Fatal("invalid callback persisted")
	}
}
func TestRejectsWrongAppCipher(t *testing.T) {
	handler, _, _ := callbackFixture(t)
	encrypted, _ := wechat.Encrypt(`<xml/>`, "another-app", "0123456789012345678901234567890123456789012")
	query := fmt.Sprintf("timestamp=1700000000&nonce=n&msg_signature=%s", wechat.MessageSignature("callback-token", "1700000000", "n", encrypted))
	recorder := httptest.NewRecorder()
	handler.Receive(recorder, httptest.NewRequest(http.MethodPost, "/callbacks/wechat/service?"+query, strings.NewReader(`<xml><Encrypt>`+encrypted+`</Encrypt></xml>`)))
	if recorder.Code != http.StatusForbidden {
		t.Fatal(recorder.Code)
	}
}
