package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateTicketLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateTicketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateTicketLogic {
	return &UpdateTicketLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UpdateTicketLogic) UpdateTicket(req *types.TicketUpdateRequest) (*types.IdResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	ticket, err := l.svcCtx.Store.Ticket(l.ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if ticket.AssigneeID != "" && ticket.AssigneeID != agent.ID && agent.Role == domain.RoleAgent {
		return nil, domain.ErrForbidden
	}
	if req.Status != "" {
		switch req.Status {
		case "open", "processing", "resolved", "closed":
			ticket.Status = req.Status
		default:
			return nil, domain.ErrInvalidRequest
		}
	}
	if req.Priority != "" {
		ticket.Priority = req.Priority
	}
	if req.AssigneeId != "" {
		ticket.AssigneeID = req.AssigneeId
	}
	due, err := ParseTime(req.DueAt)
	if err != nil {
		return nil, err
	}
	if due != nil {
		ticket.DueAt = due
	}
	if err = l.svcCtx.Store.UpdateTicket(l.ctx, ticket, agent.ID); err != nil {
		return nil, err
	}
	return &types.IdResponse{Id: ticket.ID}, nil
}
