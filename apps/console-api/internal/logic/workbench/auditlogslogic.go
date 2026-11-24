package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type AuditLogsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAuditLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AuditLogsLogic {
	return &AuditLogsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *AuditLogsLogic) AuditLogs() (*types.AuditListResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	if err = RequireSupervisor(agent); err != nil {
		return nil, err
	}
	logs, err := l.svcCtx.Store.Audits(l.ctx)
	if err != nil {
		return nil, err
	}
	resp := &types.AuditListResponse{Items: []types.AuditView{}}
	for _, log := range logs {
		resp.Items = append(resp.Items, types.AuditView{Id: log.ID, ActorId: log.ActorID, Action: log.Action, ResourceType: log.ResourceType, ResourceId: log.ResourceID, Detail: string(log.Detail), CreatedAt: formatTime(log.CreatedAt)})
	}
	return resp, nil
}
