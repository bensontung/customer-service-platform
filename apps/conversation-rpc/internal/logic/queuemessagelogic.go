package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/conversation-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/conversation-rpc/pb/github.com/example/customer-service-platform/apps/conversation-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type QueueMessageLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewQueueMessageLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueueMessageLogic {
	return &QueueMessageLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *QueueMessageLogic) QueueMessage(in *pb.QueueMessageRequest) (*pb.QueueMessageResponse, error) {
	if in == nil || in.ConversationId == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	currentAgent, err := claimedAgent(l.ctx, l.svcCtx, in.AgentId)
	if err != nil {
		return nil, err
	}
	message, err := l.svcCtx.Store.QueueMessage(l.ctx, in.ConversationId, currentAgent, in.Type, in.Text, in.MediaId)
	if err != nil {
		return nil, rpcError(err)
	}
	if err = l.svcCtx.Store.Audit(l.ctx, domain.AuditLog{ActorID: currentAgent.ID, Action: "message.send", ResourceType: "conversation", ResourceID: message.ConversationID}); err != nil {
		return nil, rpcError(err)
	}
	return &pb.QueueMessageResponse{MessageId: message.ID}, nil
}
