package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

const DefaultBaseURL = "https://api.weixin.qq.com"

var ErrRateLimited = errors.New("wechat API rate limited")

type AccountStore interface {
	AccountByKey(context.Context, string) (domain.ChannelAccount, error)
}

type AccountByIDStore interface {
	AccountByID(context.Context, string) (domain.ChannelAccount, error)
}

type TokenStore interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string, time.Duration) error
	Delete(context.Context, string) error
}

type tokenEntry struct {
	value     string
	expiresAt time.Time
}

type MemoryTokenStore struct {
	mu     sync.RWMutex
	tokens map[string]tokenEntry
}

func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{tokens: make(map[string]tokenEntry)}
}

func (s *MemoryTokenStore) Get(_ context.Context, key string) (string, error) {
	s.mu.RLock()
	entry, ok := s.tokens[key]
	s.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return "", nil
	}
	return entry.value, nil
}

func (s *MemoryTokenStore) Set(_ context.Context, key, token string, ttl time.Duration) error {
	if token == "" || ttl <= 0 {
		return nil
	}
	s.mu.Lock()
	s.tokens[key] = tokenEntry{value: token, expiresAt: time.Now().Add(ttl)}
	s.mu.Unlock()
	return nil
}

func (s *MemoryTokenStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	delete(s.tokens, key)
	s.mu.Unlock()
	return nil
}

type RedisTokenStore struct {
	client redis.UniversalClient
}

func NewRedisTokenStore(client redis.UniversalClient) *RedisTokenStore {
	return &RedisTokenStore{client: client}
}

func (s *RedisTokenStore) Get(ctx context.Context, key string) (string, error) {
	token, err := s.client.Get(ctx, tokenRedisKey(key)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return token, err
}

func (s *RedisTokenStore) Set(ctx context.Context, key, token string, ttl time.Duration) error {
	if token == "" || ttl <= 0 {
		return nil
	}
	return s.client.Set(ctx, tokenRedisKey(key), token, ttl).Err()
}

func (s *RedisTokenStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, tokenRedisKey(key)).Err()
}

func tokenRedisKey(key string) string { return "wechat:access_token:" + key }

type Config struct {
	BaseURL    string
	HTTPClient *http.Client
	Store      AccountStore
	Secrets    domain.Secrets
	Tokens     TokenStore
	Limiter    *rate.Limiter
	Timeout    time.Duration
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	store      AccountStore
	secrets    domain.Secrets
	tokens     TokenStore
	limiter    *rate.Limiter
	timeout    time.Duration
	flights    map[string]*tokenFlight
	mu         sync.Mutex
}

type tokenFlight struct {
	done  chan struct{}
	force bool
	token string
	err   error
}

type WechatClient = Client

func NewClient(config Config) (*Client, error) {
	if config.Store == nil || config.Secrets == nil {
		return nil, errors.New("wechat store and secrets are required")
	}
	baseURL, err := parseBaseURL(config.BaseURL, DefaultBaseURL)
	if err != nil {
		return nil, err
	}
	tokens := config.Tokens
	if tokens == nil {
		tokens = NewMemoryTokenStore()
	}
	limiter := config.Limiter
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Limit(5), 1)
	}
	return &Client{

		baseURL: baseURL, httpClient: httpClient(config.HTTPClient), store: config.Store,
		secrets: config.Secrets, tokens: tokens, limiter: limiter,
		timeout: requestTimeout(config.Timeout), flights: make(map[string]*tokenFlight),
	}, nil
}

func NewWechatClient(config Config) (*Client, error) { return NewClient(config) }

func (c *Client) SendMessage(ctx context.Context, account, openid, messageType, text, mediaID string) error {
	if openid == "" {
		return fmt.Errorf("%w: openid is required", domain.ErrInvalidRequest)
	}
	body := map[string]any{"touser": openid, "msgtype": messageType}
	switch messageType {
	case "text":
		if text == "" {
			return fmt.Errorf("%w: text is required", domain.ErrInvalidRequest)
		}
		body["text"] = map[string]string{"content": text}
	case "image", "voice":
		if mediaID == "" {
			return fmt.Errorf("%w: media ID is required", domain.ErrInvalidRequest)
		}
		body[messageType] = map[string]string{"media_id": mediaID}
	default:
		return fmt.Errorf("%w: unsupported wechat message type", domain.ErrInvalidRequest)
	}
	return c.authenticated(ctx, account, "/cgi-bin/message/custom/send", body, nil)
}

