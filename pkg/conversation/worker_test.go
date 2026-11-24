package conversation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/example/customer-service-platform/pkg/wechat"
)

type adapterRecorder struct {
	mu            sync.Mutex
	notifications []domain.Notification
	sends         int
	err           error
}

func (a *adapterRecorder) Notify(_ context.Context, n domain.Notification) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notifications = append(a.notifications, n)
	return a.err
}
func (a *adapterRecorder) SendMessage(context.Context, string, string, string, string, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sends++
	return a.err
}
func workerFixture(t *testing.T) (*store.Memory, *adapterRecorder, *Worker, domain.Conversation) {
	t.Helper()
	repository := store.NewMemory()
	agent := domain.Agent{ID: "agent", DingTalkUserID: "ding", Role: domain.RoleAgent, Active: true}
	_ = repository.SaveAgent(context.Background(), agent)
	_ = repository.SaveTeamAgent(context.Background(), "team", agent.ID)
	_ = repository.SaveShift(context.Background(), domain.Shift{ID: "shift", AgentID: agent.ID, StartAt: time.Now().Add(-time.Hour), EndAt: time.Now().Add(time.Hour), State: "active"})
	_ = repository.SaveRoutingRule(context.Background(), domain.RoutingRule{ID: "rule", TeamID: "team", Priority: 1, Active: true})
	account := domain.ChannelAccount{ID: "account", AccountKey: "account", Channel: domain.ChannelService, AppID: "app", Status: "active"}
	_ = repository.SaveAccount(context.Background(), account)
	_, err := repository.ReceiveInbound(context.Background(), domain.Inbound{EventID: "message", OpenID: "customer", Type: "text", Content: "hello", ReceivedAt: time.Now().UTC()}, account)
	if err != nil {
		t.Fatal(err)
	}
	conversations, _ := repository.Conversations(context.Background(), store.ConversationFilter{})
	adapter := &adapterRecorder{}
	return repository, adapter, &Worker{Store: repository, Notifier: adapter, Sender: adapter, Now: time.Now}, conversations[0]
}
func TestWorkerRoutesNotifiesAndSendsOnce(t *testing.T) {
	repository, adapter, worker, conversation := workerFixture(t)
	if err := worker.ProcessOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	assigned, _ := repository.Conversation(context.Background(), conversation.ID)
	if assigned.AssigneeID != "agent" {
		t.Fatal("not routed")
	}
	message, err := repository.QueueMessage(context.Background(), conversation.ID, domain.Agent{ID: "agent", Role: domain.RoleAgent, Active: true}, "text", "reply", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.ProcessOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if adapter.sends != 1 {
		t.Fatalf("sends=%d", adapter.sends)
	}
	sent, _ := repository.Message(context.Background(), message.ID)
	if sent.Status != domain.StatusSent {
		t.Fatal(sent.Status)
	}
	if len(adapter.notifications) != 1 {
		t.Fatalf("notifications=%d", len(adapter.notifications))
	}
}
func TestWorkerNoShiftDoesNotAssignAndSLAOnce(t *testing.T) {
	repository, adapter, worker, conversation := workerFixture(t)
	_ = repository.SaveShift(context.Background(), domain.Shift{ID: "shift", AgentID: "agent", StartAt: time.Now().Add(-time.Hour), EndAt: time.Now().Add(-time.Minute), State: "active"})
	worker.Now = func() time.Time { return time.Now().Add(49 * time.Hour) }
	if err := worker.ProcessOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := repository.Conversation(context.Background(), conversation.ID)
	if unchanged.AssigneeID != "" {
		t.Fatal("off shift agent assigned")
	}
	for i := 0; i < 2; i++ {
		if err := worker.SLA(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	slaNotifications := 0
	for _, notification := range adapter.notifications {
		if notification.Kind == "sla_expired" {
			slaNotifications++
		}
	}
	if slaNotifications != 1 {
		t.Fatalf("SLA notified more than once: %d", slaNotifications)
	}
}
func TestFailedSendIsFinal(t *testing.T) {
	repository, adapter, worker, conversation := workerFixture(t)
	if err := worker.ProcessOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	adapter.err = &wechat.APIError{Code: 45047, Message: "reply window expired"}
	message, err := repository.QueueMessage(context.Background(), conversation.ID, domain.Agent{ID: "agent", Role: domain.RoleAgent, Active: true}, "text", "reply", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Send(context.Background(), message.ID); err != nil {
		t.Fatal(err)
	}
	updated, _ := repository.Message(context.Background(), message.ID)
	if updated.Status != domain.StatusFailed {
		t.Fatal(updated.Status)
	}
}
