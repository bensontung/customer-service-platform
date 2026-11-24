package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type TransferConversationLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTransferConversationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TransferConversationLogic {
	return &TransferConversationLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *TransferConversationLogic) TransferConversation(req *types.TransferRequest) (*types.ConversationView, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	conversation, err := l.svcCtx.Store.TransferConversation(l.ctx, req.Id, req.AssigneeId, req.TeamId, agent)
	if err != nil {
		return nil, err
	}
	view := ConversationView(conversation)
	return &view, nil
}
