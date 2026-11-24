package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type CloseConversationLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCloseConversationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CloseConversationLogic {
	return &CloseConversationLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *CloseConversationLogic) CloseConversation(req *types.IdRequest) (*types.ConversationView, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	conversation, err := l.svcCtx.Store.CloseConversation(l.ctx, req.Id, agent)
	if err != nil {
		return nil, err
	}
	view := ConversationView(conversation)
	return &view, nil
}
