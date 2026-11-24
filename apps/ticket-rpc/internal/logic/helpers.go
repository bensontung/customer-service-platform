package logic

import (
	"context"
	"errors"
	"time"

	"github.com/example/customer-service-platform/apps/ticket-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/ticket-rpc/pb/github.com/example/customer-service-platform/apps/ticket-rpc/pb"
	"github.com/example/customer-service-platform/pkg/auth"
	"github.com/example/customer-service-platform/pkg/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func currentAgent(ctx context.Context, svcCtx *svc.ServiceContext) (domain.Agent, error) {
	id, err := auth.AgentID(ctx)
	if err != nil {
		return domain.Agent{}, rpcError(err)
	}
	result, err := svcCtx.Store.AgentByID(ctx, id)
	if err != nil || !result.Active {
		return domain.Agent{}, rpcError(domain.ErrForbidden)
	}
	return result, nil
}

func ticketView(t domain.Ticket) *pb.Ticket {
	return &pb.Ticket{
		Id: t.ID, CustomerId: t.CustomerID, ConversationId: t.ConversationID, CommentId: t.CommentID,
		Title: t.Title, Description: t.Description, Status: t.Status, Priority: t.Priority,
		AssigneeId: t.AssigneeID, DueAt: optionalTimestamp(t.DueAt), Version: t.Version,
		CreatedAt: timestamp(t.CreatedAt), UpdatedAt: timestamp(t.UpdatedAt),
	}
}

func parseTimestamp(value *timestamppb.Timestamp) (*time.Time, error) {
	if err := value.CheckValid(); err != nil {
		return nil, rpcError(domain.ErrInvalidRequest)
	}
	result := value.AsTime().UTC()
	return &result, nil
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamp(*value)
}

func validStatus(value string) bool {
	switch value {
	case "open", "processing", "resolved", "closed":
		return true
	default:
		return false
	}
}

func validPriority(value string) bool {
	switch value {
	case "low", "normal", "high", "urgent":
		return true
	default:
		return false
	}
}

func validateAssignee(ctx context.Context, svcCtx *svc.ServiceContext, id string) error {
	result, err := svcCtx.Store.AgentByID(ctx, id)
	if err != nil || !result.Active {
		return rpcError(domain.ErrInvalidRequest)
	}
	return nil
}

func rpcError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, "resource not found")
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrNotAssigned):
		return status.Error(codes.PermissionDenied, "permission denied")
	case errors.Is(err, domain.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, "authentication required")
	case errors.Is(err, domain.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, "invalid request")
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrDuplicate), errors.Is(err, domain.ErrReplyExpired), errors.Is(err, domain.ErrReplyQuota):
		return status.Error(codes.FailedPrecondition, "operation is not allowed in the current state")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	default:
		return status.Error(codes.Internal, "internal error")
	}
}