func (c *Client) SyncComments(ctx context.Context, accountKey, msgDataID string, index, begin int) ([]domain.Comment, error) {
	if msgDataID == "" || index < 0 || index > 7 || begin < 0 {
		return nil, fmt.Errorf("%w: invalid comment article parameters", domain.ErrInvalidRequest)
	}
	account, err := c.account(ctx, accountKey)
	if err != nil {
		return nil, err
	}
	var result struct {
		Total   int `json:"total"`
		Comment []struct {
			UserCommentID int64  `json:"user_comment_id"`
			OpenID        string `json:"openid"`
			Content       string `json:"content"`
			CommentType   int    `json:"comment_type"`
			CreateTime    int64  `json:"create_time"`
			Reply         *struct {
				Content    string `json:"content"`
				CreateTime int64  `json:"create_time"`
			} `json:"reply"`
		} `json:"comment"`
	}
	var msgDataIDValue any = msgDataID
	if numericID, err := strconv.ParseInt(msgDataID, 10, 64); err == nil {
		msgDataIDValue = numericID
	}
	requestBody := struct {
		MsgDataID any `json:"msg_data_id"`
		Index     int `json:"index"`
		Begin     int `json:"begin"`
		Count     int `json:"count"`
	}{MsgDataID: msgDataIDValue, Index: index, Begin: begin, Count: 49}
	if err := c.authenticated(ctx, accountKey, "/cgi-bin/comment/list", requestBody, &result); err != nil {
		return nil, err
	}
	comments := make([]domain.Comment, 0, len(result.Comment))
	for _, item := range result.Comment {
		updatedAt := time.Unix(item.CreateTime, 0).UTC()
		if item.CreateTime == 0 {
			updatedAt = time.Now().UTC()
		}
		comment := domain.Comment{
			ID: domain.NewID(), ChannelAccountID: account.ID, MsgDataID: msgDataID, ArticleIndex: index,
			CommentID: strconv.FormatInt(item.UserCommentID, 10), AuthorID: item.OpenID,
			Content: item.Content, Status: "normal", SyncedAt: time.Now().UTC(), UpdatedAt: updatedAt,
		}
		if item.CommentType != 0 {
			comment.Status = strconv.Itoa(item.CommentType)
		}
		if item.Reply != nil {
			comment.ReplyContent = item.Reply.Content
			comment.Status = "replied"
		}
		comments = append(comments, comment)
	}
	return comments, nil
}

func (c *Client) CommentAction(ctx context.Context, comment domain.Comment, action, content string) error {
	if comment.MsgDataID == "" || comment.ArticleIndex < 0 || comment.ArticleIndex > 7 || comment.CommentID == "" {
		return fmt.Errorf("%w: incomplete comment identity", domain.ErrInvalidRequest)
	}
	commentID, err := strconv.ParseInt(comment.CommentID, 10, 64)
	if err != nil || commentID <= 0 {
		return fmt.Errorf("%w: invalid comment ID", domain.ErrInvalidRequest)
	}
	var msgDataID any = comment.MsgDataID
	if numericID, err := strconv.ParseInt(comment.MsgDataID, 10, 64); err == nil {
		msgDataID = numericID
	}
	body := map[string]any{
		"msg_data_id": msgDataID, "index": comment.ArticleIndex, "user_comment_id": commentID,
	}
	var path string
	switch action {
	case "reply":
		if content == "" {
			return fmt.Errorf("%w: reply content is required", domain.ErrInvalidRequest)
		}
		path, body["content"] = "/cgi-bin/comment/reply/add", content
	case "feature":
		path = "/cgi-bin/comment/markelect"
	case "unfeature":
		path = "/cgi-bin/comment/unmarkelect"
	case "delete":
		path = "/cgi-bin/comment/delete"
	default:
		return fmt.Errorf("%w: unsupported comment action", domain.ErrInvalidRequest)
	}
	return c.authenticated(ctx, comment.ChannelAccountID, path, body, nil)
}

func (c *Client) account(ctx context.Context, key string) (domain.ChannelAccount, error) {
	if key == "" {
		return domain.ChannelAccount{}, fmt.Errorf("%w: account is required", domain.ErrInvalidRequest)
	}
	account, err := c.store.AccountByKey(ctx, key)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			if store, ok := c.store.(AccountByIDStore); ok {
				return store.AccountByID(ctx, key)
			}
		}
		return domain.ChannelAccount{}, fmt.Errorf("load wechat account: %w", err)
	}
	return account, nil
}

func (c *Client) authenticated(ctx context.Context, accountKey, path string, body, output any) error {
	account, err := c.account(ctx, accountKey)
	if err != nil {
		return err
	}
	if account.AppID == "" || account.AppSecretRef == "" {
		return errors.New("wechat account credential references are not configured")
	}
	token, err := c.accessToken(ctx, account, false)
	if err != nil {
		return err
	}
	err = c.post(ctx, path, token, body, output)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.TokenInvalid() {
		token, err = c.accessToken(ctx, account, true)
		if err != nil {
			return err
		}
		return c.post(ctx, path, token, body, output)
	}
	return err
}

func (c *Client) accessToken(ctx context.Context, account domain.ChannelAccount, force bool) (string, error) {
	key := account.ID
	if key == "" {
		key = account.AppID
	}
	for {
		if !force {
			token, err := c.tokens.Get(ctx, key)
			if err != nil {
				return "", fmt.Errorf("read wechat token cache: %w", err)
			}
			if token != "" {
				return token, nil
			}
		}
		c.mu.Lock()
		if flight, ok := c.flights[key]; ok {
			force = force || flight.force
			c.mu.Unlock()
			select {
			case <-flight.done:
				if !force || (flight.force && flight.err == nil) {
					return flight.token, flight.err
				}
			case <-ctx.Done():
				return "", ctx.Err()
			}
			continue
		}
		flight := &tokenFlight{done: make(chan struct{}), force: force}
		c.flights[key] = flight
		c.mu.Unlock()
		defer c.finishFlight(key, flight)
		flight.token, flight.err = c.fetchToken(ctx, account, key, force)
		return flight.token, flight.err
	}
}

