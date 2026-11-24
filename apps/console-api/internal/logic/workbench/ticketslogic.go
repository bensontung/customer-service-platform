package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/zeromicro/go-zero/core/logx"
)

type TicketsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTicketsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TicketsLogic {
	return &TicketsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *TicketsLogic) Tickets() (*types.TicketListResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	tickets, err := l.svcCtx.Store.Tickets(l.ctx)
	if err != nil {
		return nil, err
	}
	resp := &types.TicketListResponse{Items: []types.TicketView{}}
	for _, ticket := range tickets {
		if agent.Role != domain.RoleAgent || ticket.AssigneeID == "" || ticket.AssigneeID == agent.ID {
			resp.Items = append(resp.Items, TicketView(ticket))
		}
	}
	return resp, nil
}
