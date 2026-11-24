package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ShiftsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewShiftsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ShiftsLogic {
	return &ShiftsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ShiftsLogic) Shifts() (*types.ShiftListResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	if err = RequireAdmin(agent); err != nil {
		return nil, err
	}
	shifts, err := l.svcCtx.Store.Shifts(l.ctx)
	if err != nil {
		return nil, err
	}
	resp := &types.ShiftListResponse{Items: []types.ShiftView{}}
	for _, shift := range shifts {
		resp.Items = append(resp.Items, types.ShiftView{Id: shift.ID, AgentId: shift.AgentID, TeamId: shift.TeamID, StartAt: formatTime(shift.StartAt), EndAt: formatTime(shift.EndAt), State: shift.State})
	}
	return resp, nil
}
