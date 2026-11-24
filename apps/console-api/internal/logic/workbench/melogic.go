package workbench

import (
	"context"
	"time"

	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type MeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MeLogic {
	return &MeLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *MeLogic) Me() (*types.MeResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	shifts, err := l.svcCtx.Store.Shifts(l.ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	onShift := false
	for _, shift := range shifts {
		if shift.State == "active" && (shift.AgentID == agent.ID || shift.TeamID != "") && now.After(shift.StartAt) && now.Before(shift.EndAt) {
			onShift = true
		}
	}
	return &types.MeResponse{AgentId: agent.ID, Role: agent.Role, Name: agent.Name, OnShift: onShift}, nil
}
