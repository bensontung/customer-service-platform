package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/ticket-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/ticket-rpc/pb/github.com/example/customer-service-platform/apps/ticket-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateTicketLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateTicketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateTicketLogic {
	return &CreateTicketLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateTicketLogic) CreateTicket(in *pb.CreateTicketRequest) (*pb.Ticket, error) {
	if in == nil || strings.TrimSpace(in.Title) == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	currentAgent, err := currentAgent(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	priority := in.Priority
	if priority == "" {
		priority = "normal"
	}
	if !validPriority(priority) {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	if in.AssigneeId != "" {
		if err = validateAssignee(l.ctx, l.svcCtx, in.AssigneeId); err != nil {
			return nil, err
		}
	}
	var due *time.Time
	if in.DueAt != nil {
		due, err = parseTimestamp(in.DueAt)
		if err != nil {
			return nil, err
		}
	}
	customerID := in.CustomerId
	if in.ConversationId != "" {
		conversation, loadErr := l.svcCtx.Store.Conversation(l.ctx, in.ConversationId)
		if loadErr != nil {
			return nil, rpcError(loadErr)
		}
		if !domain.CanAccess(conversation, currentAgent) {
			return nil, rpcError(domain.ErrForbidden)
		}
		customerID = conversation.CustomerID
	}
	if customerID == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	if _, err = l.svcCtx.Store.Customer(l.ctx, customerID); err != nil {
		return nil, rpcError(err)
	}
	if in.CommentId != "" {
		if _, err = l.svcCtx.Store.Comment(l.ctx, in.CommentId); err != nil {
			return nil, rpcError(err)
		}
	}
	now := time.Now().UTC()
	ticket := domain.Ticket{
		ID: domain.NewID(), CustomerID: customerID, ConversationID: in.ConversationId, CommentID: in.CommentId,
		Title: in.Title, Description: in.Description, Status: "open", Priority: priority,
		AssigneeID: in.AssigneeId, DueAt: due, CreatedAt: now, UpdatedAt: now,
	}
	if err = l.svcCtx.Store.CreateTicket(l.ctx, ticket, currentAgent.ID); err != nil {
		return nil, rpcError(err)
	}
	return ticketView(ticket), nil
}
