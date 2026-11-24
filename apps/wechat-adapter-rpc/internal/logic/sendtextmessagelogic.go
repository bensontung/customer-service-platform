package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/wechat-adapter-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/wechat-adapter-rpc/pb/github.com/example/customer-service-platform/apps/wechat-adapter-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SendTextMessageLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSendTextMessageLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendTextMessageLogic {
	return &SendTextMessageLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *SendTextMessageLogic) SendTextMessage(in *pb.SendTextMessageRequest) (*pb.SendTextMessageResponse, error) {
	if in == nil || in.AccountKey == "" || in.OpenId == "" || in.Text == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	if l.svcCtx.WeChat == nil {
		return nil, status.Error(codes.Unavailable, "upstream service unavailable")
	}
	if err := l.svcCtx.WeChat.SendMessage(l.ctx, in.AccountKey, in.OpenId, "text", in.Text, ""); err != nil {
		return nil, rpcError(err)
	}
	messageID := in.IdempotencyKey
	if messageID == "" {
		messageID = domain.NewID()
	}
	return &pb.SendTextMessageResponse{MessageId: messageID}, nil
}
