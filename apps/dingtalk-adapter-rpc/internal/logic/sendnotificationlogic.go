package logic

import (
	"context"
	"time"

	"github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/pb/github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type SendNotificationLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSendNotificationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendNotificationLogic {
	return &SendNotificationLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *SendNotificationLogic) SendNotification(in *pb.SendNotificationRequest) (*pb.SendNotificationResponse, error) {
	if in == nil || in.AgentId == "" || in.ConversationId == "" || in.Kind == "" || in.Title == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	notification := toNotification(in)
	owner, reserved, err := l.svcCtx.Store.ReserveNotification(l.ctx, in.ConversationId, in.AgentId, in.Kind, time.Now().UTC())
	if err != nil {
		return nil, rpcError(err)
	}
	if !reserved {
		return &pb.SendNotificationResponse{NotificationId: notificationID(notification)}, nil
	}
	if err = l.svcCtx.DingTalk.Notify(l.ctx, notification); err != nil {
		return nil, rpcError(err)
	}
	if err = l.svcCtx.Store.CompleteNotification(l.ctx, in.ConversationId, in.AgentId, in.Kind, owner); err != nil {
		return nil, rpcError(err)
	}
	return &pb.SendNotificationResponse{NotificationId: notificationID(notification)}, nil
}
