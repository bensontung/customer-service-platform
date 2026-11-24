package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/zeromicro/go-zero/core/logx"
)

type ClaimConversationLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewClaimConversationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ClaimConversationLogic {
	return &ClaimConversationLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *ClaimConversationLogic) ClaimConversation(req *types.IdRequest) (*types.ConversationView, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	conversation, err := l.svcCtx.Store.ClaimConversation(l.ctx, req.Id, agent)
	if err != nil {
		return nil, err
	}
	if err = l.svcCtx.Store.Audit(l.ctx, storeAudit(agent.ID, conversation.ID, "conversation.claim")); err != nil {
		return nil, err
	}
	view := ConversationView(conversation)
	return &view, nil
}

var _ = store.JSON
