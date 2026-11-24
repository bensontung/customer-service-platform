package logic

import (
	"context"
	"reflect"
	"testing"

	"github.com/example/customer-service-platform/apps/customer-rpc/internal/config"
	"github.com/example/customer-service-platform/apps/customer-rpc/internal/svc"
	"github.com/example/customer-service-platform/apps/customer-rpc/pb/github.com/example/customer-service-platform/apps/customer-rpc/pb"
	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func customerService(t *testing.T) (store.Store, *svc.ServiceContext, string) {
	t.Helper()
	repository := store.NewMemory()
	created, err := repository.ReceiveInbound(context.Background(), domain.Inbound{EventID: "event", OpenID: "open-id", Type: "text"}, domain.ChannelAccount{ID: "account", Channel: domain.ChannelService, AppID: "app"})
	if err != nil || !created {
		t.Fatalf("create customer: %v", err)
	}
	conversations, err := repository.Conversations(context.Background(), store.ConversationFilter{})
	if err != nil || len(conversations) != 1 {
		t.Fatal("customer fixture missing")
	}
	return repository, svc.NewServiceContextWithStore(config.Config{}, repository), conversations[0].CustomerID
}

func TestCustomerGetAndUpdate(t *testing.T) {
	repository, svcCtx, id := customerService(t)
	got, err := NewGetCustomerLogic(context.Background(), svcCtx).GetCustomer(&pb.GetCustomerRequest{Id: id})
	if err != nil || got.OpenId != "open-id" || got.AppId != "app" || len(got.Tags) != 0 {
		t.Fatalf("get customer: %+v %v", got, err)
	}
	updated, err := NewUpdateCustomerLogic(context.Background(), svcCtx).UpdateCustomer(&pb.UpdateCustomerRequest{Id: id, Note: "VIP", Tags: []string{"priority", "wechat"}})
	if err != nil || updated.Note != "VIP" || !reflect.DeepEqual(updated.Tags, []string{"priority", "wechat"}) {
		t.Fatalf("update customer: %+v %v", updated, err)
	}
	stored, err := repository.Customer(context.Background(), id)
	if err != nil || stored.Note != updated.Note || !reflect.DeepEqual(stored.Tags, updated.Tags) {
		t.Fatalf("customer not persisted: %+v %v", stored, err)
	}
}

func TestCustomerInvalidAndNotFound(t *testing.T) {
	_, svcCtx, _ := customerService(t)
	_, err := NewUpdateCustomerLogic(context.Background(), svcCtx).UpdateCustomer(&pb.UpdateCustomerRequest{Id: "missing", Tags: []string{" "}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	_, err = NewGetCustomerLogic(context.Background(), svcCtx).GetCustomer(&pb.GetCustomerRequest{Id: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}
