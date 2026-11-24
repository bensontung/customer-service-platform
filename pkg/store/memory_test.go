package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
)

func memoryFixture(t *testing.T) (*Memory, domain.ChannelAccount, domain.Agent, domain.Inbound) {
	t.Helper()
	m := NewMemory()
	fixed := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixed }
	account := domain.ChannelAccount{
		ID: "account-1", AccountKey: "service", Channel: domain.ChannelService, AppID: "app-1", Status: "active",
	}
	agent := domain.Agent{ID: "agent-1", DingTalkUserID: "ding-1", Role: domain.RoleAgent, Active: true}
	if err := m.SaveAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveAgent(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	inbound := domain.Inbound{
		EventID: "event-1", PayloadHash: "hash", OpenID: "openid-1", Type: "text",
		Content: "hello", ReceivedAt: fixed.Add(-time.Hour),
	}
	return m, account, agent, inbound
}

func onlyConversation(t *testing.T, m *Memory) domain.Conversation {
	t.Helper()
	conversations, err := m.Conversations(context.Background(), ConversationFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 1 {
		t.Fatalf("got %d conversations, want 1", len(conversations))
	}
	return conversations[0]
}

func TestMemoryReceiveInboundLinksRecordsAndDeduplicates(t *testing.T) {
	m, account, _, inbound := memoryFixture(t)
	ctx := context.Background()
	created, err := m.ReceiveInbound(ctx, inbound, account)
	if err != nil || !created {
		t.Fatalf("ReceiveInbound() = %v, %v; want true, nil", created, err)
	}
	created, err = m.ReceiveInbound(ctx, inbound, account)
	if err != nil || created {
		t.Fatalf("duplicate ReceiveInbound() = %v, %v; want false, nil", created, err)
	}
	conversation := onlyConversation(t, m)
	if conversation.Status != domain.StatusWaiting || conversation.ChannelAccountID != account.ID {
		t.Fatalf("conversation = %+v", conversation)
	}
	if !conversation.ReplyDeadlineAt.Equal(domain.NewReplyWindow(inbound.ReceivedAt)) {
		t.Fatalf("deadline = %v", conversation.ReplyDeadlineAt)
	}
	customer, err := m.Customer(ctx, conversation.CustomerID)
	if err != nil {
		t.Fatal(err)
	}
	if customer.Channel != account.Channel || customer.AppID != account.AppID || customer.OpenID != inbound.OpenID {
		t.Fatalf("customer = %+v", customer)
	}
	messages, err := m.Messages(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(messages))
	}
	if messages[0].ConversationID != conversation.ID || messages[0].ChannelMessageID != inbound.EventID || messages[0].Channel != account.Channel {
		t.Fatalf("message = %+v", messages[0])
	}
	events, err := m.ClaimOutbox(ctx, m.now())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "MessageReceived" || events[0].AggregateID != conversation.ID {
		t.Fatalf("events = %+v", events)
	}
	var payload map[string]string
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["messageId"] != messages[0].ID || payload["conversationId"] != conversation.ID {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestMemoryReceiveInboundConcurrentDuplicate(t *testing.T) {
	m, account, _, inbound := memoryFixture(t)
	const workers = 32
	var created atomic.Int32
	var wg sync.WaitGroup
	errorsCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := m.ReceiveInbound(context.Background(), inbound, account)
			if err != nil {
				errorsCh <- err
			}
			if ok {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
	if created.Load() != 1 {
		t.Fatalf("created count = %d, want 1", created.Load())
	}
	conversation := onlyConversation(t, m)
	messages, err := m.Messages(context.Background(), conversation.ID)
	if err != nil || len(messages) != 1 {
		t.Fatalf("Messages() = %d, %v; want 1", len(messages), err)
	}
	events, err := m.ClaimOutbox(context.Background(), m.now())
	if err != nil || len(events) != 1 {
		t.Fatalf("ClaimOutbox() = %d, %v; want 1", len(events), err)
	}
}

func TestMemoryInboundResetsWindowAndReopensAfterClose(t *testing.T) {
	m, account, agent, inbound := memoryFixture(t)
	ctx := context.Background()
	if _, err := m.ReceiveInbound(ctx, inbound, account); err != nil {
		t.Fatal(err)
	}
	first := onlyConversation(t, m)
	claimed, err := m.ClaimConversation(ctx, first.ID, agent)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := m.QueueMessage(ctx, first.ID, agent, "text", fmt.Sprintf("reply-%d", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	inbound.EventID = "event-2"
	inbound.ReceivedAt = inbound.ReceivedAt.Add(24 * time.Hour)
	if _, err := m.ReceiveInbound(ctx, inbound, account); err != nil {
		t.Fatal(err)
	}
	reopened, err := m.Conversation(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ID != claimed.ID || reopened.RepliesInWindow != 0 {
		t.Fatalf("reopened conversation = %+v", reopened)
	}
	if got, want := reopened.ReplyDeadlineAt, domain.NewReplyWindow(inbound.ReceivedAt); !got.Equal(want) {
		t.Fatalf("deadline = %v, want %v", got, want)
	}
	if _, err := m.CloseConversation(ctx, first.ID, agent); err != nil {
		t.Fatal(err)
	}
	inbound.EventID = "event-3"
	inbound.ReceivedAt = inbound.ReceivedAt.Add(time.Hour)
	if _, err := m.ReceiveInbound(ctx, inbound, account); err != nil {
		t.Fatal(err)
	}
	conversations, err := m.Conversations(ctx, ConversationFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 2 {
		t.Fatalf("got %d conversations, want 2", len(conversations))
	}
	for _, conversation := range conversations {
		if conversation.CustomerID != first.CustomerID {
			t.Fatalf("conversation customer = %q, want %q", conversation.CustomerID, first.CustomerID)
		}
		if conversation.ID != first.ID && (conversation.Status != domain.StatusWaiting || conversation.AssigneeID != "") {
			t.Fatalf("new conversation = %+v", conversation)
		}
	}
}

func TestMemoryClaimConversationOnlyOneWinner(t *testing.T) {
	m, account, agent, inbound := memoryFixture(t)
	ctx := context.Background()
	if _, err := m.ReceiveInbound(ctx, inbound, account); err != nil {
		t.Fatal(err)
	}
	conversation := onlyConversation(t, m)
	const workers = 16
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.ClaimConversation(ctx, conversation.ID, agent)
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, domain.ErrConflict) {
				t.Errorf("ClaimConversation() error = %v", err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful claims = %d, want 1", successes.Load())
	}
	claimed, err := m.Conversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Status != domain.StatusOpen || claimed.AssigneeID != agent.ID || claimed.Version != 1 {
		t.Fatalf("claimed conversation = %+v", claimed)
	}
}

func TestMemoryQueueMessageValidatesClaimWindowAndQuota(t *testing.T) {
	m, account, agent, inbound := memoryFixture(t)
	ctx := context.Background()
	if _, err := m.ReceiveInbound(ctx, inbound, account); err != nil {
		t.Fatal(err)
	}
	conversation := onlyConversation(t, m)
	if _, err := m.QueueMessage(ctx, conversation.ID, agent, "text", "before claim", ""); !errors.Is(err, domain.ErrNotAssigned) {
		t.Fatalf("unassigned QueueMessage() error = %v", err)
	}
	if _, err := m.ClaimConversation(ctx, conversation.ID, agent); err != nil {
		t.Fatal(err)
	}
	if _, err := m.QueueMessage(ctx, conversation.ID, domain.Agent{ID: "other", Role: domain.RoleAgent, Active: true}, "text", "no", ""); !errors.Is(err, domain.ErrNotAssigned) {
		t.Fatalf("other agent QueueMessage() error = %v", err)
	}
	for _, test := range []struct{ kind, text, mediaID string }{
		{kind: "text", text: "  "},
		{kind: "audio", text: "bad", mediaID: "media"},
		{kind: "image"},
	} {
		if _, err := m.QueueMessage(ctx, conversation.ID, agent, test.kind, test.text, test.mediaID); !errors.Is(err, domain.ErrInvalidRequest) {
			t.Fatalf("QueueMessage(%q) error = %v", test.kind, err)
		}
	}
	for i := 0; i < 5; i++ {
		message, err := m.QueueMessage(ctx, conversation.ID, agent, "text", fmt.Sprintf("reply %d", i), "")
		if err != nil {
			t.Fatalf("QueueMessage() %d error = %v", i, err)
		}
		if message.Channel != account.Channel || message.Direction != "outbound" || message.Status != domain.StatusQueued {
			t.Fatalf("queued message = %+v", message)
		}
	}
	if _, err := m.QueueMessage(ctx, conversation.ID, agent, "text", "sixth", ""); !errors.Is(err, domain.ErrReplyQuota) {
		t.Fatalf("sixth QueueMessage() error = %v", err)
	}
	updated, err := m.Conversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.RepliesInWindow != 5 || updated.Version != 6 {
		t.Fatalf("conversation = %+v", updated)
	}
	messages, err := m.Messages(ctx, conversation.ID)
	if err != nil || len(messages) != 6 {
		t.Fatalf("Messages() = %d, %v; want inbound + five replies", len(messages), err)
	}
	m.now = func() time.Time { return updated.ReplyDeadlineAt }
	if _, err := m.QueueMessage(ctx, conversation.ID, agent, "text", "late", ""); !errors.Is(err, domain.ErrReplyExpired) {
		t.Fatalf("expired QueueMessage() error = %v", err)
	}
}

func TestMemoryQueueMessageConcurrentQuota(t *testing.T) {
	m, account, agent, inbound := memoryFixture(t)
	ctx := context.Background()
	if _, err := m.ReceiveInbound(ctx, inbound, account); err != nil {
		t.Fatal(err)
	}
	conversation := onlyConversation(t, m)
	if _, err := m.ClaimConversation(ctx, conversation.ID, agent); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := m.QueueMessage(ctx, conversation.ID, agent, "text", fmt.Sprintf("reply-%d", i), "")
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, domain.ErrReplyQuota) {
				t.Errorf("QueueMessage() error = %v", err)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 5 {
		t.Fatalf("successful replies = %d, want 5", successes.Load())
	}
}

func TestMemoryOutboxAvailabilityAndCopies(t *testing.T) {
	m, account, _, inbound := memoryFixture(t)
	ctx := context.Background()
	if _, err := m.ReceiveInbound(ctx, inbound, account); err != nil {
		t.Fatal(err)
	}
	events, err := m.ClaimOutbox(ctx, m.now().Add(-time.Second))
	if err != nil || len(events) != 0 {
		t.Fatalf("early ClaimOutbox() = %d, %v", len(events), err)
	}
	events, err = m.ClaimOutbox(ctx, m.now())
	if err != nil || len(events) != 1 {
		t.Fatalf("ClaimOutbox() = %d, %v", len(events), err)
	}
	original := string(events[0].Payload)
	events[0].Payload[0] = '['
	events[0].Type = "mutated"
	claimed, err := m.ClaimOutbox(ctx, m.now())
	if err != nil || len(claimed) != 0 {
		t.Fatalf("claimed ClaimOutbox() = %d, %v", len(claimed), err)
	}
	again, err := m.ClaimOutbox(ctx, m.now().Add(31*time.Second))
	if err != nil || len(again) != 1 {
		t.Fatalf("expired claim ClaimOutbox() = %d, %v", len(again), err)
	}
	if again[0].Type != "MessageReceived" || string(again[0].Payload) != original {
		t.Fatalf("stored event was mutated: %+v", again[0])
	}
	if err := m.ReleaseOutbox(ctx, again[0].ID, m.now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	events, err = m.ClaimOutbox(ctx, m.now())
	if err != nil || len(events) != 0 {
		t.Fatalf("deferred ClaimOutbox() = %d, %v", len(events), err)
	}
	if err := m.MarkOutboxPublished(ctx, again[0].ID); err != nil {
		t.Fatal(err)
	}
	events, err = m.ClaimOutbox(ctx, m.now().Add(2*time.Hour))
	if err != nil || len(events) != 0 {
		t.Fatalf("published ClaimOutbox() = %d, %v", len(events), err)
	}
}

func TestMemoryReturnedValuesAreIndependent(t *testing.T) {
	m, account, _, inbound := memoryFixture(t)
	ctx := context.Background()
	if _, err := m.ReceiveInbound(ctx, inbound, account); err != nil {
		t.Fatal(err)
	}
	conversation := onlyConversation(t, m)
	customer, err := m.Customer(ctx, conversation.CustomerID)
	if err != nil {
		t.Fatal(err)
	}
	customer.Tags = append(customer.Tags, "vip")
	if err := m.UpdateCustomer(ctx, customer); err != nil {
		t.Fatal(err)
	}
	customer.Tags[0] = "mutated"
	storedCustomer, err := m.Customer(ctx, customer.ID)
	if err != nil || storedCustomer.Tags[0] != "vip" {
		t.Fatalf("stored customer = %+v, %v", storedCustomer, err)
	}
	messages, err := m.Messages(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	messages[0].Content[0] = '['
	storedMessage, err := m.Message(ctx, messages[0].ID)
	if err != nil || storedMessage.Content[0] != '{' {
		t.Fatalf("stored message content = %s, %v", storedMessage.Content, err)
	}
	next := m.now().Add(time.Minute)
	storedMessage.Status = domain.StatusRetrying
	storedMessage.NextRetryAt = &next
	if err := m.UpdateMessage(ctx, storedMessage); err != nil {
		t.Fatal(err)
	}
	storedMessage.NextRetryAt = nil
	retryable, err := m.RetryableMessages(ctx, m.now())
	if err != nil || len(retryable) != 0 {
		t.Fatalf("early RetryableMessages() = %d, %v", len(retryable), err)
	}
	retryable, err = m.RetryableMessages(ctx, next)
	if err != nil || len(retryable) != 1 {
		t.Fatalf("RetryableMessages() = %d, %v", len(retryable), err)
	}
}

func TestMemoryDueRetryClaimAndNotificationOwnership(t *testing.T) {
	m, account, agent, inbound := memoryFixture(t)
	ctx := context.Background()
	if _, err := m.ReceiveInbound(ctx, inbound, account); err != nil {
		t.Fatal(err)
	}
	conversation := onlyConversation(t, m)
	if _, err := m.ClaimConversation(ctx, conversation.ID, agent); err != nil {
		t.Fatal(err)
	}
	message, err := m.QueueMessage(ctx, conversation.ID, agent, "text", "reply", "")
	if err != nil {
		t.Fatal(err)
	}
	message.Status = domain.StatusRetrying
	next := m.now().Add(-time.Minute)
	message.NextRetryAt = &next
	if err = m.UpdateMessage(ctx, message); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wait sync.WaitGroup
	for i := 0; i < 16; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, claimed, claimErr := m.ClaimMessage(ctx, message.ID, m.now())
			if claimErr != nil {
				t.Error(claimErr)
			}
			if claimed {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("retry claims = %d, want 1", successes.Load())
	}
	var owners sync.Map
	var reserved atomic.Int32
	for i := 0; i < 16; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			owner, claimed, claimErr := m.ReserveNotification(ctx, conversation.ID, agent.ID, "new_conversation", m.now())
			if claimErr != nil {
				t.Error(claimErr)
			}
			if claimed {
				owners.Store(owner, true)
				reserved.Add(1)
			}
		}()
	}
	wait.Wait()
	if reserved.Load() != 1 {
		t.Fatalf("notification reservations = %d, want 1", reserved.Load())
	}
	owner, _, _ := m.ReserveNotification(ctx, conversation.ID, agent.ID, "other", m.now())
	if _, ok, _ := m.ReserveNotification(ctx, conversation.ID, agent.ID, "other", m.now()); ok {
		t.Fatal("active notification was reserved twice")
	}
	newOwner, claimed, err := m.ReserveNotification(ctx, conversation.ID, agent.ID, "other", m.now().Add(31*time.Second))
	if err != nil || !claimed || newOwner == owner {
		t.Fatalf("expired notification claim = %q, %v, %v", newOwner, claimed, err)
	}
	if err = m.CompleteNotification(ctx, conversation.ID, agent.ID, "other", "stale-owner"); err != nil {
		t.Fatal(err)
	}
	if _, claimed, _ := m.ReserveNotification(ctx, conversation.ID, agent.ID, "other", m.now()); claimed {
		t.Fatal("stale owner released the notification")
	}
	if err = m.CompleteNotification(ctx, conversation.ID, agent.ID, "other", newOwner); err != nil {
		t.Fatal(err)
	}
	if _, claimed, _ := m.ReserveNotification(ctx, conversation.ID, agent.ID, "other", m.now().Add(32*time.Second)); claimed {
		t.Fatal("completed notification was reserved again")
	}
}

func TestMemoryTransferCloseAndAudit(t *testing.T) {
	m, account, agent, inbound := memoryFixture(t)
	ctx := context.Background()
	if _, err := m.ReceiveInbound(ctx, inbound, account); err != nil {
		t.Fatal(err)
	}
	conversation := onlyConversation(t, m)
	if _, err := m.TransferConversation(ctx, conversation.ID, agent.ID, "team-1", agent); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("unassigned agent TransferConversation() error = %v", err)
	}
	supervisor := domain.Agent{ID: "supervisor", Role: domain.RoleSupervisor, Active: true}
	if _, err := m.TransferConversation(ctx, conversation.ID, "missing", "team-1", supervisor); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("missing target TransferConversation() error = %v", err)
	}
	transferred, err := m.TransferConversation(ctx, conversation.ID, agent.ID, "team-1", supervisor)
	if err != nil {
		t.Fatal(err)
	}
	if transferred.AssigneeID != agent.ID || transferred.TeamID != "team-1" || transferred.Status != domain.StatusOpen {
		t.Fatalf("transferred conversation = %+v", transferred)
	}
	if _, err := m.CloseConversation(ctx, transferred.ID, domain.Agent{ID: "other", Role: domain.RoleAgent}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("other agent CloseConversation() error = %v", err)
	}
	closed, err := m.CloseConversation(ctx, transferred.ID, agent)
	if err != nil || closed.Status != domain.StatusClosed || closed.Version != 2 {
		t.Fatalf("CloseConversation() = %+v, %v", closed, err)
	}
	audits, err := m.Audits(ctx)
	if err != nil || len(audits) != 2 {
		t.Fatalf("Audits() = %d, %v; want transfer and close", len(audits), err)
	}
	actions := map[string]bool{}
	for _, audit := range audits {
		actions[audit.Action] = true
	}
	if !actions["conversation.transfer"] || !actions["conversation.close"] {
		t.Fatalf("audit actions = %+v", actions)
	}
}

func TestMemoryCommentsAreIndependentAndUpdatable(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	comment := domain.Comment{
		ID: "comment-record", ChannelAccountID: "account-1", MsgDataID: "article-1", ArticleIndex: 1,
		CommentID: "external-comment", AuthorID: "author", Content: "question", Status: "pending",
		SyncedAt: time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC),
	}
	if err := m.UpsertComment(ctx, comment); err != nil {
		t.Fatal(err)
	}
	comment.Content = "updated question"
	if err := m.UpsertComment(ctx, comment); err != nil {
		t.Fatal(err)
	}
	comments, err := m.Comments(ctx, "account-1", "pending")
	if err != nil || len(comments) != 1 {
		t.Fatalf("Comments() = %d, %v", len(comments), err)
	}
	comment.Status, comment.ReplyContent = "replied", "answer"
	if err := m.UpdateComment(ctx, comment); err != nil {
		t.Fatal(err)
	}
	stored, err := m.Comment(ctx, comment.ID)
	if err != nil || stored.Content != "updated question" || stored.Status != "replied" || stored.ReplyContent != "answer" {
		t.Fatalf("Comment() = %+v, %v", stored, err)
	}
	comments, err = m.Comments(ctx, "", "")
	if err != nil || len(comments) != 1 {
		t.Fatalf("all Comments() = %d, %v", len(comments), err)
	}
	if conversations, err := m.Conversations(ctx, ConversationFilter{}); err != nil || len(conversations) != 0 {
		t.Fatalf("comment created conversations = %d, %v", len(conversations), err)
	}
}

func TestMemoryTicketsVersionEventsAndAudit(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	createdAt := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return createdAt.Add(time.Hour) }
	ticket := domain.Ticket{
		ID: "ticket-1", CustomerID: "customer-1", ConversationID: "conversation-1", CommentID: "comment-1",
		Title: "help", Status: "open", Priority: "normal", CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if err := m.CreateTicket(ctx, ticket, "actor-1"); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateTicket(ctx, ticket, "actor-1"); !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("duplicate CreateTicket() error = %v", err)
	}
	ticket.Status, ticket.Priority, ticket.AssigneeID = "resolved", "high", "agent-1"
	if err := m.UpdateTicket(ctx, ticket, "actor-2"); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateTicket(ctx, ticket, "actor-2"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale UpdateTicket() error = %v", err)
	}
	stored, err := m.Ticket(ctx, ticket.ID)
	if err != nil || stored.Version != 1 || stored.Status != "resolved" {
		t.Fatalf("Ticket() = %+v, %v", stored, err)
	}
	tickets, err := m.TicketsByConversation(ctx, ticket.ConversationID)
	if err != nil || len(tickets) != 1 {
		t.Fatalf("TicketsByConversation() = %d, %v", len(tickets), err)
	}
	events, err := m.TicketEvents(ctx, ticket.ID)
	if err != nil || len(events) != 2 || events[0].Type != "created" || events[1].Type != "updated" {
		t.Fatalf("TicketEvents() = %+v, %v", events, err)
	}
	audits, err := m.Audits(ctx)
	if err != nil || len(audits) != 1 || audits[0].Action != "ticket.create" {
		t.Fatalf("Audits() = %+v, %v", audits, err)
	}
}

func TestMemorySLAAndReport(t *testing.T) {
	m, account, _, inbound := memoryFixture(t)
	ctx := context.Background()
	if _, err := m.ReceiveInbound(ctx, inbound, account); err != nil {
		t.Fatal(err)
	}
	conversation := onlyConversation(t, m)
	expired, err := m.ExpiredConversations(ctx, conversation.ReplyDeadlineAt.Add(-time.Nanosecond))
	if err != nil || len(expired) != 0 {
		t.Fatalf("early ExpiredConversations() = %d, %v", len(expired), err)
	}
	expired, err = m.ExpiredConversations(ctx, conversation.ReplyDeadlineAt)
	if err != nil || len(expired) != 1 {
		t.Fatalf("ExpiredConversations() = %d, %v", len(expired), err)
	}
	for i := 0; i < 2; i++ {
		recorded, err := m.RecordSLA(ctx, "conversation", conversation.ID, "expired")
		if err != nil || recorded != (i == 0) {
			t.Fatalf("RecordSLA() %d = %v, %v", i, recorded, err)
		}
	}
	recorded, err := m.RecordSLA(ctx, "conversation", conversation.ID, "warning")
	if err != nil || !recorded {
		t.Fatalf("different SLA kind = %v, %v", recorded, err)
	}
	escalated, err := m.Conversation(ctx, conversation.ID)
	if err != nil || escalated.SLAEscalatedAt == nil {
		t.Fatalf("escalated conversation = %+v, %v", escalated, err)
	}
	report, err := m.Report(ctx)
	if err != nil || report.NewConversations != 1 || report.SLAExpired != 1 || report.ClosedConversations != 0 {
		t.Fatalf("Report() = %+v, %v", report, err)
	}
}

func TestMemoryRoutingFixturesAndMissingRecords(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	agent := domain.Agent{ID: "agent-1", DingTalkUserID: "ding-1", Active: true}
	if err := m.SaveAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveTeamAgent(ctx, "team-1", agent.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveTeamAgent(ctx, "team-1", agent.ID); err != nil {
		t.Fatal(err)
	}
	agents, err := m.TeamAgents(ctx, "team-1")
	if err != nil || len(agents) != 1 {
		t.Fatalf("TeamAgents() = %d, %v", len(agents), err)
	}
	rule := domain.RoutingRule{ID: "rule-1", Priority: 2, TeamID: "team-1", Active: true}
	if err := m.SaveRoutingRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveRoutingRule(ctx, domain.RoutingRule{ID: "rule-2", Priority: 3, TeamID: "team-2", Active: true}); err != nil {
		t.Fatal(err)
	}
	rules, err := m.RoutingRules(ctx)
	if err != nil || len(rules) != 2 || rules[0].ID != "rule-2" {
		t.Fatalf("RoutingRules() = %+v, %v", rules, err)
	}
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	if err := m.SaveShift(ctx, domain.Shift{ID: "shift-1", AgentID: agent.ID, TeamID: "team-1", StartAt: now, EndAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	shifts, err := m.Shifts(ctx)
	if err != nil || len(shifts) != 1 {
		t.Fatalf("Shifts() = %d, %v", len(shifts), err)
	}
	if _, err := m.Conversation(ctx, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing Conversation() error = %v", err)
	}
	if err := m.UpdateComment(ctx, domain.Comment{ID: "missing"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing UpdateComment() error = %v", err)
	}
}
