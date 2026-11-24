package dingtalk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"golang.org/x/time/rate"
)

const (
	DefaultBaseURL       = "https://api.dingtalk.com"
	DefaultLegacyBaseURL = "https://oapi.dingtalk.com"
)

var ErrRateLimited = errors.New("DingTalk API rate limited")

type AgentStore interface {
	AgentByID(context.Context, string) (domain.Agent, error)
}

type Config struct {
	BaseURL       string
	LegacyBaseURL string
	HTTPClient    *http.Client
	Secrets       domain.Secrets
	Store         AgentStore

	AppKeySecretRef  string
	AppKeyRef        string
	AppSecretRef     string
	CardTemplateID   string
	CardSecretRef    string
	APISecretRef     string
	CallbackRouteKey string
	AESKeySecretRef  string
	CorpIDSecretRef  string
	Limiter          *rate.Limiter
	Timeout          time.Duration
	MaxAttempts      int
	RetryDelay       time.Duration
	Now              func() time.Time
}

type Client struct {
	baseURL       *url.URL
	legacyBaseURL *url.URL
	httpClient    *http.Client
	secrets       domain.Secrets
	store         AgentStore
	config        Config
	limiter       *rate.Limiter

	mu        sync.Mutex
	token     string
	expiresAt time.Time
	flight    *tokenFlight
}

type tokenFlight struct {
	done  chan struct{}
	token string
	err   error
}

type DingTalkClient = Client

type UserInfo struct {
	UserID  string `json:"userid"`
	UnionID string `json:"unionid"`
	Name    string `json:"name"`
}

func NewClient(config Config) (*Client, error) {
	if config.Secrets == nil {
		return nil, errors.New("DingTalk secrets are required")
	}
	baseURL, err := parseBaseURL(config.BaseURL, DefaultBaseURL)
	if err != nil {
		return nil, err
	}
	legacyBaseURL, err := parseBaseURL(config.LegacyBaseURL, DefaultLegacyBaseURL)
	if err != nil {
		return nil, err
	}
	if config.AppKeySecretRef == "" {
		config.AppKeySecretRef = config.AppKeyRef
	}
	if config.AppKeySecretRef == "" || config.AppSecretRef == "" {
		return nil, errors.New("DingTalk app key and secret references are required")
	}
	limiter := config.Limiter
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Limit(5), 1)
	}
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Second
	}
	if config.MaxAttempts == 0 {
		config.MaxAttempts = 3
	}
	if config.MaxAttempts < 1 {
		return nil, errors.New("DingTalk max attempts must be positive")
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = 100 * time.Millisecond
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Client{
		baseURL: baseURL, legacyBaseURL: legacyBaseURL, httpClient: safeHTTPClient(config.HTTPClient),
		secrets: config.Secrets, store: config.Store, config: config, limiter: limiter,
	}, nil
}

func NewDingTalkClient(config Config) (*Client, error) { return NewClient(config) }

func (c *Client) ExchangeLoginCode(ctx context.Context, code string) (UserInfo, error) {
	if code == "" {
		return UserInfo{}, fmt.Errorf("%w: login code is required", domain.ErrInvalidRequest)
	}
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return UserInfo{}, err
	}
	var response struct {
		UserInfo
		Result *UserInfo `json:"result"`
	}
	err = c.post(ctx, c.legacyBaseURL, "/topapi/v2/user/getuserinfo", token, map[string]string{"code": code}, &response)
	if err != nil && tokenInvalid(err) {
		token, tokenErr := c.accessToken(ctx, true)
		if tokenErr != nil {
			return UserInfo{}, tokenErr
		}
		response.Result = nil
		err = c.post(ctx, c.legacyBaseURL, "/topapi/v2/user/getuserinfo", token, map[string]string{"code": code}, &response)
	}
	if err != nil {
		return UserInfo{}, err
	}
	result := response.UserInfo
	if response.Result != nil {
		result = *response.Result
	}
	if result.UserID == "" {
		return UserInfo{}, errors.New("DingTalk login response has no userid")
	}
	return result, nil
}

