package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateCustomerLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateCustomerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateCustomerLogic {
	return &UpdateCustomerLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UpdateCustomerLogic) UpdateCustomer(req *types.CustomerUpdateRequest) (*types.CustomerView, error) {
	if _, err := CurrentAgent(l.ctx, l.svcCtx.Store); err != nil {
		return nil, err
	}
	customer, err := l.svcCtx.Store.Customer(l.ctx, req.Id)
	if err != nil {
		return nil, err
	}
	customer.Tags = req.Tags
	customer.Note = req.Note
	if err = l.svcCtx.Store.UpdateCustomer(l.ctx, customer); err != nil {
		return nil, err
	}
	view := CustomerView(customer)
	return &view, nil
}
