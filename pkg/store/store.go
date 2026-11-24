package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
)

type ConversationFilter struct{ Status, Source, Assignee string }
type Report struct {
	NewConversations, ClosedConversations, SLAExpired int64
	AverageFirstResponseSeconds                       float64
}

type Store interface {
	AccountByKey(context.Context, string) (domain.ChannelAccount, error)
	AccountByID(context.Context, string) (domain.ChannelAccount, error)
	SaveAccount(context.Context, domain.ChannelAccount) error
	ReceiveInbound(context.Context, domain.Inbound, domain.ChannelAccount) (bool, error)
	AgentByID(context.Context, string) (domain.Agent, error)
	AgentByDingTalkID(context.Context, string) (domain.Agent, error)
	SaveAgent(context.Context, domain.Agent) error
	Conversation(context.Context, string) (domain.Conversation, error)
	Conversations(context.Context, ConversationFilter) ([]domain.Conversation, error)
	Messages(context.Context, string) ([]domain.Message, error)
	Customer(context.Context, string) (domain.Customer, error)
	UpdateCustomer(context.Context, domain.Customer) error
	ClaimConversation(context.Context, string, domain.Agent) (domain.Conversation, error)
	TransferConversation(context.Context, string, string, string, domain.Agent) (domain.Conversation, error)
	AssignConversation(context.Context, string, string, string) (domain.Conversation, error)
	CloseConversation(context.Context, string, domain.Agent) (domain.Conversation, error)
	QueueMessage(context.Context, string, domain.Agent, string, string, string, ...string) (domain.Message, error)
	MessageByExternalID(context.Context, string, string) (domain.Message, error)
	UpdateMessage(context.Context, domain.Message) error
	Message(context.Context, string) (domain.Message, error)
	ClaimMessage(context.Context, string, time.Time) (domain.Message, bool, error)
	RetryableMessages(context.Context, time.Time) ([]domain.Message, error)
	ClaimOutbox(context.Context, time.Time) ([]domain.OutboxEvent, error)
	MarkOutboxPublished(context.Context, string) error
	ReleaseOutbox(context.Context, string, time.Time) error
	ReserveNotification(context.Context, string, string, string, time.Time) (string, bool, error)
	CompleteNotification(context.Context, string, string, string, string) error
	Comment(context.Context, string) (domain.Comment, error)
	Comments(context.Context, string, string) ([]domain.Comment, error)
	UpsertComment(context.Context, domain.Comment) error
	UpdateComment(context.Context, domain.Comment) error
	CreateTicket(context.Context, domain.Ticket, string) error
	UpdateTicket(context.Context, domain.Ticket, string) error
	Ticket(context.Context, string) (domain.Ticket, error)
	Tickets(context.Context) ([]domain.Ticket, error)
	TicketsByConversation(context.Context, string) ([]domain.Ticket, error)
	TicketEvents(context.Context, string) ([]domain.TicketEvent, error)
	SaveShift(context.Context, domain.Shift) error
	Shifts(context.Context) ([]domain.Shift, error)
	SaveRoutingRule(context.Context, domain.RoutingRule) error
	RoutingRules(context.Context) ([]domain.RoutingRule, error)
	TeamAgents(context.Context, string) ([]domain.Agent, error)
	SaveTeamAgent(context.Context, string, string) error
	Audit(context.Context, domain.AuditLog) error
	Audits(context.Context) ([]domain.AuditLog, error)
	RecordSLA(context.Context, string, string, string) (bool, error)
	ExpiringConversations(context.Context, time.Time, time.Time) ([]domain.Conversation, error)
	ExpiredConversations(context.Context, time.Time) ([]domain.Conversation, error)
	ExpiredTickets(context.Context, time.Time) ([]domain.Ticket, error)
	Report(context.Context) (Report, error)
}

func JSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
