package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/zeromicro/go-zero/core/logx"
	"strings"
	"time"
)

type CreateTicketLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateTicketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateTicketLogic {
	return &CreateTicketLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *CreateTicketLogic) CreateTicket(req *types.TicketCreateRequest) (*types.IdResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Title) == "" {
		return nil, domain.ErrInvalidRequest
	}
	if req.ConversationId != "" {
		c, err := l.svcCtx.Store.Conversation(l.ctx, req.ConversationId)
		if err != nil {
			return nil, err
		}
		if !domain.CanAccess(c, agent) {
			return nil, domain.ErrForbidden
		}
		req.CustomerId = c.CustomerID
	}
	if req.CommentId != "" {
		if _, err = l.svcCtx.Store.Comment(l.ctx, req.CommentId); err != nil {
			return nil, err
		}
	}
	due, err := ParseTime(req.DueAt)
	if err != nil {
		return nil, err
	}
	priority := req.Priority
	if priority == "" {
		priority = "normal"
	}
	ticket := domain.Ticket{ID: domain.NewID(), CustomerID: req.CustomerId, ConversationID: req.ConversationId, CommentID: req.CommentId, Title: req.Title, Description: req.Description, Status: "open", Priority: priority, AssigneeID: req.AssigneeId, DueAt: due, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err = l.svcCtx.Store.CreateTicket(l.ctx, ticket, agent.ID); err != nil {
		return nil, err
	}
	return &types.IdResponse{Id: ticket.ID}, nil
}
