package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/auth"
	"github.com/example/customer-service-platform/pkg/domain"
)

type contextKey string

const agentKey contextKey = "authenticated-agent"

func CurrentAgent(ctx context.Context, repository interface {
	AgentByID(context.Context, string) (domain.Agent, error)
}) (domain.Agent, error) {
	if agent, ok := ctx.Value(agentKey).(domain.Agent); ok {
		return agent, nil
	}
	id, err := auth.AgentID(ctx)
	if err != nil {
		return domain.Agent{}, err
	}
	agent, err := repository.AgentByID(ctx, id)
	if err != nil || !agent.Active {
		return domain.Agent{}, domain.ErrForbidden
	}
	return agent, nil
}

func RequireAdmin(agent domain.Agent) error {
	if agent.Role != domain.RoleAdmin {
		return domain.ErrForbidden
	}
	return nil
}

func RequireSupervisor(agent domain.Agent) error {
	if agent.Role != domain.RoleAdmin && agent.Role != domain.RoleSupervisor {
		return domain.ErrForbidden
	}
	return nil
}

func ConversationView(c domain.Conversation) types.ConversationView {
	return types.ConversationView{Id: c.ID, Source: c.Source, CustomerId: c.CustomerID, Status: c.Status, AssigneeId: c.AssigneeID, LastInboundAt: formatTime(c.LastInboundAt), ReplyDeadlineAt: formatTime(c.ReplyDeadlineAt), RepliesInWindow: c.RepliesInWindow}
}
func CustomerView(c domain.Customer) types.CustomerView {
	if c.Tags == nil {
		c.Tags = []string{}
	}
	return types.CustomerView{Id: c.ID, OpenId: c.OpenID, Nickname: c.Nickname, Note: c.Note, Tags: c.Tags}
}
func TicketView(t domain.Ticket) types.TicketView {
	var due string
	if t.DueAt != nil {
		due = formatTime(*t.DueAt)
	}
	return types.TicketView{Id: t.ID, CustomerId: t.CustomerID, ConversationId: t.ConversationID, CommentId: t.CommentID, Title: t.Title, Description: t.Description, Status: t.Status, Priority: t.Priority, AssigneeId: t.AssigneeID, DueAt: due}
}
func MessageView(m domain.Message) types.MessageView {
	var content struct {
		Text    string `json:"text"`
		MediaID string `json:"mediaId"`
	}
	_ = json.Unmarshal(m.Content, &content)
	text := content.Text
	if text == "" && content.MediaID != "" {
		text = "[" + m.Type + "附件]"
	}
	return types.MessageView{Id: m.ID, Direction: m.Direction, Type: m.Type, Content: text, Status: m.Status, ErrorMessage: m.ErrorMessage, CreatedAt: formatTime(m.CreatedAt)}
}
func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func ParseTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%w: time must use RFC3339", domain.ErrInvalidRequest)
	}
	utc := t.UTC()
	return &utc, nil
}

func HTTPError(err error) (int, string) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, domain.ErrUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, domain.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, domain.ErrReplyExpired), errors.Is(err, domain.ErrReplyQuota), errors.Is(err, domain.ErrNotAssigned), errors.Is(err, domain.ErrInvalidRequest):
		status = http.StatusUnprocessableEntity
	}
	return status, err.Error()
}
