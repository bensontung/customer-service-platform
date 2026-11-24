package logic

import (
	"context"
	"testing"
	"time"

	"github.com/example/customer-service-platform/apps/ticket-rpc/internal/config"
	"github.com/example/customer-service-platform/apps/ticket-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/ticket-rpc/pb/github.com/example/customer-service-platform/apps/ticket-rpc/pb"
	"github.com/example/customer-service-platform/pkg/auth"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ticketService(t *testing.T) (*store.Memory, *svc.ServiceContext, context.Context, domain.Conversation) {
	t.Helper()
	repository := store.NewMemory()
	if err := repository.SaveAgent(context.Background(), domain.Agent{ID: "agent", DingTalkUserID: "agent-ding", Active: true, Role: domain.RoleAgent}); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveAgent(context.Background(), domain.Agent{ID: "other", DingTalkUserID: "other-ding", Active: true, Role: domain.RoleAgent}); err != nil {
		t.Fatal(err)
	}
	_, err := repository.ReceiveInbound(context.Background(), domain.Inbound{EventID: "event", OpenID: "open", ReceivedAt: time.Now().UTC()}, domain.ChannelAccount{ID: "account", Channel: domain.ChannelService, AppID: "app"})
	if err != nil {
		t.Fatal(err)
	}
	conversations, err := repository.Conversations(context.Background(), store.ConversationFilter{})
	if err != nil || len(conversations) != 1 {
		t.Fatal("conversation fixture missing")
	}
	return repository, svc.NewServiceContextWithStore(config.Config{}, repository), context.WithValue(context.Background(), auth.AgentIDKey, "agent"), conversations[0]
}

func TestTicketCreateUpdateGet(t *testing.T) {
	repository, svcCtx, ctx, conversation := ticketService(t)
	due := time.Now().Add(time.Hour).UTC()
	created, err := NewCreateTicketLogic(ctx, svcCtx).CreateTicket(&pb.CreateTicketRequest{ConversationId: conversation.ID, Title: "Refund", Description: "Investigate refund", Priority: "high", AssigneeId: "agent", DueAt: timestamppb.New(due)})
	if err != nil {
		t.Fatal(err)
	}
	if created.Id == "" || created.CustomerId != conversation.CustomerID || created.Status != "open" || created.DueAt == nil {
		t.Fatalf("unexpected ticket: %+v", created)
	}
	updated, err := NewUpdateTicketLogic(ctx, svcCtx).UpdateTicket(&pb.UpdateTicketRequest{Id: created.Id, Status: "processing", Priority: "urgent", ClearDueAt: true, ExpectedVersion: created.Version})
	if err != nil || updated.Status != "processing" || updated.Priority != "urgent" || updated.DueAt != nil || updated.Version != created.Version+1 {
		t.Fatalf("unexpected update: %+v %v", updated, err)
	}
	got, err := NewGetTicketLogic(ctx, svcCtx).GetTicket(&pb.GetTicketRequest{Id: created.Id})
	if err != nil || got.Version != updated.Version || got.CustomerId != created.CustomerId {
		t.Fatalf("get ticket: %+v %v", got, err)
	}
	events, err := repository.TicketEvents(ctx, created.Id)
	if err != nil || len(events) != 2 {
		t.Fatalf("expected create/update events: %v", err)
	}
}

func TestTicketVersionAndPermissions(t *testing.T) {
	_, svcCtx, ctx, conversation := ticketService(t)
	created, err := NewCreateTicketLogic(ctx, svcCtx).CreateTicket(&pb.CreateTicketRequest{ConversationId: conversation.ID, Title: "Issue", AssigneeId: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewUpdateTicketLogic(ctx, svcCtx).UpdateTicket(&pb.UpdateTicketRequest{Id: created.Id, ExpectedVersion: created.Version + 1})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected failed precondition, got %v", err)
	}
	_, err = NewUpdateTicketLogic(context.WithValue(ctx, auth.AgentIDKey, "other"), svcCtx).UpdateTicket(&pb.UpdateTicketRequest{Id: created.Id, ExpectedVersion: created.Version})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestTicketInvalidRequest(t *testing.T) {
	_, svcCtx, ctx, conversation := ticketService(t)
	_, err := NewCreateTicketLogic(ctx, svcCtx).CreateTicket(&pb.CreateTicketRequest{ConversationId: conversation.ID, Title: " "})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}
