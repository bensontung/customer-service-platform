package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/customer-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/customer-rpc/pb/github.com/example/customer-service-platform/apps/customer-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetCustomerLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetCustomerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCustomerLogic {
	return &GetCustomerLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetCustomerLogic) GetCustomer(in *pb.GetCustomerRequest) (*pb.Customer, error) {
	if in == nil || in.Id == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	customer, err := l.svcCtx.Store.Customer(l.ctx, in.Id)
	if err != nil {
		return nil, rpcError(err)
	}
	return customerView(customer), nil
}
