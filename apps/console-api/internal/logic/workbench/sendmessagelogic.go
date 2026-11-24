package workbench

import (
	"context"

	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/idempotency"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/zeromicro/go-zero/core/logx"
)

type SendMessageLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSendMessageLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendMessageLogic {
	return &SendMessageLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *SendMessageLogic) SendMessage(req *types.SendMessageRequest) (*types.IdResponse, error) {
	agent, err := CurrentAgent(l.ctx, l.svcCtx.Store)
	if err != nil {
		return nil, err
	}
	var keys []string
	if rawKey := idempotency.FromContext(l.ctx); rawKey != "" {
		conversation, conversationErr := l.svcCtx.Store.Conversation(l.ctx, req.Id)
		if conversationErr != nil {
			return nil, conversationErr
		}
		key, keyErr := idempotency.Key(agent.ID, conversation.ID, rawKey)
		if keyErr != nil {
			return nil, domain.ErrInvalidRequest
		}
		existing, lookupErr := l.svcCtx.Store.MessageByExternalID(l.ctx, conversation.ChannelAccountID, key)
		if lookupErr == nil {
			if existing.ConversationID != conversation.ID {
				return nil, domain.ErrConflict
			}
			return &types.IdResponse{Id: existing.ID}, nil
		}
		keys = []string{key}
	}
	message, err := l.svcCtx.Store.QueueMessage(l.ctx, req.Id, agent, req.Type, req.Text, req.MediaId, keys...)
	if err != nil {
		return nil, err
	}
	if err = l.svcCtx.Store.Audit(l.ctx, storeAudit(agent.ID, message.ConversationID, "message.send")); err != nil {
		return nil, err
	}
	return &types.IdResponse{Id: message.ID}, nil
}
func storeAudit(actorID, resourceID, action string) domain.AuditLog {
	return domain.AuditLog{ID: domain.NewID(), ActorID: actorID, Action: action, ResourceType: "conversation", ResourceID: resourceID, Detail: store.JSON(map[string]string{})}
}
