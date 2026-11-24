package logic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/pb/github.com/example/customer-service-platform/apps/dingtalk-adapter-rpc/pb"
	"github.com/example/customer-service-platform/pkg/dingtalk"
	"github.com/example/customer-service-platform/pkg/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func toNotification(in *pb.SendNotificationRequest) domain.Notification {
	return domain.Notification{AgentID: in.AgentId, ConversationID: in.ConversationId, Kind: in.Kind, Title: in.Title, Text: in.Text, URL: in.Url}
}

func notificationID(n domain.Notification) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{n.AgentID, n.ConversationID, n.Kind, n.Title, n.Text, n.URL}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func rpcError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, "resource not found")
	case errors.Is(err, domain.ErrForbidden):
		return status.Error(codes.PermissionDenied, "permission denied")
	case errors.Is(err, domain.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, "invalid request")
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrDuplicate), errors.Is(err, domain.ErrNotAssigned), errors.Is(err, domain.ErrReplyExpired), errors.Is(err, domain.ErrReplyQuota):
		return status.Error(codes.FailedPrecondition, "operation is not allowed in the current state")
	case errors.Is(err, dingtalk.ErrRateLimited):
		return status.Error(codes.Unavailable, "upstream service rate limited")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	default:
		return status.Error(codes.Unavailable, "upstream service unavailable")
	}
}
