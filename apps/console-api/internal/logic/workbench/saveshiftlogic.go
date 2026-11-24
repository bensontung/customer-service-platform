package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/zeromicro/go-zero/core/logx"
	"time"
)

type SaveShiftLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSaveShiftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SaveShiftLogic {
	return &SaveShiftLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *SaveShiftLogic) SaveShift(req *types.ShiftRequest) (*types.IdResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	if err = RequireAdmin(agent); err != nil {
		return nil, err
	}
	start, err := ParseTime(req.StartAt)
	if err != nil {
		return nil, err
	}
	end, err := ParseTime(req.EndAt)
	if err != nil || start == nil || end == nil || !end.After(*start) {
		return nil, domain.ErrInvalidRequest
	}
	state := req.State
	if state == "" {
		state = "active"
	}
	shift := domain.Shift{ID: domain.NewID(), AgentID: req.AgentId, TeamID: req.TeamId, StartAt: *start, EndAt: *end, State: state}
	if err = l.svcCtx.Store.SaveShift(l.ctx, shift); err != nil {
		return nil, err
	}
	return &types.IdResponse{Id: shift.ID}, nil
}

var _ = time.UTC
