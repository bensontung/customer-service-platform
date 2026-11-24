package workbench

import (
	"context"
	"github.com/example/customer-service-platform/apps/console-api/internal/svc"
	"github.com/example/customer-service-platform/apps/console-api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type CustomerLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCustomerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CustomerLogic {
	return &CustomerLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *CustomerLogic) Customer(req *types.IdRequest) (*types.CustomerView, error) {
	if _, err := CurrentAgent(l.ctx, l.svcCtx.Store); err != nil {
		return nil, err
	}
	customer, err := l.svcCtx.Store.Customer(l.ctx, req.Id)
	if err != nil {
		return nil, err
	}
	view := CustomerView(customer)
	return &view, nil
}
