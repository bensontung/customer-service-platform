package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/zeromicro/go-zero/core/logx"
)

type TicketEventsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTicketEventsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TicketEventsLogic {
	return &TicketEventsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *TicketEventsLogic) TicketEvents(req *types.IdRequest) (*types.TicketEventListResponse, error) {
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
	events, err := l.svcCtx.Store.TicketEvents(l.ctx, req.Id)
	if err != nil {
		return nil, err
	}
	resp := &types.TicketEventListResponse{Items: []types.TicketEventView{}}
	for _, event := range events {
		resp.Items = append(resp.Items, types.TicketEventView{Id: event.ID, Type: event.Type, ActorId: event.ActorID, Payload: string(event.Payload), CreatedAt: formatTime(event.CreatedAt)})
	}
	return resp, nil
}
