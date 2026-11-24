package workbench

import (
	"context"

	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/zeromicro/go-zero/core/logx"
)

type ConversationLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewConversationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ConversationLogic {
	return &ConversationLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ConversationLogic) Conversation(req *types.IdRequest) (*types.ConversationDetail, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	conversation, err := l.svcCtx.Store.Conversation(l.ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if !domain.CanAccess(conversation, agent) || (conversation.AssigneeID == "" && agent.Role == domain.RoleAgent) {
		return nil, domain.ErrForbidden
	}
	customer, err := l.svcCtx.Store.Customer(l.ctx, conversation.CustomerID)
	if err != nil {
		return nil, err
	}
	messages, err := l.svcCtx.Store.Messages(l.ctx, conversation.ID)
	if err != nil {
		return nil, err
	}
	tickets, err := l.svcCtx.Store.TicketsByConversation(l.ctx, conversation.ID)
	if err != nil {
		return nil, err
	}
	view := ConversationView(conversation)
	account, err := l.svcCtx.Store.AccountByID(l.ctx, conversation.ChannelAccountID)
	if err != nil {
		return nil, err
	}
	view.Channel = account.Channel
	resp := &types.ConversationDetail{Conversation: view, Customer: CustomerView(customer), Messages: []types.MessageView{}, Tickets: []types.TicketView{}}
	for _, message := range messages {
		resp.Messages = append(resp.Messages, MessageView(message))
	}
	for _, ticket := range tickets {
		resp.Tickets = append(resp.Tickets, TicketView(ticket))
	}
	return resp, nil
}
