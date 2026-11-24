package logic

import (
	"context"
	"testing"
	"time"

	"github.com/example/customer-service-platform/apps/conversation-rpc/internal/config"
	"github.com/example/customer-service-platform/apps/conversation-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/conversation-rpc/pb/github.com/example/customer-service-platform/apps/conversation-rpc/pb"
	"github.com/example/customer-service-platform/pkg/auth"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func conversationFixture(t *testing.T) (*store.Memory, *svc.ServiceContext, context.Context) {
	t.Helper()
	repository := store.NewMemory()
	ctx := context.Background()
	account := domain.ChannelAccount{ID: "account", AccountKey: "support", Channel: domain.ChannelService, Status: "active"}
	if err := repository.SaveAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	created, err := repository.ReceiveInbound(ctx, domain.Inbound{EventID: "event", OpenID: "customer", Type: "text", Content: "hello", ReceivedAt: time.Now().UTC()}, account)
	if err != nil || !created {
		t.Fatalf("create conversation: %v", err)
	}
	if err := repository.SaveAgent(ctx, domain.Agent{ID: "agent", DingTalkUserID: "agent-ding", Active: true, Role: domain.RoleAgent}); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveAgent(ctx, domain.Agent{ID: "other", DingTalkUserID: "other-ding", Active: true, Role: domain.RoleAgent}); err != nil {
		t.Fatal(err)
	}
	conversations, err := repository.Conversations(ctx, store.ConversationFilter{})
	if err != nil || len(conversations) != 1 {
		t.Fatalf("load conversation: %v", err)
	}
	return repository, svc.NewServiceContextWithStore(config.Config{}, repository), context.WithValue(ctx, auth.AgentIDKey, "agent")
}

func TestConversationLifecycleAndPermissions(t *testing.T) {
	repository, svcCtx, ctx := conversationFixture(t)
	conversations, err := repository.Conversations(ctx, store.ConversationFilter{})
	if err != nil {
		t.Fatal(err)
	}
	id := conversations[0].ID
	claimed, err := NewClaimConversationLogic(ctx, svcCtx).ClaimConversation(&pb.ClaimConversationRequest{ConversationId: id, AgentId: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Status != domain.StatusOpen || claimed.AssigneeId != "agent" || claimed.LastInboundAt == nil {
		t.Fatalf("unexpected claim result: %+v", claimed)
	}
	got, err := NewGetConversationLogic(ctx, svcCtx).GetConversation(&pb.GetConversationRequest{Id: id})
	if err != nil || got.Id != id {
		t.Fatalf("get conversation: %v", err)
	}
	queued, err := NewQueueMessageLogic(ctx, svcCtx).QueueMessage(&pb.QueueMessageRequest{ConversationId: id, AgentId: "agent", Type: "text", Text: "reply"})
	if err != nil || queued.MessageId == "" {
		t.Fatalf("queue message: %v", err)
	}
	_, err = NewQueueMessageLogic(context.WithValue(ctx, auth.AgentIDKey, "other"), svcCtx).QueueMessage(&pb.QueueMessageRequest{ConversationId: id, AgentId: "other", Type: "text", Text: "not allowed"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", err)
	}
	closed, err := NewCloseConversationLogic(ctx, svcCtx).CloseConversation(&pb.CloseConversationRequest{ConversationId: id, AgentId: "agent", ExpectedVersion: 2})
	if err != nil || closed.Status != domain.StatusClosed {
		t.Fatalf("close conversation: %v", err)
	}
}

func TestConversationTransferAndVersion(t *testing.T) {
	_, svcCtx, ctx := conversationFixture(t)
	claimed, err := NewClaimConversationLogic(ctx, svcCtx).ClaimConversation(&pb.ClaimConversationRequest{ConversationId: conversationID(t, svcCtx), AgentId: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewTransferConversationLogic(ctx, svcCtx).TransferConversation(&pb.TransferConversationRequest{ConversationId: claimed.Id, ActorId: "agent", AssigneeId: "other", ExpectedVersion: claimed.Version + 1})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected stale version error, got %v", err)
	}
	transferred, err := NewTransferConversationLogic(ctx, svcCtx).TransferConversation(&pb.TransferConversationRequest{ConversationId: claimed.Id, ActorId: "agent", AssigneeId: "other", ExpectedVersion: claimed.Version})
	if err != nil || transferred.AssigneeId != "other" {
		t.Fatalf("transfer: %v", err)
	}
}

func TestConversationNotFound(t *testing.T) {
	_, svcCtx, ctx := conversationFixture(t)
	_, err := NewGetConversationLogic(ctx, svcCtx).GetConversation(&pb.GetConversationRequest{Id: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}

func conversationID(t *testing.T, svcCtx *svc.ServiceContext) string {
	t.Helper()
	conversations, err := svcCtx.Store.Conversations(context.Background(), store.ConversationFilter{})
	if err != nil || len(conversations) == 0 {
		t.Fatalf("load conversation: %v", err)
	}
	return conversations[0].ID
}
