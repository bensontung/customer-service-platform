package wechat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
)

type Envelope struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName" json:"ToUserName"`
	FromUserName string   `xml:"FromUserName" json:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime" json:"CreateTime"`
	MsgType      string   `xml:"MsgType" json:"MsgType"`
	Content      string   `xml:"Content" json:"Content"`
	MsgId        string   `xml:"MsgId" json:"MsgId"`
	MediaId      string   `xml:"MediaId" json:"MediaId"`
	Event        string   `xml:"Event" json:"Event"`
	EventKey     string   `xml:"EventKey" json:"EventKey"`
	Encrypt      string   `xml:"Encrypt" json:"Encrypt"`
}

func ReadBody(r io.Reader) ([]byte, error) { return io.ReadAll(io.LimitReader(r, 2<<20)) }

func Unmarshal(raw []byte) (Envelope, error) {
	var envelope Envelope
	trimmed := strings.TrimSpace(string(raw))
	var err error
	if strings.HasPrefix(trimmed, "{") {
		err = json.Unmarshal(raw, &envelope)
	} else {
		err = xml.Unmarshal(raw, &envelope)
	}
	if err != nil {
		return envelope, fmt.Errorf("decode wechat event: %w", err)
	}
	return envelope, nil
}

func (e Envelope) Inbound(accountKey, channel string, now time.Time) (domain.Inbound, error) {
	if e.FromUserName == "" || e.ToUserName == "" {
		return domain.Inbound{}, fmt.Errorf("%w: missing wechat user identity", domain.ErrInvalidRequest)
	}
	if e.MsgType == "event" {
		return domain.Inbound{}, fmt.Errorf("%w: event does not open a private-message window", domain.ErrInvalidRequest)
	}
	received := now.UTC()
	if e.CreateTime > 0 {
		received = time.Unix(e.CreateTime, 0).UTC()
	}
	if received.After(now.Add(10*time.Minute)) || now.Sub(received) > 10*time.Minute {
		received = now.UTC()
	}
	hash := sha256.Sum256([]byte(strings.Join([]string{channel, e.ToUserName, e.FromUserName, e.MsgType, e.Content, e.MediaId, fmt.Sprint(e.CreateTime)}, "\x00")))
	eventID := e.MsgId
	if eventID == "" {
		eventID = "derived:" + hex.EncodeToString(hash[:])
	}
	content, err := json.Marshal(map[string]string{"text": e.Content, "mediaId": e.MediaId})
	if err != nil {
		return domain.Inbound{}, err
	}
	return domain.Inbound{
		AccountKey: accountKey, Channel: channel, AppID: e.ToUserName, OpenID: e.FromUserName,
		EventID: eventID, PayloadHash: string(content), Type: e.MsgType, Content: e.Content,
		MediaID: e.MediaId, ReceivedAt: received,
	}, nil
}
