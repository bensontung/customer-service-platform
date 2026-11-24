package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/conversation-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/conversation-rpc/pb/github.com/example/customer-service-platform/apps/conversation-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type ClaimConversationLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewClaimConversationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ClaimConversationLogic {
	return &ClaimConversationLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ClaimConversationLogic) ClaimConversation(in *pb.ClaimConversationRequest) (*pb.Conversation, error) {
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
	conversation, err = l.svcCtx.Store.ClaimConversation(l.ctx, in.ConversationId, currentAgent)
	if err != nil {
		return nil, rpcError(err)
	}
	if err = l.svcCtx.Store.Audit(l.ctx, domain.AuditLog{ActorID: currentAgent.ID, Action: "conversation.claim", ResourceType: "conversation", ResourceID: conversation.ID}); err != nil {
		return nil, rpcError(err)
	}
	return conversationView(conversation), nil
}
