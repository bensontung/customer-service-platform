package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/zeromicro/go-zero/core/logx"
)

type TicketLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTicketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TicketLogic {
	return &TicketLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *TicketLogic) Ticket(req *types.IdRequest) (*types.TicketView, error) {
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
	view := TicketView(ticket)
	return &view, nil
}