func (c *Client) finishFlight(key string, flight *tokenFlight) {
	c.mu.Lock()
	delete(c.flights, key)
	c.mu.Unlock()
	close(flight.done)
}

func (c *Client) fetchToken(ctx context.Context, account domain.ChannelAccount, key string, force bool) (string, error) {
	secret, err := c.secrets.Value(ctx, account.AppSecretRef)
	if err != nil {
		return "", fmt.Errorf("resolve wechat app secret: %w", err)
	}
	if secret == "" {
		return "", errors.New("wechat app secret is empty")
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	body := struct {
		GrantType string `json:"grant_type"`
		AppID     string `json:"appid"`
		Secret    string `json:"secret"`
		Force     bool   `json:"force_refresh"`
	}{GrantType: "client_credential", AppID: account.AppID, Secret: secret, Force: force}
	if err = c.post(ctx, "/cgi-bin/stable_token", "", body, &result); err != nil {
		return "", err
	}
	if result.AccessToken == "" {
		return "", errors.New("wechat token response has no access token")
	}
	if account.ID != "" {
		if err = c.tokens.Delete(ctx, key); err != nil {
			return "", fmt.Errorf("invalidate wechat token cache: %w", err)
		}
	}
	ttl := time.Duration(result.ExpiresIn)*time.Second - 5*time.Minute
	if result.ExpiresIn <= 0 {
		ttl = 90 * time.Minute
	} else if ttl <= 0 {
		ttl = time.Duration(result.ExpiresIn) * time.Second / 2
	}
	if err = c.tokens.Set(ctx, key, result.AccessToken, ttl); err != nil {
		return "", fmt.Errorf("write wechat token cache: %w", err)
	}
	return result.AccessToken, nil
}

type APIError struct {
	Code      int
	Message   string
	Status    int
	Retryable bool
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("wechat API error %d", e.Code)
	}
	return fmt.Sprintf("wechat API HTTP status %d", e.Status)
}

func (e *APIError) Is(target error) bool {
	return target == ErrRateLimited && (e.Status == http.StatusTooManyRequests || e.Code == 45009 || e.Code == 45011)
}

func (e *APIError) Retry() bool { return e.Retryable }

func (e *APIError) TokenInvalid() bool {
	switch e.Code {
	case 40001, 40014, 42001, 42007, 42009, 42010:
		return true
	default:
		return false
	}
}

func IsRetryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, domain.ErrInvalidRequest)
}

func (c *Client) post(ctx context.Context, path, token string, body, output any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint, err := c.endpoint(path, token)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err = c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("wait for wechat rate limit: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("wechat API request: %w", sanitizeRequestError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read wechat API response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return &APIError{Status: resp.StatusCode, Retryable: retryable}
	}
	var envelope struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return errors.New("decode wechat API response")
	}
	if envelope.ErrCode != 0 {
		return wechatError(envelope.ErrCode, envelope.ErrMsg)
	}
	if output != nil && len(data) != 0 {
		if err = json.Unmarshal(data, output); err != nil {
			return errors.New("decode wechat API result")
		}
	}
	return nil
}

func wechatError(code int, message string) error {
	retryable := code == -1 || code == 45009 || code == 45011 || code == 45056 || code == 45058
	apiErr := &APIError{Code: code, Message: message, Retryable: retryable}
	if retryable && code != -1 {
		apiErr = &APIError{Code: code, Message: message, Retryable: true, Status: http.StatusTooManyRequests}
	}
	switch code {
	case 45015:
		return errors.Join(domain.ErrReplyExpired, apiErr)
	case 45047:
		return errors.Join(domain.ErrReplyQuota, apiErr)
	default:
		return apiErr
	}
}

func (c *Client) endpoint(path, token string) (string, error) {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	query := endpoint.Query()
	if token != "" {
		query.Set("access_token", token)
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func parseBaseURL(value, fallback string) (*url.URL, error) {
	if value == "" {
		value = fallback
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid API base URL")
	}
	return parsed, nil
}

var accessTokenPattern = regexp.MustCompile(`([?&]access_token=)[^&]*`)

func sanitizeRequestError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		clone := *urlErr
		clone.URL = accessTokenPattern.ReplaceAllString(clone.URL, "${1}[REDACTED]")
		return &clone
	}
	return err
}

func httpClient(client *http.Client) *http.Client {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client = &http.Client{Transport: transport, Timeout: 10 * time.Second}
	} else {
		clone := *client
		client = &clone
	}
	if client.CheckRedirect == nil {
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}

func requestTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return 10 * time.Second
}
