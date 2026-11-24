package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/ticket-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/ticket-rpc/pb/github.com/example/customer-service-platform/apps/ticket-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTicketLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTicketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTicketLogic {
	return &GetTicketLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetTicketLogic) GetTicket(in *pb.GetTicketRequest) (*pb.Ticket, error) {
	if in == nil || in.Id == "" {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	ticket, err := l.svcCtx.Store.Ticket(l.ctx, in.Id)
	if err != nil {
		return nil, rpcError(err)
	}
	return ticketView(ticket), nil
}
