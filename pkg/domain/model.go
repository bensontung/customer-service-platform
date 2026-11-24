package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	ChannelService = "wechat_service"
	ChannelMiniApp = "wechat_miniapp"
	SourcePrivate  = "private_message"
	SourceComment  = "article_comment"
	StatusWaiting  = "waiting"
	StatusOpen     = "open"
	StatusClosed   = "closed"
	StatusQueued   = "queued"
	StatusRetrying = "retrying"
	StatusSent     = "sent"
	StatusFailed   = "failed_final"
	RoleAgent      = "agent"
	RoleSupervisor = "supervisor"
	RoleAdmin      = "admin"
)

var (
	ErrNotFound       = errors.New("resource not found")
	ErrDuplicate      = errors.New("duplicate event")
	ErrConflict       = errors.New("concurrent update")
	ErrForbidden      = errors.New("forbidden")
	ErrUnauthorized   = errors.New("unauthorized")
	ErrReplyExpired   = errors.New("wechat customer service reply window expired")
	ErrReplyQuota     = errors.New("wechat customer service reply quota exhausted")
	ErrNotAssigned    = errors.New("conversation must be claimed by the agent")
	ErrInvalidRequest = errors.New("invalid request")
)

type ChannelAccount struct {
	ID, AccountKey, Channel, AppID  string
	TokenSecretRef, AESKeySecretRef string
	AppSecretRef, Status            string
}

type ObjectStorage interface {
	PresignPut(context.Context, string, string, int64, time.Duration) (ObjectUpload, error)
}

type ObjectUpload struct {
	ObjectKey string
	UploadURL string
	Headers   map[string]string
	ExpiresAt time.Time
}

type Customer struct {
	ID, Channel, AppID, OpenID, UnionID string
	Nickname, Note                      string
	Tags                                []string
}

type Conversation struct {
	ID, Source, ChannelAccountID, CustomerID string
	Status, AssigneeID, TeamID               string
	Version                                  int64
	LastInboundAt, ReplyDeadlineAt           time.Time
	RepliesInWindow                          int
	FirstResponseAt                          *time.Time
	SLAEscalatedAt                           *time.Time
	CreatedAt, UpdatedAt                     time.Time
}

type Message struct {
	ID, ConversationID, ChannelAccountID string
	Channel, Direction, ChannelMessageID string
	Type                                 string
	Content                              json.RawMessage
	Status, ErrorCode, ErrorMessage      string
	RetryCount                           int
	NextRetryAt                          *time.Time
	CreatedAt, UpdatedAt                 time.Time
}

type Inbound struct {
	AccountKey, EventID, PayloadHash string
	Channel, AppID, OpenID           string
	Type, Content                    string
	MediaID                          string
	ReceivedAt                       time.Time
}

type Agent struct {
	ID, DingTalkUserID, Name, Role string
	Active                         bool
}

type Shift struct {
	ID, AgentID, TeamID string
	StartAt, EndAt      time.Time
	State               string
}

type Team struct{ ID, Name string }

type RoutingRule struct {
	ID, TeamID  string
	Priority    int
	Channel     string
	AccountID   string
	Tag         string
	StartMinute int
	EndMinute   int
	Active      bool
}

type Comment struct {
	ID, ChannelAccountID, MsgDataID string
	ArticleIndex                    int
	CommentID, AuthorID, Content    string
	Status, ReplyContent            string
	SyncedAt, UpdatedAt             time.Time
}

type Ticket struct {
	ID, CustomerID, ConversationID, CommentID string
	Title, Description, Status, Priority      string
	AssigneeID                                string
	DueAt                                     *time.Time
	Version                                   int64
	CreatedAt, UpdatedAt                      time.Time
}

type TicketEvent struct {
	ID, TicketID, Type, ActorID string
	Payload                     json.RawMessage
	CreatedAt                   time.Time
}

type AuditLog struct {
	ID, ActorID, Action, ResourceType, ResourceID string
	Detail                                        json.RawMessage
	CreatedAt                                     time.Time
}

type OutboxEvent struct {
	ID, Type, AggregateID string
	Payload               json.RawMessage
	Attempts              int
}

type Notification struct {
	AgentID, ConversationID, Kind, Title, Text, URL string
}

func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id.String()
}

func NewReplyWindow(inbound time.Time) time.Time { return inbound.UTC().Add(48 * time.Hour) }

func CanReply(c Conversation, agent Agent, now time.Time) error {
	if c.Status == StatusClosed {
		return domainError("conversation is closed")
	}
	if !CanAccess(c, agent) || c.AssigneeID == "" {
		return ErrNotAssigned
	}
	if !now.UTC().Before(c.ReplyDeadlineAt.UTC()) {
		return ErrReplyExpired
	}
	if c.RepliesInWindow >= 5 {
		return ErrReplyQuota
	}
	return nil
}

func CanAccess(c Conversation, agent Agent) bool {
	return agent.Active && (agent.Role == RoleSupervisor || agent.Role == RoleAdmin || c.AssigneeID == "" || c.AssigneeID == agent.ID)
}

func domainError(message string) error { return errors.Join(ErrInvalidRequest, errors.New(message)) }

type Secrets interface {
	Value(ctx context.Context, reference string) (string, error)
}
