package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

type MediaObjectStore interface {
	ReadObject(context.Context, string) (io.ReadCloser, error)
}
type MediaSender struct {
	Client  *Client
	Storage MediaObjectStore
}

func (s MediaSender) SendMessage(ctx context.Context, account, openID, kind, text, mediaKey string) error {
	if kind == "text" {
		return s.Client.SendMessage(ctx, account, openID, kind, text, "")
	}
	mediaID, err := s.upload(ctx, account, kind, mediaKey)
	if err != nil {
		return err
	}
	return s.Client.SendMessage(ctx, account, openID, kind, "", mediaID)
}
func (s MediaSender) upload(ctx context.Context, account, kind, objectKey string) (string, error) {
	if s.Storage == nil || (kind != "image" && kind != "voice") || !strings.HasPrefix(objectKey, "agent-media/") {
		return "", domainErrMedia()
	}
	object, err := s.Storage.ReadObject(ctx, objectKey)
	if err != nil {
		return "", fmt.Errorf("read media object: %w", err)
	}
	defer func() { _ = object.Close() }()
	data, err := io.ReadAll(io.LimitReader(object, 20<<20+1))
	if err != nil || len(data) > 20<<20 || len(data) == 0 {
		return "", fmt.Errorf("invalid media object")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("media", "media.bin")
	if err != nil {
		return "", err
	}
	if _, err = part.Write(data); err != nil {
		return "", err
	}
	_ = writer.Close()
	return s.postMedia(ctx, account, kind, &body, writer.FormDataContentType())
}
func (s MediaSender) postMedia(ctx context.Context, account, kind string, body *bytes.Buffer, contentType string) (string, error) {
	acc, err := s.Client.account(ctx, account)
	if err != nil {
		return "", err
	}
	token, err := s.Client.accessToken(ctx, acc, false)
	if err != nil {
		return "", err
	}
	endpoint := s.Client.baseURL.ResolveReference(&url.URL{Path: "/cgi-bin/media/upload", RawQuery: url.Values{"access_token": {token}, "type": {kind}}.Encode()})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := s.Client.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload WeChat media: %w", sanitizeRequestError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	var result struct {
		MediaID string `json:"media_id"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 {
		return "", wechatError(result.ErrCode, result.ErrMsg)
	}
	if result.MediaID == "" {
		return "", fmt.Errorf("WeChat media ID missing")
	}
	return result.MediaID, nil
}
func domainErrMedia() error { return fmt.Errorf("invalid media object or message type") }
