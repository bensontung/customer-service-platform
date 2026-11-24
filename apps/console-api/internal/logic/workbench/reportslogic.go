package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ReportsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewReportsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReportsLogic {
	return &ReportsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ReportsLogic) Reports() (*types.ReportResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	if err = RequireSupervisor(agent); err != nil {
		return nil, err
	}
	report, err := l.svcCtx.Store.Report(l.ctx)
	if err != nil {
		return nil, err
	}
	return &types.ReportResponse{NewConversations: report.NewConversations, ClosedConversations: report.ClosedConversations, SLAExpired: report.SLAExpired, AverageFirstResponseSeconds: report.AverageFirstResponseSeconds}, nil
}
