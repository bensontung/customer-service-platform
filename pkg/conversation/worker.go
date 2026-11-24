package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/customer-service-platform/pkg/dingtalk"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/example/customer-service-platform/pkg/wechat"
)

type Notifier interface {
	Notify(context.Context, domain.Notification) error
}
type Sender interface {
	SendMessage(context.Context, string, string, string, string, string) error
}

type Worker struct {
	Store      store.Store
	Notifier   Notifier
	Sender     Sender
	ConsoleURL string
	Now        func() time.Time
}

func (w *Worker) ProcessOutbox(ctx context.Context) error {
	events, err := w.Store.ClaimOutbox(ctx, w.now())
	if err != nil {
		return err
	}
	for _, event := range events {
		if err = w.Process(ctx, event); err != nil {
			if releaseErr := w.Store.ReleaseOutbox(ctx, event.ID, w.now().Add(time.Second*time.Duration(min(60, 1<<min(eventAttempts(event), 6))))); releaseErr != nil {
				return releaseErr
			}
			continue
		}
		if err = w.Store.MarkOutboxPublished(ctx, event.ID); err != nil {
			return err
		}
	}
	return nil
}

func eventAttempts(event domain.OutboxEvent) int { return event.Attempts + 1 }
func (w *Worker) Process(ctx context.Context, event domain.OutboxEvent) error {
	switch event.Type {
	case "MessageReceived":
		if err := w.route(ctx, event.AggregateID); err != nil {
			return err
		}
		return w.notifyConversation(ctx, event.AggregateID, "new_conversation", "新微信客服会话", "有新的客户消息等待处理")
	case "MessageSendRequested":
		var payload struct {
			MessageID string `json:"messageId"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return w.Send(ctx, payload.MessageID)
	case "ConversationTransferred":
		var payload struct {
			AgentID string `json:"agentId"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return w.notifyAgent(ctx, payload.AgentID, event.AggregateID, "transfer", "会话已转派", "请处理转派给你的客服会话")
	default:
		return fmt.Errorf("unknown outbox event type %q", event.Type)
	}
}

func (w *Worker) route(ctx context.Context, id string) error {
	conversation, err := w.Store.Conversation(ctx, id)
	if err != nil || conversation.AssigneeID != "" {
		return err
	}
	rules, err := w.Store.RoutingRules(ctx)
	if err != nil || len(rules) == 0 {
		return err
	}
	shifts, err := w.Store.Shifts(ctx)
	if err != nil {
		return err
	}
	customer, err := w.Store.Customer(ctx, conversation.CustomerID)
	if err != nil {
		return err
	}
	account, err := w.Store.AccountByID(ctx, conversation.ChannelAccountID)
	if err != nil {
		return err
	}
	conversations, err := w.Store.Conversations(ctx, store.ConversationFilter{Status: domain.StatusOpen})
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, item := range conversations {
		counts[item.AssigneeID]++
	}
	for _, rule := range rules {
		if !rule.Active {
			continue
		}
		agents, agentErr := w.Store.TeamAgents(ctx, rule.TeamID)
		if agentErr != nil {
			return agentErr
		}
		assignment, ok := domain.Route(domain.RouteInput{Conversation: conversation, Customer: customer, Account: account, Rules: []domain.RoutingRule{rule}, Agents: agents, Shifts: shifts, ActiveCounts: counts, Now: w.now()})
		if ok {
			_, err = w.Store.AssignConversation(ctx, id, assignment.TeamID, assignment.AgentID)
			return err
		}
	}
	return nil
}

func (w *Worker) notifyConversation(ctx context.Context, conversationID, kind, title, text string) error {
	conversation, err := w.Store.Conversation(ctx, conversationID)
	if err != nil {
		return err
	}
	agentID := conversation.AssigneeID
	if agentID == "" {
		teamID := conversation.TeamID
		if teamID == "" {
			rules, ruleErr := w.Store.RoutingRules(ctx)
			if ruleErr != nil {
				return ruleErr
			}
			if len(rules) > 0 {
				teamID = rules[0].TeamID
			}
		}
		agents, err := w.Store.TeamAgents(ctx, teamID)
		if err != nil {
			return err
		}
		for _, agent := range agents {
			if agent.Role == domain.RoleSupervisor || agent.Role == domain.RoleAdmin {
				agentID = agent.ID
				break
			}
		}
		if agentID == "" && len(agents) > 0 {
			agentID = agents[0].ID
		}
	}
	if agentID == "" {
		return nil
	}
	return w.notifyAgent(ctx, agentID, conversationID, kind, title, text)
}
func (w *Worker) notifyAgent(ctx context.Context, agentID, conversationID, kind, title, text string) error {
	if w.Notifier == nil || agentID == "" {
		return nil
	}
	owner, first, err := w.Store.ReserveNotification(ctx, conversationID, agentID, kind, w.now())
	if err != nil || !first {
		return err
	}
	if err = w.Notifier.Notify(ctx, domain.Notification{AgentID: agentID, ConversationID: conversationID, Kind: kind, Title: title, Text: text, URL: strings.TrimRight(w.ConsoleURL, "/") + "/dingtalk/conversations/" + conversationID}); err != nil {
		return err
	}
	return w.Store.CompleteNotification(ctx, conversationID, agentID, kind, owner)
}

func (w *Worker) Send(ctx context.Context, id string) error {
	message, claimed, err := w.Store.ClaimMessage(ctx, id, w.now())
	if err != nil || !claimed {
		return err
	}
	conversation, err := w.Store.Conversation(ctx, message.ConversationID)
	if err != nil {
		return err
	}
	account, err := w.Store.AccountByID(ctx, message.ChannelAccountID)
	if err != nil {
		return err
	}
	customer, err := w.Store.Customer(ctx, conversation.CustomerID)
	if err != nil {
		return err
	}
	var content struct {
		Text    string `json:"text"`
		MediaID string `json:"mediaId"`
	}
	if err = json.Unmarshal(message.Content, &content); err != nil {
		return err
	}
	err = w.Sender.SendMessage(ctx, account.AccountKey, customer.OpenID, message.Type, content.Text, content.MediaID)
	if err == nil {
		message.Status = domain.StatusSent
		message.ErrorCode = ""
		message.ErrorMessage = ""
		message.NextRetryAt = nil
		return w.Store.UpdateMessage(ctx, message)
	}
	var apiError *wechat.APIError
	if errors.As(err, &apiError) {
		message.ErrorCode = fmt.Sprint(apiError.Code)
		message.ErrorMessage = apiError.Message
	} else {
		message.ErrorMessage = "WeChat API temporarily unavailable"
	}
	if !wechat.IsRetryable(err) || message.RetryCount >= 5 || w.now().After(conversation.ReplyDeadlineAt) {
		message.Status = domain.StatusFailed
		message.NextRetryAt = nil
		return w.Store.UpdateMessage(ctx, message)
	}
	message.RetryCount++
	message.Status = domain.StatusRetrying
	next := w.now().Add(time.Second * time.Duration(1<<min(message.RetryCount, 6)))
	message.NextRetryAt = &next
	return w.Store.UpdateMessage(ctx, message)
}

func (w *Worker) Retry(ctx context.Context) error {
	messages, err := w.Store.RetryableMessages(ctx, w.now())
	if err != nil {
		return err
	}
	for _, message := range messages {
		if err = w.Send(ctx, message.ID); err != nil {
			return err
		}
	}
	return nil
}
func (w *Worker) SLA(ctx context.Context) error {
	expiring, err := w.Store.ExpiringConversations(ctx, w.now(), w.now().Add(15*time.Minute))
	if err != nil {
		return err
	}
	for _, conversation := range expiring {
		first, alertErr := w.Store.RecordSLA(ctx, "conversation", conversation.ID, "warning")
		if alertErr != nil {
			return alertErr
		}
		if first {
			if alertErr = w.notifyConversation(ctx, conversation.ID, "sla_warning", "会话即将超时", "请在回复窗口到期前处理"); alertErr != nil {
				return alertErr
			}
		}
	}
	conversations, err := w.Store.ExpiredConversations(ctx, w.now())
	if err != nil {
		return err
	}
	for _, conversation := range conversations {
		first, err := w.Store.RecordSLA(ctx, "conversation", conversation.ID, "expired")
		if err != nil {
			return err
		}
		if first {
			if err = w.notifyConversation(ctx, conversation.ID, "sla_expired", "会话已超时", "请立即检查会话与微信回复窗口"); err != nil {
				return err
			}
		}
	}
	tickets, err := w.Store.ExpiredTickets(ctx, w.now())
	if err != nil {
		return err
	}
	for _, ticket := range tickets {
		first, ticketErr := w.Store.RecordSLA(ctx, "ticket", ticket.ID, "expired")
		if ticketErr != nil {
			return ticketErr
		}
		if first && ticket.ConversationID != "" {
			if ticketErr = w.notifyConversation(ctx, ticket.ConversationID, "ticket_expired", "工单已超时", "工单 "+ticket.ID+" 已超过处理截止时间"); ticketErr != nil {
				return ticketErr
			}
		}
	}
	return nil
}
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_ = w.ProcessOutbox(ctx)
		_ = w.Retry(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

var _ = dingtalk.ErrRateLimited
