package logic

import (
	"context"

	"github.com/example/customer-service-platform/apps/ticket-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/ticket-rpc/pb/github.com/example/customer-service-platform/apps/ticket-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UpdateTicketLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateTicketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateTicketLogic {
	return &UpdateTicketLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateTicketLogic) UpdateTicket(in *pb.UpdateTicketRequest) (*pb.Ticket, error) {
	if in == nil || in.Id == "" || in.ExpectedVersion < 0 || in.ClearDueAt && in.DueAt != nil {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	currentAgent, err := currentAgent(l.ctx, l.svcCtx)
	if err != nil {
		return nil, err
	}
	ticket, err := l.svcCtx.Store.Ticket(l.ctx, in.Id)
	if err != nil {
		return nil, rpcError(err)
	}
	if ticket.AssigneeID != "" && ticket.AssigneeID != currentAgent.ID && currentAgent.Role == domain.RoleAgent {
		return nil, rpcError(domain.ErrForbidden)
	}
	if in.ExpectedVersion > 0 && ticket.Version != in.ExpectedVersion {
		return nil, status.Error(codes.FailedPrecondition, "ticket version does not match")
	}
	if in.Status != "" {
		if !validStatus(in.Status) {
			return nil, rpcError(domain.ErrInvalidRequest)
		}
		ticket.Status = in.Status
	}
	if in.Priority != "" {
		if !validPriority(in.Priority) {
			return nil, rpcError(domain.ErrInvalidRequest)
		}
		ticket.Priority = in.Priority
	}
	if in.AssigneeId != "" {
		if err = validateAssignee(l.ctx, l.svcCtx, in.AssigneeId); err != nil {
			return nil, err
		}
		ticket.AssigneeID = in.AssigneeId
	}
	if in.ClearDueAt {
		ticket.DueAt = nil
	} else if in.DueAt != nil {
		due, err := parseTimestamp(in.DueAt)
		if err != nil {
			return nil, err
		}
		ticket.DueAt = due
	}
	if err = l.svcCtx.Store.UpdateTicket(l.ctx, ticket, currentAgent.ID); err != nil {
		return nil, rpcError(err)
	}
	ticket, err = l.svcCtx.Store.Ticket(l.ctx, ticket.ID)
	if err != nil {
		return nil, rpcError(err)
	}
	return ticketView(ticket), nil
}