func (c *Client) Notify(ctx context.Context, notification domain.Notification) error {
	if c.store == nil {
		return errors.New("DingTalk agent store is required")
	}
	agent, err := c.store.AgentByID(ctx, notification.AgentID)
	if err != nil {
		return fmt.Errorf("load DingTalk agent: %w", err)
	}
	if !agent.Active || agent.DingTalkUserID == "" {
		return fmt.Errorf("%w: agent has no active DingTalk identity", domain.ErrInvalidRequest)
	}
	if c.config.CardTemplateID == "" {
		return errors.New("DingTalk card template ID is not configured")
	}
	parameters := map[string]string{
		"title": notification.Title, "text": notification.Text, "url": notification.URL,
		"conversationId": notification.ConversationID, "kind": notification.Kind,
	}
	body := map[string]any{
		"cardTemplateId":   c.config.CardTemplateID,
		"outTrackId":       notificationTrackID(notification),
		"openSpaceId":      "dtv1.card//IM_SINGLE." + agent.DingTalkUserID,
		"cardData":         map[string]any{"cardParamMap": parameters},
		"callbackType":     "HTTP",
		"callbackRouteKey": callbackRouteKey(c.config.CallbackRouteKey),
		"userIdModel":      map[string]string{"userId": agent.DingTalkUserID},
	}
	return c.authenticatedPost(ctx, c.baseURL, "/v1.0/card/instances/createAndDeliver", body, nil, true)
}

func (c *Client) authenticatedPost(ctx context.Context, base *url.URL, path string, body, output any, retry bool) error {
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return err
	}
	err = c.post(ctx, base, path, token, body, output)
	if err != nil && tokenInvalid(err) {
		token, err = c.accessToken(ctx, true)
		if err != nil {
			return err
		}
		err = c.post(ctx, base, path, token, body, output)
	}
	if retry {
		for attempt := 1; err != nil && IsRetryable(err) && attempt < c.config.MaxAttempts; attempt++ {
			if waitErr := waitContext(ctx, c.retryDelay(attempt)); waitErr != nil {
				return waitErr
			}
			err = c.post(ctx, base, path, token, body, output)
		}
	}
	return err
}

func (c *Client) accessToken(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	if !force && c.token != "" && c.config.Now().Before(c.expiresAt) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	if c.flight != nil {
		flight := c.flight
		c.mu.Unlock()
		select {
		case <-flight.done:
			return flight.token, flight.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	flight := &tokenFlight{done: make(chan struct{})}
	c.flight = flight
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.flight = nil
		c.mu.Unlock()
		close(flight.done)
	}()
	if !force {
		c.mu.Lock()
		cached := c.token != "" && c.config.Now().Before(c.expiresAt)
		token := c.token
		c.mu.Unlock()
		if cached {
			flight.token = token
			return token, nil
		}
	}
	flight.token, flight.err = c.fetchToken(ctx)
	return flight.token, flight.err
}

func (c *Client) fetchToken(ctx context.Context) (string, error) {
	appKey, err := c.secrets.Value(ctx, c.config.AppKeySecretRef)
	if err != nil {
		return "", fmt.Errorf("resolve DingTalk app key: %w", err)
	}
	appSecret, err := c.secrets.Value(ctx, c.config.AppSecretRef)
	if err != nil {
		return "", fmt.Errorf("resolve DingTalk app secret: %w", err)
	}
	if appKey == "" || appSecret == "" {
		return "", errors.New("DingTalk app credentials are empty")
	}
	var result struct {
		AccessToken string      `json:"accessToken"`
		ExpiresIn   json.Number `json:"expireIn"`
	}
	body := map[string]string{"appKey": appKey, "appSecret": appSecret}
	if err = c.post(ctx, c.baseURL, "/v1.0/oauth2/accessToken", "", body, &result); err != nil {
		return "", err
	}
	if result.AccessToken == "" {
		return "", errors.New("DingTalk token response has no accessToken")
	}
	expires, err := strconv.ParseInt(result.ExpiresIn.String(), 10, 64)
	if err != nil || expires <= 0 {
		expires = 7200
	}
	ttl := time.Duration(expires)*time.Second - 5*time.Minute
	if ttl <= 0 {
		ttl = time.Duration(expires) * time.Second / 2
	}
	c.mu.Lock()
	c.token, c.expiresAt = result.AccessToken, c.config.Now().Add(ttl)
	c.mu.Unlock()
	return result.AccessToken, nil
}

type APIError struct {
	Status    int
	Code      string
	Message   string
	Retryable bool
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("DingTalk API error %s", e.Code)
	}
	return fmt.Sprintf("DingTalk API HTTP status %d", e.Status)
}

