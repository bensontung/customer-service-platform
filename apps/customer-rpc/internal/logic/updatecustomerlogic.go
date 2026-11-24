package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/customer-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/customer-rpc/pb/github.com/example/customer-service-platform/apps/customer-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateCustomerLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateCustomerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateCustomerLogic {
	return &UpdateCustomerLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateCustomerLogic) UpdateCustomer(in *pb.UpdateCustomerRequest) (*pb.Customer, error) {
	if in == nil || in.Id == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	if err := validateTags(in.Tags); err != nil {
		return nil, err
	}
	customer, err := l.svcCtx.Store.Customer(l.ctx, in.Id)
	if err != nil {
		return nil, rpcError(err)
	}
	customer.Tags = append([]string(nil), in.Tags...)
	customer.Note = in.Note
	if err = l.svcCtx.Store.UpdateCustomer(l.ctx, customer); err != nil {
		return nil, rpcError(err)
	}
	return customerView(customer), nil
}
