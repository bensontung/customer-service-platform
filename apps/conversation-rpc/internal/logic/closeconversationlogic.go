package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/conversation-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/conversation-rpc/pb/github.com/example/customer-service-platform/apps/conversation-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type CloseConversationLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCloseConversationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CloseConversationLogic {
	return &CloseConversationLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CloseConversationLogic) CloseConversation(in *pb.CloseConversationRequest) (*pb.Conversation, error) {
	if in == nil || in.ConversationId == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	currentAgent, err := claimedAgent(l.ctx, l.svcCtx, in.AgentId)
	if err != nil {
		return nil, err
	}
	conversation, err := l.svcCtx.Store.Conversation(l.ctx, in.ConversationId)
	if err != nil {
		return nil, rpcError(err)
	}
	if err = requireVersion(conversation.Version, in.ExpectedVersion); err != nil {
		return nil, err
	}
	if _, err = l.svcCtx.Store.CloseConversation(l.ctx, in.ConversationId, currentAgent); err != nil {
		return nil, rpcError(err)
	}
	conversation, err = l.svcCtx.Store.Conversation(l.ctx, in.ConversationId)
	if err != nil {
		return nil, rpcError(err)
	}
	return conversationView(conversation), nil
}
