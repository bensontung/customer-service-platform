package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/conversation-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/conversation-rpc/pb/github.com/example/customer-service-platform/apps/conversation-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type TransferConversationLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewTransferConversationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TransferConversationLogic {
	return &TransferConversationLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *TransferConversationLogic) TransferConversation(in *pb.TransferConversationRequest) (*pb.Conversation, error) {
	if in == nil || in.ConversationId == "" || in.AssigneeId == "" && in.TeamId == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	currentAgent, err := claimedAgent(l.ctx, l.svcCtx, in.ActorId)
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
	conversation, err = l.svcCtx.Store.TransferConversation(l.ctx, in.ConversationId, in.AssigneeId, in.TeamId, currentAgent)
	if err != nil {
		return nil, rpcError(err)
	}
	return conversationView(conversation), nil
}
