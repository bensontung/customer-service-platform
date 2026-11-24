package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/go-sql-driver/mysql"
)

func integrationMySQL(t *testing.T) *MySQL {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		if os.Getenv("REQUIRE_INTEGRATION") == "1" {
			t.Fatal("TEST_MYSQL_DSN is required by REQUIRE_INTEGRATION=1")
		}
		t.Skip("set TEST_MYSQL_DSN to run isolated MySQL integration tests")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MultiStatements = true
	repository, err := OpenMySQL(config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := filepath.Glob(filepath.Join("..", "..", "deploy", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(migrations)
	for _, migration := range migrations {
		version := 0
		if _, err = fmt.Sscanf(filepath.Base(migration), "%d_", &version); err != nil {
			t.Fatalf("parse migration %s: %v", migration, err)
		}
		var applied int
		if err = repository.DB().QueryRow(`SELECT COUNT(*) FROM schema_migration WHERE version=?`, version).Scan(&applied); err != nil || applied != 1 {
			t.Fatalf("migration %d is not applied: %v; use make test-integration", version, err)
		}
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository
}

func TestMySQLCoreAndConcurrentClaims(t *testing.T) {
	repository := integrationMySQL(t)
	ctx := context.Background()
	account := domain.ChannelAccount{AccountKey: "integration-" + domain.NewID(), Channel: domain.ChannelService, AppID: "wx-integration-" + domain.NewID(), TokenSecretRef: "token", AESKeySecretRef: "aes", AppSecretRef: "secret", Status: "active"}
	account.ID = domain.NewID()
	if err := repository.SaveAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	inbound := domain.Inbound{EventID: "message-" + domain.NewID(), OpenID: "openid-" + domain.NewID(), Type: "text", Content: "hello", ReceivedAt: time.Now().UTC()}
	created, err := repository.ReceiveInbound(ctx, inbound, account)
	if err != nil || !created {
		t.Fatalf("ReceiveInbound() = %v, %v", created, err)
	}
	created, err = repository.ReceiveInbound(ctx, inbound, account)
	if err != nil || created {
		t.Fatalf("duplicate ReceiveInbound() = %v, %v", created, err)
	}
	conversations, err := repository.Conversations(ctx, ConversationFilter{})
	if err != nil || len(conversations) == 0 {
		t.Fatalf("Conversations() = %d, %v", len(conversations), err)
	}
	conversation := conversations[0]
	for _, candidate := range conversations {
		if candidate.ChannelAccountID == account.ID {
			conversation = candidate
			break
		}
	}
	if conversation.ChannelAccountID != account.ID {
		t.Fatal("integration conversation not found")
	}
	customer, err := repository.Customer(ctx, conversation.CustomerID)
	if err != nil || customer.Note != "" {
		t.Fatalf("nullable note scan: %#v, %v", customer, err)
	}
	events, err := repository.ClaimOutbox(ctx, time.Now().UTC())
	if err != nil || len(events) == 0 {
		t.Fatalf("ClaimOutbox() = %d, %v", len(events), err)
	}
	var eventID string
	for _, event := range events {
		if event.AggregateID == conversation.ID {
			eventID = event.ID
		}
	}
	if eventID == "" {
		t.Fatal("inbound outbox event not found")
	}
	claimedAgain, err := repository.ClaimOutbox(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range claimedAgain {
		if event.ID == eventID {
			t.Fatal("outbox event claimed twice concurrently")
		}
	}
	if err = repository.MarkOutboxPublished(ctx, eventID); err != nil {
		t.Fatal(err)
	}
	agent := domain.Agent{ID: domain.NewID(), DingTalkUserID: "ding-" + domain.NewID(), Name: "Integration", Role: domain.RoleAgent, Active: true}
	if err = repository.SaveAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	conversation, err = repository.ClaimConversation(ctx, conversation.ID, agent)
	if err != nil {
		t.Fatal(err)
	}
	message, err := repository.QueueMessage(ctx, conversation.ID, agent, "text", "reply", "")
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, claimed, claimErr := repository.ClaimMessage(ctx, message.ID, time.Now().UTC())
			if claimErr != nil && !errors.Is(claimErr, context.Canceled) {
				t.Error(claimErr)
			}
			if claimed {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("message claimed %d times, want 1", successes.Load())
	}
	message.Status = domain.StatusRetrying
	next := time.Now().UTC().Add(-time.Minute)
	message.NextRetryAt = &next
	if err = repository.UpdateMessage(ctx, message); err != nil {
		t.Fatal(err)
	}
	successes.Store(0)
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, claimed, claimErr := repository.ClaimMessage(ctx, message.ID, time.Now().UTC())
			if claimErr != nil && !errors.Is(claimErr, context.Canceled) {
				t.Error(claimErr)
			}
			if claimed {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("due retry claimed %d times, want 1", successes.Load())
	}
	message.Status = domain.StatusSent
	if err = repository.UpdateMessage(ctx, message); err != nil {
		t.Fatal(err)
	}
	successes.Store(0)
	var owner string
	var ownerMu sync.Mutex
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			claimOwner, claimed, claimErr := repository.ReserveNotification(ctx, conversation.ID, agent.ID, "new_conversation", time.Now().UTC())
			if claimErr != nil && !errors.Is(claimErr, context.Canceled) {
				t.Error(claimErr)
			}
			if claimed {
				successes.Add(1)
				ownerMu.Lock()
				owner = claimOwner
				ownerMu.Unlock()
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("notification reserved %d times, want 1", successes.Load())
	}
	nextOwner, claimed, err := repository.ReserveNotification(ctx, conversation.ID, agent.ID, "new_conversation", time.Now().UTC().Add(31*time.Second))
	if err != nil || !claimed || nextOwner == owner {
		t.Fatalf("expired notification claim = %q, %v, %v", nextOwner, claimed, err)
	}
	if err = repository.CompleteNotification(ctx, conversation.ID, agent.ID, "new_conversation", owner); err != nil {
		t.Fatal(err)
	}
	var status, claimToken string
	if err = repository.DB().QueryRowContext(ctx, `SELECT status,claim_token FROM dingtalk_delivery WHERE conversation_id=? AND agent_id=? AND kind=?`, conversation.ID, agent.ID, "new_conversation").Scan(&status, &claimToken); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || claimToken != nextOwner {
		t.Fatalf("stale completion changed notification to %s with owner %s", status, claimToken)
	}
	owner = nextOwner
	if err = repository.CompleteNotification(ctx, conversation.ID, agent.ID, "new_conversation", owner); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := repository.ReserveNotification(ctx, conversation.ID, agent.ID, "new_conversation", time.Now().UTC()); err != nil || claimed {
		t.Fatalf("completed notification claimed = %v, %v", claimed, err)
	}
}
