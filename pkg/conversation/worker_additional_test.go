package conversation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/example/customer-service-platform/pkg/store"
	"github.com/example/customer-service-platform/pkg/wechat"
)

func TestWorkerConcurrentOutboxAndMessageSendOnce(t *testing.T) {
	repository, adapter, worker, conversation := workerFixture(t)
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := worker.ProcessOutbox(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if len(adapter.notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(adapter.notifications))
	}
	message, err := repository.QueueMessage(context.Background(), conversation.ID, domain.Agent{ID: "agent", Role: domain.RoleAgent, Active: true}, "text", "reply", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := worker.Send(context.Background(), message.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if adapter.sends != 1 {
		t.Fatalf("sends = %d, want 1", adapter.sends)
	}
}

func TestWorkerRetriesRecoversAndMarksQuotaFinal(t *testing.T) {
	repository, adapter, worker, conversation := workerFixture(t)
	if err := worker.ProcessOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	adapter.err = &wechat.APIError{Code: -1, Message: "temporary", Retryable: true}
	message, err := repository.QueueMessage(context.Background(), conversation.ID, domain.Agent{ID: "agent", Role: domain.RoleAgent, Active: true}, "text", "reply", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Send(context.Background(), message.ID); err != nil {
		t.Fatal(err)
	}
	message, _ = repository.Message(context.Background(), message.ID)
	if message.Status != domain.StatusRetrying || message.RetryCount != 1 || message.NextRetryAt == nil {
		t.Fatalf("retry state = %#v", message)
	}
	if err = worker.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	adapter.err = nil
	retryAt := message.NextRetryAt.Add(time.Second)
	worker.Now = func() time.Time { return retryAt }
	if err = worker.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	message, _ = repository.Message(context.Background(), message.ID)
	if message.Status != domain.StatusSent || adapter.sends != 2 {
		t.Fatalf("recovered state = %#v, sends = %d", message, adapter.sends)
	}
	adapter.err = &wechat.APIError{Code: 45047, Message: "expired"}
	second, err := repository.QueueMessage(context.Background(), conversation.ID, domain.Agent{ID: "agent", Role: domain.RoleAgent, Active: true}, "text", "second", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Send(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
	second, _ = repository.Message(context.Background(), second.ID)
	if second.Status != domain.StatusFailed || second.NextRetryAt != nil {
		t.Fatalf("terminal state = %#v", second)
	}
}

func TestWorkerSLAWarningAndExpiredTicketOnce(t *testing.T) {
	repository, adapter, worker, conversation := workerFixture(t)
	if err := worker.ProcessOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	worker.Now = func() time.Time { return conversation.ReplyDeadlineAt.Add(-5 * time.Minute) }
	ticket := domain.Ticket{ID: "ticket", ConversationID: conversation.ID, Title: "overdue", Status: "open", Priority: "high", DueAt: timePtr(time.Now().Add(-time.Minute)), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := repository.CreateTicket(context.Background(), ticket, "agent"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := worker.SLA(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	counts := map[string]int{}
	for _, notification := range adapter.notifications {
		counts[notification.Kind]++
	}
	if counts["sla_warning"] != 1 || counts["ticket_expired"] != 1 {
		t.Fatalf("notifications = %#v", counts)
	}
	tickets, err := repository.ExpiredTickets(context.Background(), time.Now())
	if err != nil || len(tickets) != 1 {
		t.Fatalf("ExpiredTickets() = %d, %v", len(tickets), err)
	}
}

func TestWorkerProcessErrorsAndRetriesOutbox(t *testing.T) {
	_, adapter, worker, _ := workerFixture(t)
	adapter.err = errors.New("temporary")
	if err := worker.ProcessOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(adapter.notifications) != 1 {
		t.Fatal("notification should have failed before completion")
	}
	adapter.err = nil
	worker.Now = func() time.Time { return time.Now().Add(31 * time.Second) }
	if err := worker.ProcessOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(adapter.notifications) != 2 {
		t.Fatalf("notifications = %d, want initial failure and retry", len(adapter.notifications))
	}
	if err := worker.Process(context.Background(), domain.OutboxEvent{Type: "MessageSendRequested", Payload: []byte("{")}); err == nil {
		t.Fatal("expected malformed payload error")
	}
	if err := worker.Process(context.Background(), domain.OutboxEvent{Type: "unknown"}); err == nil {
		t.Fatal("expected unknown event error")
	}
}

type commentSyncer struct {
	pages int
}

func (s *commentSyncer) SyncComments(_ context.Context, accountKey, msgDataID string, articleIndex, cursor int) ([]domain.Comment, error) {
	s.pages++
	if accountKey != "account" || msgDataID != "article" || articleIndex != 0 || cursor > 49 {
		return nil, errors.New("invalid comment cursor")
	}
	if cursor == 0 {
		comments := make([]domain.Comment, 49)
		for i := range comments {
			comments[i] = domain.Comment{ID: string(rune(i + 1)), ChannelAccountID: "account", MsgDataID: "article", CommentID: string(rune(i + 1)), SyncedAt: time.Now()}
		}
		return comments, nil
	}
	return []domain.Comment{{ID: "last", ChannelAccountID: "account", MsgDataID: "article", CommentID: "last", SyncedAt: time.Now()}}, nil
}

func TestCommentSyncPaginatesAndPersists(t *testing.T) {
	repository := store.NewMemory()
	client := &commentSyncer{}
	worker := &Worker{Store: repository}
	_ = worker
	if err := worker.SyncComments(context.Background(), client, []CommentSource{{AccountKey: "account", MsgDataID: "article"}}); err != nil {
		t.Fatal(err)
	}
	if client.pages != 2 {
		t.Fatalf("pages = %d", client.pages)
	}
	comments, err := repository.Comments(context.Background(), "account", "")
	if err != nil || len(comments) != 50 {
		t.Fatalf("comments = %d, %v", len(comments), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker.RunCommentSync(ctx, client, nil)
}

func timePtr(value time.Time) *time.Time { return &value }
