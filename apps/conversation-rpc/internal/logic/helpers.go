package logic

import (
	"context"
	"errors"
	"time"

	"github.com/example/customer-service-platform/apps/conversation-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/conversation-rpc/pb/github.com/example/customer-service-platform/apps/conversation-rpc/pb"
	"github.com/example/customer-service-platform/pkg/auth"
	"github.com/example/customer-service-platform/pkg/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func agent(ctx context.Context, svcCtx *svc.ServiceContext) (domain.Agent, error) {
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

func claimedAgent(ctx context.Context, svcCtx *svc.ServiceContext, id string) (domain.Agent, error) {
	if id == "" {
		return domain.Agent{}, status.Error(codes.InvalidArgument, "agent_id is required")
	}
	current, err := agent(ctx, svcCtx)
	if err != nil {
		return domain.Agent{}, err
	}
	if current.ID != id {
		return domain.Agent{}, status.Error(codes.PermissionDenied, "agent identity mismatch")
	}
	return current, nil
}

func requireVersion(current, expected int64) error {
	if expected < 0 {
		return status.Error(codes.InvalidArgument, "expected_version must not be negative")
	}
	if expected > 0 && current != expected {
		return status.Error(codes.FailedPrecondition, "conversation version does not match")
	}
	return nil
}

func conversationView(c domain.Conversation) *pb.Conversation {
	return &pb.Conversation{
		Id: c.ID, Source: c.Source, ChannelAccountId: c.ChannelAccountID, CustomerId: c.CustomerID,
		Status: c.Status, AssigneeId: c.AssigneeID, TeamId: c.TeamID, Version: c.Version,
		LastInboundAt: timestamp(c.LastInboundAt), ReplyDeadlineAt: timestamp(c.ReplyDeadlineAt),
		RepliesInWindow: int64(c.RepliesInWindow), FirstResponseAt: optionalTimestamp(c.FirstResponseAt),
		SlaEscalatedAt: optionalTimestamp(c.SLAEscalatedAt), CreatedAt: timestamp(c.CreatedAt), UpdatedAt: timestamp(c.UpdatedAt),
	}
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

func rpcError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrNotAssigned):
		return status.Error(codes.PermissionDenied, "permission denied")
	case errors.Is(err, domain.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, "authentication required")
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, "resource not found")
	case errors.Is(err, domain.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, "invalid request")
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrDuplicate),
		errors.Is(err, domain.ErrReplyExpired), errors.Is(err, domain.ErrReplyQuota):
		return status.Error(codes.FailedPrecondition, "operation is not allowed in the current state")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	default:
		return status.Error(codes.Internal, "internal error")
	}
}