func (e *APIError) Is(target error) bool {
	return target == ErrRateLimited && (e.Status == http.StatusTooManyRequests || e.Code == "88" || e.Code == "Throttling.User" || e.Code == "Throttling.System" || e.Code == "rate.limit")
}

func (e *APIError) Retry() bool { return e.Retryable }

func IsRetryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, domain.ErrInvalidRequest) && !errors.Is(err, ErrInvalidSignature)
}

func (c *Client) post(ctx context.Context, base *url.URL, path, token string, body, output any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := *base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	query := endpoint.Query()
	if token != "" && base == c.legacyBaseURL {
		query.Set("access_token", token)
	}
	endpoint.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	if err = c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("wait for DingTalk rate limit: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" && base == c.baseURL {
		req.Header.Set("x-acs-dingtalk-access-token", token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DingTalk API request: %w", sanitizeRequestError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read DingTalk API response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{
			Status:    resp.StatusCode,
			Retryable: resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
		}
	}
	var envelope struct {
		ErrCode json.Number `json:"errcode"`
		ErrMsg  string      `json:"errmsg"`
		Code    string      `json:"code"`
		Message string      `json:"message"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return errors.New("decode DingTalk API response")
	}
	code := envelope.Code
	if envelope.ErrCode.String() != "" && envelope.ErrCode.String() != "0" {
		code = envelope.ErrCode.String()
	}
	if code != "" && code != "ok" && code != "Success" {
		return &APIError{
			Code: code, Message: firstNonempty(envelope.Message, envelope.ErrMsg),
			Retryable: code == "-1" || code == "88" || code == "Throttling.User" || code == "Throttling.System" || code == "rate.limit",
		}
	}
	if output != nil && len(data) != 0 {
		if err = json.Unmarshal(data, output); err != nil {
			return errors.New("decode DingTalk API result")
		}
	}
	return nil
}

func tokenInvalid(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Code {
	case "40001", "40014", "42001", "InvalidAuthentication", "InvalidAccessToken":
		return true
	default:
		return apiErr.Status == http.StatusUnauthorized
	}
}

func (c *Client) retryDelay(attempt int) time.Duration {
	delay := c.config.RetryDelay
	for i := 1; i < attempt && delay < time.Second; i++ {
		delay *= 2
	}
	if delay > time.Second {
		return time.Second
	}
	return delay
}

func (c *Client) VerifyCardSignature(ctx context.Context, headers http.Header) error {
	reference := c.config.CardSecretRef
	if reference == "" {
		reference = c.config.APISecretRef
	}
	if reference == "" {
		reference = c.config.AppSecretRef
	}
	secret, err := c.secrets.Value(ctx, reference)
	if err != nil {
		return fmt.Errorf("resolve DingTalk callback secret: %w", err)
	}
	return VerifyCardHeaders(headers, secret, c.config.Now())
}

func (c *Client) DecryptEvent(ctx context.Context, encrypted string) (string, error) {
	if c.config.AESKeySecretRef == "" {
		return "", errors.New("DingTalk AES key reference is not configured")
	}
	key, err := c.secrets.Value(ctx, c.config.AESKeySecretRef)
	if err != nil {
		return "", fmt.Errorf("resolve DingTalk AES key: %w", err)
	}
	if c.config.CorpIDSecretRef == "" {
		return "", errors.New("DingTalk CorpID reference is not configured")
	}
	corpID, err := c.secrets.Value(ctx, c.config.CorpIDSecretRef)
	if err != nil {
		return "", fmt.Errorf("resolve DingTalk CorpID: %w", err)
	}
	return DecryptEvent(encrypted, corpID, key)
}

func callbackRouteKey(route string) string {
	if route != "" {
		return route
	}
	return "customer-service-notification"
}

func notificationTrackID(notification domain.Notification) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		notification.AgentID, notification.ConversationID, notification.Kind,
		notification.Title, notification.Text, notification.URL,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func parseBaseURL(value, fallback string) (*url.URL, error) {
	if value == "" {
		value = fallback
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid DingTalk API base URL")
	}
	return parsed, nil
}

func safeHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client = &http.Client{Transport: transport, Timeout: 10 * time.Second}
	} else {
		clone := *client
		client = &clone
	}
	if client.CheckRedirect == nil {
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	}
	return client
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

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
