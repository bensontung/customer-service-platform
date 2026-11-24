package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/pb/github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type ExchangeLoginCodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewExchangeLoginCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ExchangeLoginCodeLogic {
	return &ExchangeLoginCodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ExchangeLoginCodeLogic) ExchangeLoginCode(in *pb.ExchangeLoginCodeRequest) (*pb.ExchangeLoginCodeResponse, error) {
	if in == nil || in.Code == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	user, err := l.svcCtx.DingTalk.ExchangeLoginCode(l.ctx, in.Code)
	if err != nil {
		return nil, rpcError(err)
	}
	return &pb.ExchangeLoginCodeResponse{DingTalkUserId: user.UserID}, nil
}
