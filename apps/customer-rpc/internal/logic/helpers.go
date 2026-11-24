package logic

import (
	"context"
	"errors"
	"strings"

	"github.com/example/customer-service-platform/apps/customer-rpc/pb/github.com/example/customer-service-platform/apps/customer-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func customerView(c domain.Customer) *pb.Customer {
	tags := append([]string{}, c.Tags...)
	return &pb.Customer{Id: c.ID, Channel: c.Channel, AppId: c.AppID, OpenId: c.OpenID, UnionId: c.UnionID, Nickname: c.Nickname, Note: c.Note, Tags: tags}
}

func validateTags(tags []string) error {
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" || len(tag) > 64 {
			return rpcError(domain.ErrInvalidRequest)
		}
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
