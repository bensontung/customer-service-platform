package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/conversation-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/conversation-rpc/pb/github.com/example/customer-service-platform/apps/conversation-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetConversationLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetConversationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetConversationLogic {
	return &GetConversationLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetConversationLogic) GetConversation(in *pb.GetConversationRequest) (*pb.Conversation, error) {
	if in == nil || in.Id == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	currentAgent, err := agent(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	conversation, err := l.svcCtx.Store.Conversation(l.ctx, in.Id)
	if err != nil {
		return nil, rpcError(err)
	}
	if !domain.CanAccess(conversation, currentAgent) || conversation.AssigneeID == "" && currentAgent.Role == domain.RoleAgent {
		return nil, rpcError(domain.ErrForbidden)
	}
	return conversationView(conversation), nil
}
