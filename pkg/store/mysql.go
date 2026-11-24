package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
	"github.com/go-sql-driver/mysql"
)

type MySQL struct{ db *sql.DB }

func NewMySQL(db *sql.DB) *MySQL { return &MySQL{db: db} }

func OpenMySQL(dsn string) (*MySQL, error) {
	if dsn == "" {
		return nil, fmt.Errorf("MYSQL_DSN is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect MySQL: %w", err)
	}
	return &MySQL{db: db}, nil
}

func (m *MySQL) Close() error { return m.db.Close() }
func (m *MySQL) DB() *sql.DB  { return m.db }

func (m *MySQL) AccountByKey(ctx context.Context, key string) (a domain.ChannelAccount, err error) {
	err = m.db.QueryRowContext(ctx, `SELECT id,account_key,channel,app_id,token_secret_ref,aes_key_secret_ref,app_secret_ref,status FROM channel_account WHERE account_key=? AND status='active'`, key).Scan(&a.ID, &a.AccountKey, &a.Channel, &a.AppID, &a.TokenSecretRef, &a.AESKeySecretRef, &a.AppSecretRef, &a.Status)
	return a, mapSQLError(err)
}

func (m *MySQL) AccountByID(ctx context.Context, id string) (a domain.ChannelAccount, err error) {
	err = m.db.QueryRowContext(ctx, `SELECT id,account_key,channel,app_id,token_secret_ref,aes_key_secret_ref,app_secret_ref,status FROM channel_account WHERE id=?`, id).Scan(&a.ID, &a.AccountKey, &a.Channel, &a.AppID, &a.TokenSecretRef, &a.AESKeySecretRef, &a.AppSecretRef, &a.Status)
	return a, mapSQLError(err)
}

func (m *MySQL) SaveAccount(ctx context.Context, a domain.ChannelAccount) error {
	_, err := m.db.ExecContext(ctx, `INSERT INTO channel_account(id,account_key,channel,app_id,token_secret_ref,aes_key_secret_ref,app_secret_ref,status,created_at) VALUES(?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE token_secret_ref=VALUES(token_secret_ref),aes_key_secret_ref=VALUES(aes_key_secret_ref),app_secret_ref=VALUES(app_secret_ref),status=VALUES(status)`, a.ID, a.AccountKey, a.Channel, a.AppID, a.TokenSecretRef, a.AESKeySecretRef, a.AppSecretRef, a.Status, time.Now().UTC())
	return err
}

func (m *MySQL) ReceiveInbound(ctx context.Context, in domain.Inbound, a domain.ChannelAccount) (bool, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `INSERT INTO inbox_dedup(source,event_id,payload_hash,received_at) VALUES(?,?,?,?)`, "wechat:"+a.ID, in.EventID, in.PayloadHash, time.Now().UTC())
	if duplicate(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return false, nil
	}
	customerID := domain.NewID()
	if _, err = tx.ExecContext(ctx, `INSERT INTO external_customer(id,channel,app_id,openid,tags_json,note,created_at,updated_at) VALUES(?,?,?,?,?,'',?,?) ON DUPLICATE KEY UPDATE id=id`, customerID, a.Channel, a.AppID, in.OpenID, `[]`, time.Now().UTC(), time.Now().UTC()); err != nil {
		return false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM external_customer WHERE channel=? AND app_id=? AND openid=?`, a.Channel, a.AppID, in.OpenID).Scan(&customerID); err != nil {
		return false, err
	}
	conversationID := domain.NewID()
	deadline := domain.NewReplyWindow(in.ReceivedAt)
	_, err = tx.ExecContext(ctx, `INSERT INTO conversation(id,source,channel_account_id,customer_id,status,last_inbound_at,reply_deadline_at,created_at,updated_at) SELECT ?,?,?,?,?,?,?,?,? FROM DUAL WHERE NOT EXISTS(SELECT 1 FROM conversation WHERE customer_id=? AND channel_account_id=? AND status<>'closed')`, conversationID, domain.SourcePrivate, a.ID, customerID, domain.StatusWaiting, in.ReceivedAt, deadline, time.Now().UTC(), time.Now().UTC(), customerID, a.ID)
	if err != nil {
		return false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT id,status FROM conversation WHERE customer_id=? AND channel_account_id=? AND status<>'closed' ORDER BY created_at DESC LIMIT 1`, customerID, a.ID).Scan(&conversationID, new(string)); err != nil {
		return false, err
	}
	messageID := domain.NewID()
	content := JSON(map[string]string{"text": in.Content, "mediaId": in.MediaID})
	if _, err = tx.ExecContext(ctx, `INSERT INTO message(id,conversation_id,channel_account_id,channel,direction,channel_msg_id,msg_type,content_json,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, messageID, conversationID, a.ID, a.Channel, "inbound", in.EventID, in.Type, content, "received", time.Now().UTC(), time.Now().UTC()); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE conversation SET last_inbound_at=?,reply_deadline_at=?,replies_in_window=0,updated_at=? WHERE id=?`, in.ReceivedAt, deadline, time.Now().UTC(), conversationID); err != nil {
		return false, err
	}
	event := domain.OutboxEvent{ID: domain.NewID(), Type: "MessageReceived", AggregateID: conversationID, Payload: JSON(map[string]string{"messageId": messageID, "conversationId": conversationID})}
	if err = insertOutbox(ctx, tx, event); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func insertOutbox(ctx context.Context, tx *sql.Tx, event domain.OutboxEvent) error {
	now := time.Now().UTC()
	_, err := tx.ExecContext(ctx, `INSERT INTO outbox_event(id,event_type,aggregate_id,payload_json,available_at,created_at) VALUES(?,?,?,?,?,?)`, event.ID, event.Type, event.AggregateID, event.Payload, now, now)
	return err
}

func (m *MySQL) AgentByID(ctx context.Context, id string) (a domain.Agent, err error) {
	err = m.db.QueryRowContext(ctx, `SELECT id,dingtalk_user_id,name,role,active FROM agent WHERE id=?`, id).Scan(&a.ID, &a.DingTalkUserID, &a.Name, &a.Role, &a.Active)
	return a, mapSQLError(err)
}

func (m *MySQL) AgentByDingTalkID(ctx context.Context, id string) (a domain.Agent, err error) {
	err = m.db.QueryRowContext(ctx, `SELECT id,dingtalk_user_id,name,role,active FROM agent WHERE dingtalk_user_id=? AND active=true`, id).Scan(&a.ID, &a.DingTalkUserID, &a.Name, &a.Role, &a.Active)
	return a, mapSQLError(err)
}

func (m *MySQL) SaveAgent(ctx context.Context, a domain.Agent) error {
	_, err := m.db.ExecContext(ctx, `INSERT INTO agent(id,dingtalk_user_id,name,role,active,created_at) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE name=VALUES(name),role=VALUES(role),active=VALUES(active)`, a.ID, a.DingTalkUserID, a.Name, a.Role, a.Active, time.Now().UTC())
	return err
}

func (m *MySQL) Conversation(ctx context.Context, id string) (domain.Conversation, error) {
	return scanConversation(m.db.QueryRowContext(ctx, conversationSelect+` WHERE id=?`, id))
}

const conversationSelect = `SELECT id,source,channel_account_id,customer_id,status,assignee_id,team_id,version,last_inbound_at,reply_deadline_at,replies_in_window,first_response_at,sla_escalated_at,created_at,updated_at FROM conversation`

func scanConversation(row interface{ Scan(...any) error }) (c domain.Conversation, err error) {
	err = row.Scan(&c.ID, &c.Source, &c.ChannelAccountID, &c.CustomerID, &c.Status, &c.AssigneeID, &c.TeamID, &c.Version, &c.LastInboundAt, &c.ReplyDeadlineAt, &c.RepliesInWindow, &c.FirstResponseAt, &c.SLAEscalatedAt, &c.CreatedAt, &c.UpdatedAt)
	return c, mapSQLError(err)
}

func (m *MySQL) Conversations(ctx context.Context, filter ConversationFilter) ([]domain.Conversation, error) {
	query := conversationSelect + ` WHERE 1=1`
	args := make([]any, 0, 3)
	for _, item := range []struct{ column, value string }{{"status", filter.Status}, {"source", filter.Source}, {"assignee_id", filter.Assignee}} {
		if item.value != "" {
			query += ` AND ` + item.column + `=?`
			args = append(args, item.value)
		}
	}
	rows, err := m.db.QueryContext(ctx, query+` ORDER BY last_inbound_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []domain.Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (m *MySQL) Messages(ctx context.Context, id string) ([]domain.Message, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,conversation_id,channel_account_id,channel,direction,channel_msg_id,msg_type,content_json,status,error_code,error_message,retry_count,next_retry_at,created_at,updated_at FROM message WHERE conversation_id=? ORDER BY created_at ASC LIMIT 1000`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []domain.Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, rows.Err()
}

func scanMessage(row interface{ Scan(...any) error }) (message domain.Message, err error) {
	var conversationID sql.NullString
	err = row.Scan(&message.ID, &conversationID, &message.ChannelAccountID, &message.Channel, &message.Direction, &message.ChannelMessageID, &message.Type, &message.Content, &message.Status, &message.ErrorCode, &message.ErrorMessage, &message.RetryCount, &message.NextRetryAt, &message.CreatedAt, &message.UpdatedAt)
	message.ConversationID = conversationID.String
	return message, mapSQLError(err)
}

func (m *MySQL) Customer(ctx context.Context, id string) (domain.Customer, error) {
	var c domain.Customer
	var tags []byte
	err := m.db.QueryRowContext(ctx, `SELECT id,channel,app_id,openid,unionid,nickname,tags_json,note FROM external_customer WHERE id=?`, id).Scan(&c.ID, &c.Channel, &c.AppID, &c.OpenID, &c.UnionID, &c.Nickname, &tags, &c.Note)
	if err == nil {
		err = json.Unmarshal(tags, &c.Tags)
	}
	return c, mapSQLError(err)
}

func (m *MySQL) UpdateCustomer(ctx context.Context, c domain.Customer) error {
	result, err := m.db.ExecContext(ctx, `UPDATE external_customer SET nickname=?,note=?,tags_json=?,updated_at=? WHERE id=?`, c.Nickname, c.Note, JSON(c.Tags), time.Now().UTC(), c.ID)
	return changed(result, err)
}

func (m *MySQL) ClaimConversation(ctx context.Context, id string, agent domain.Agent) (domain.Conversation, error) {
	result, err := m.db.ExecContext(ctx, `UPDATE conversation SET assignee_id=?,status=?,version=version+1,updated_at=? WHERE id=? AND status='waiting' AND assignee_id=''`, agent.ID, domain.StatusOpen, time.Now().UTC(), id)
	if err != nil {
		return domain.Conversation{}, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return domain.Conversation{}, domain.ErrConflict
	}
	return m.Conversation(ctx, id)
}

func (m *MySQL) TransferConversation(ctx context.Context, id, assigneeID, teamID string, agent domain.Agent) (domain.Conversation, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Conversation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	c, err := scanConversation(tx.QueryRowContext(ctx, conversationSelect+` WHERE id=? FOR UPDATE`, id))
	if err != nil {
		return c, err
	}
	if !domain.CanAccess(c, agent) || (c.AssigneeID == "" && agent.Role == domain.RoleAgent) {
		return c, domain.ErrForbidden
	}
	if assigneeID != "" {
		target, err := m.AgentByID(ctx, assigneeID)
		if err != nil || !target.Active {
			return c, domain.ErrInvalidRequest
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE conversation SET assignee_id=?,team_id=?,status=?,version=version+1,updated_at=? WHERE id=? AND version=?`, assigneeID, teamID, domain.StatusOpen, time.Now().UTC(), id, c.Version)
	if err = changed(result, err); err != nil {
		return c, domain.ErrConflict
	}
	if err = insertAuditTx(ctx, tx, agent.ID, "conversation.transfer", "conversation", id, JSON(map[string]string{"assigneeId": assigneeID, "teamId": teamID})); err != nil {
		return c, err
	}
	if err = insertOutbox(ctx, tx, domain.OutboxEvent{ID: domain.NewID(), Type: "ConversationTransferred", AggregateID: id, Payload: JSON(map[string]string{"agentId": assigneeID})}); err != nil {
		return c, err
	}
	if err = tx.Commit(); err != nil {
		return c, err
	}
	return m.Conversation(ctx, id)
}

func (m *MySQL) AssignConversation(ctx context.Context, id, teamID, agentID string) (domain.Conversation, error) {
	result, err := m.db.ExecContext(ctx, `UPDATE conversation SET team_id=?,assignee_id=?,status='open',version=version+1,updated_at=? WHERE id=? AND assignee_id='' AND status='waiting'`, teamID, agentID, time.Now().UTC(), id)
	if err != nil {
		return domain.Conversation{}, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return m.Conversation(ctx, id)
	}
	return m.Conversation(ctx, id)
}

func (m *MySQL) CloseConversation(ctx context.Context, id string, agent domain.Agent) (domain.Conversation, error) {
	c, err := m.Conversation(ctx, id)
	if err != nil {
		return c, err
	}
	if c.AssigneeID != agent.ID && agent.Role == domain.RoleAgent {
		return c, domain.ErrForbidden
	}
	result, err := m.db.ExecContext(ctx, `UPDATE conversation SET status='closed',version=version+1,updated_at=? WHERE id=? AND version=?`, time.Now().UTC(), id, c.Version)
	if err = changed(result, err); err != nil {
		return c, domain.ErrConflict
	}
	return c, m.insertAudit(ctx, agent.ID, "conversation.close", "conversation", id, JSON(map[string]string{}))
}

func (m *MySQL) QueueMessage(ctx context.Context, id string, agent domain.Agent, kind, text, mediaID string, keys ...string) (domain.Message, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Message{}, err
	}
	defer func() { _ = tx.Rollback() }()
	c, err := scanConversation(tx.QueryRowContext(ctx, conversationSelect+` WHERE id=? FOR UPDATE`, id))
	if err != nil {
		return domain.Message{}, err
	}
	if err = domain.CanReply(c, agent, time.Now().UTC()); err != nil {
		return domain.Message{}, err
	}
	if kind == "text" && (strings.TrimSpace(text) == "" || len(text) > 2048) {
		return domain.Message{}, domain.ErrInvalidRequest
	}
	if kind != "text" && kind != "image" && kind != "voice" {
		return domain.Message{}, domain.ErrInvalidRequest
	}
	if kind != "text" && mediaID == "" {
		return domain.Message{}, domain.ErrInvalidRequest
	}
	externalID := "local:" + domain.NewID()
	if len(keys) > 0 && keys[0] != "" {
		externalID = keys[0]
		existing, lookupErr := m.MessageByExternalID(ctx, c.ChannelAccountID, externalID)
		if lookupErr == nil {
			if existing.ConversationID != id {
				return domain.Message{}, domain.ErrConflict
			}
			return existing, nil
		}
	}
	message := domain.Message{ID: domain.NewID(), ConversationID: id, ChannelAccountID: c.ChannelAccountID, Direction: "outbound", ChannelMessageID: externalID, Type: kind, Status: domain.StatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	message.Content = JSON(map[string]string{"text": text, "mediaId": mediaID})
	if _, err = tx.ExecContext(ctx, `INSERT INTO message(id,conversation_id,channel_account_id,channel,direction,channel_msg_id,msg_type,content_json,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, message.ID, id, c.ChannelAccountID, "pending", message.Direction, message.ChannelMessageID, kind, message.Content, message.Status, message.CreatedAt, message.UpdatedAt); err != nil {
		return message, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE conversation SET replies_in_window=replies_in_window+1,version=version+1,updated_at=? WHERE id=?`, time.Now().UTC(), id); err != nil {
		return message, err
	}
	event := domain.OutboxEvent{ID: domain.NewID(), Type: "MessageSendRequested", AggregateID: message.ID, Payload: JSON(map[string]string{"messageId": message.ID})}
	if err = insertOutbox(ctx, tx, event); err != nil {
		return message, err
	}
	if err = tx.Commit(); err != nil {
		return message, err
	}
	account, err := m.AccountByID(ctx, c.ChannelAccountID)
	if err == nil {
		message.Channel = account.Channel
	}
	return message, err
}

func (m *MySQL) MessageByExternalID(ctx context.Context, accountID, externalID string) (domain.Message, error) {
	return scanMessage(m.db.QueryRowContext(ctx, `SELECT id,conversation_id,channel_account_id,channel,direction,channel_msg_id,msg_type,content_json,status,error_code,error_message,retry_count,next_retry_at,created_at,updated_at FROM message WHERE channel_account_id=? AND channel_msg_id=?`, accountID, externalID))
}
func (m *MySQL) Message(ctx context.Context, id string) (domain.Message, error) {
	return scanMessage(m.db.QueryRowContext(ctx, `SELECT id,conversation_id,channel_account_id,channel,direction,channel_msg_id,msg_type,content_json,status,error_code,error_message,retry_count,next_retry_at,created_at,updated_at FROM message WHERE id=?`, id))
}

func (m *MySQL) ClaimMessage(ctx context.Context, id string, now time.Time) (domain.Message, bool, error) {
	result, err := m.db.ExecContext(ctx, `UPDATE message SET claimed_until=? WHERE id=? AND status IN (?,?) AND (claimed_until IS NULL OR claimed_until<=?)`, now.Add(30*time.Second).UTC(), id, domain.StatusQueued, domain.StatusRetrying, now.UTC())
	if err != nil {
		return domain.Message{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domain.Message{}, false, err
	}
	if changed == 0 {
		return domain.Message{}, false, nil
	}
	message, err := m.Message(ctx, id)
	return message, true, err
}

func (m *MySQL) UpdateMessage(ctx context.Context, message domain.Message) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `UPDATE message SET channel=?,status=?,error_code=?,error_message=?,retry_count=?,next_retry_at=?,claimed_until=NULL,updated_at=? WHERE id=?`, message.Channel, message.Status, message.ErrorCode, message.ErrorMessage, message.RetryCount, message.NextRetryAt, time.Now().UTC(), message.ID)
	if err != nil {
		return err
	}
	if message.Status == domain.StatusSent && message.Direction == "outbound" {
		_, err = tx.ExecContext(ctx, `UPDATE conversation SET first_response_at=COALESCE(first_response_at,?),updated_at=? WHERE id=?`, time.Now().UTC(), time.Now().UTC(), message.ConversationID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (m *MySQL) RetryableMessages(ctx context.Context, now time.Time) ([]domain.Message, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,conversation_id,channel_account_id,channel,direction,channel_msg_id,msg_type,content_json,status,error_code,error_message,retry_count,next_retry_at,created_at,updated_at FROM message WHERE status=? AND next_retry_at<=? AND (claimed_until IS NULL OR claimed_until<=?) LIMIT 100`, domain.StatusRetrying, now.UTC(), now.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var messages []domain.Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (m *MySQL) ClaimOutbox(ctx context.Context, now time.Time) ([]domain.OutboxEvent, error) {
	connection, err := m.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = connection.Close() }()
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id,event_type,aggregate_id,payload_json,attempts FROM outbox_event WHERE published_at IS NULL AND available_at<=? AND (claimed_until IS NULL OR claimed_until<=?) ORDER BY created_at LIMIT 100 FOR UPDATE SKIP LOCKED`, now.UTC(), now.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var events []domain.OutboxEvent
	for rows.Next() {
		var event domain.OutboxEvent
		if err = rows.Scan(&event.ID, &event.Type, &event.AggregateID, &event.Payload, &event.Attempts); err != nil {
			_ = rows.Close()
			return nil, err
		}
		events = append(events, event)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	ids := make([]any, len(events))
	placeholders := make([]string, len(events))
	for i, event := range events {
		ids[i] = event.ID
		placeholders[i] = "?"
	}
	// #nosec G202 -- only fixed question-mark placeholders are joined; all values remain bound parameters.
	_, err = tx.ExecContext(ctx, `UPDATE outbox_event SET claimed_until=? WHERE id IN (`+strings.Join(placeholders, ",")+`) AND published_at IS NULL`, append([]any{now.Add(30 * time.Second).UTC()}, ids...)...)
	if err != nil {
		return nil, err
	}
	return events, tx.Commit()
}

func (m *MySQL) MarkOutboxPublished(ctx context.Context, id string) error {
	_, err := m.db.ExecContext(ctx, `UPDATE outbox_event SET published_at=?,claimed_until=NULL WHERE id=? AND published_at IS NULL`, time.Now().UTC(), id)
	return err
}
func (m *MySQL) ReleaseOutbox(ctx context.Context, id string, next time.Time) error {
	_, err := m.db.ExecContext(ctx, `UPDATE outbox_event SET attempts=attempts+1,available_at=?,claimed_until=NULL WHERE id=?`, next.UTC(), id)
	return err
}

func (m *MySQL) ReserveNotification(ctx context.Context, conversationID, agentID, kind string, now time.Time) (string, bool, error) {
	eventID := conversationID + ":" + agentID + ":" + kind
	now = now.UTC()
	owner := domain.NewID()
	result, err := m.db.ExecContext(ctx, `INSERT INTO dingtalk_delivery(id,conversation_id,agent_id,kind,out_track_id,status,claim_token,claimed_until,created_at,updated_at) VALUES(?,?,?,?,?,'pending',?,?,?,?) AS new ON DUPLICATE KEY UPDATE claim_token=IF(dingtalk_delivery.status='pending' AND (dingtalk_delivery.claimed_until IS NULL OR dingtalk_delivery.claimed_until<=new.created_at),new.claim_token,dingtalk_delivery.claim_token), claimed_until=IF(dingtalk_delivery.status='pending' AND (dingtalk_delivery.claimed_until IS NULL OR dingtalk_delivery.claimed_until<=new.created_at),new.claimed_until,dingtalk_delivery.claimed_until), updated_at=IF(dingtalk_delivery.status='pending' AND (dingtalk_delivery.claimed_until IS NULL OR dingtalk_delivery.claimed_until<=new.created_at),new.updated_at,dingtalk_delivery.updated_at)`, domain.NewID(), conversationID, agentID, kind, eventID, owner, now.Add(30*time.Second), now, now)
	if err != nil {
		return "", false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return "", false, err
	}
	if changed == 0 {
		return "", false, nil
	}
	var claimedToken string
	if err = m.db.QueryRowContext(ctx, `SELECT claim_token FROM dingtalk_delivery WHERE out_track_id=?`, eventID).Scan(&claimedToken); err != nil {
		return "", false, err
	}
	return owner, claimedToken == owner, nil
}
func (m *MySQL) CompleteNotification(ctx context.Context, conversationID, agentID, kind, owner string) error {
	_, err := m.db.ExecContext(ctx, `UPDATE dingtalk_delivery SET status='sent',claim_token='',claimed_until=NULL,updated_at=? WHERE out_track_id=? AND claim_token=?`, time.Now().UTC(), conversationID+":"+agentID+":"+kind, owner)
	return err
}

func (m *MySQL) Comment(ctx context.Context, id string) (c domain.Comment, err error) {
	err = m.db.QueryRowContext(ctx, `SELECT id,channel_account_id,msg_data_id,article_index,comment_id,author_id,content,status,reply_content,synced_at,updated_at FROM article_comment WHERE id=?`, id).Scan(&c.ID, &c.ChannelAccountID, &c.MsgDataID, &c.ArticleIndex, &c.CommentID, &c.AuthorID, &c.Content, &c.Status, &c.ReplyContent, &c.SyncedAt, &c.UpdatedAt)
	return c, mapSQLError(err)
}
func (m *MySQL) Comments(ctx context.Context, accountID, status string) ([]domain.Comment, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,channel_account_id,msg_data_id,article_index,comment_id,author_id,content,status,reply_content,synced_at,updated_at FROM article_comment WHERE (?='' OR channel_account_id=?) AND (?='' OR status=?) ORDER BY synced_at DESC LIMIT 200`, accountID, accountID, status, status)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []domain.Comment
	for rows.Next() {
		var c domain.Comment
		if err = rows.Scan(&c.ID, &c.ChannelAccountID, &c.MsgDataID, &c.ArticleIndex, &c.CommentID, &c.AuthorID, &c.Content, &c.Status, &c.ReplyContent, &c.SyncedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (m *MySQL) UpsertComment(ctx context.Context, c domain.Comment) error {
	_, err := m.db.ExecContext(ctx, `INSERT INTO article_comment(id,channel_account_id,msg_data_id,article_index,comment_id,author_id,content,status,reply_content,synced_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE content=VALUES(content),author_id=VALUES(author_id),synced_at=VALUES(synced_at)`, c.ID, c.ChannelAccountID, c.MsgDataID, c.ArticleIndex, c.CommentID, c.AuthorID, c.Content, c.Status, c.ReplyContent, c.SyncedAt, time.Now().UTC())
	return err
}
func (m *MySQL) UpdateComment(ctx context.Context, c domain.Comment) error {
	result, err := m.db.ExecContext(ctx, `UPDATE article_comment SET status=?,reply_content=?,updated_at=? WHERE id=?`, c.Status, c.ReplyContent, time.Now().UTC(), c.ID)
	return changed(result, err)
}

func (m *MySQL) CreateTicket(ctx context.Context, t domain.Ticket, actorID string) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO ticket(id,customer_id,conversation_id,comment_id,title,description,status,priority,assignee_id,due_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, t.ID, t.CustomerID, t.ConversationID, t.CommentID, t.Title, t.Description, t.Status, t.Priority, t.AssigneeID, t.DueAt, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ticket_event(id,ticket_id,event_type,actor_id,payload_json,created_at) VALUES(?,?,?,?,?,?)`, domain.NewID(), t.ID, "created", actorID, JSON(map[string]string{"status": t.Status}), t.CreatedAt)
	if err != nil {
		return err
	}
	if err = insertAuditTx(ctx, tx, actorID, "ticket.create", "ticket", t.ID, JSON(map[string]string{"conversationId": t.ConversationID, "commentId": t.CommentID})); err != nil {
		return err
	}
	return tx.Commit()
}
func (m *MySQL) UpdateTicket(ctx context.Context, t domain.Ticket, actorID string) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE ticket SET status=?,priority=?,assignee_id=?,due_at=?,version=version+1,updated_at=? WHERE id=? AND version=?`, t.Status, t.Priority, t.AssigneeID, t.DueAt, time.Now().UTC(), t.ID, t.Version)
	if err = changed(result, err); err != nil {
		return domain.ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ticket_event(id,ticket_id,event_type,actor_id,payload_json,created_at) VALUES(?,?,?,?,?,?)`, domain.NewID(), t.ID, "updated", actorID, JSON(map[string]string{"status": t.Status, "priority": t.Priority, "assigneeId": t.AssigneeID}), time.Now().UTC())
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (m *MySQL) Ticket(ctx context.Context, id string) (t domain.Ticket, err error) {
	err = m.db.QueryRowContext(ctx, `SELECT id,customer_id,conversation_id,comment_id,title,description,status,priority,assignee_id,due_at,version,created_at,updated_at FROM ticket WHERE id=?`, id).Scan(&t.ID, &t.CustomerID, &t.ConversationID, &t.CommentID, &t.Title, &t.Description, &t.Status, &t.Priority, &t.AssigneeID, &t.DueAt, &t.Version, &t.CreatedAt, &t.UpdatedAt)
	return t, mapSQLError(err)
}
func (m *MySQL) Tickets(ctx context.Context) ([]domain.Ticket, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,customer_id,conversation_id,comment_id,title,description,status,priority,assignee_id,due_at,version,created_at,updated_at FROM ticket ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tickets []domain.Ticket
	for rows.Next() {
		var t domain.Ticket
		if err = rows.Scan(&t.ID, &t.CustomerID, &t.ConversationID, &t.CommentID, &t.Title, &t.Description, &t.Status, &t.Priority, &t.AssigneeID, &t.DueAt, &t.Version, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}
func (m *MySQL) TicketsByConversation(ctx context.Context, id string) ([]domain.Ticket, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,customer_id,conversation_id,comment_id,title,description,status,priority,assignee_id,due_at,version,created_at,updated_at FROM ticket WHERE conversation_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tickets []domain.Ticket
	for rows.Next() {
		var t domain.Ticket
		if err = rows.Scan(&t.ID, &t.CustomerID, &t.ConversationID, &t.CommentID, &t.Title, &t.Description, &t.Status, &t.Priority, &t.AssigneeID, &t.DueAt, &t.Version, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}
func (m *MySQL) TicketEvents(ctx context.Context, id string) ([]domain.TicketEvent, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,ticket_id,event_type,actor_id,payload_json,created_at FROM ticket_event WHERE ticket_id=? ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var events []domain.TicketEvent
	for rows.Next() {
		var e domain.TicketEvent
		if err = rows.Scan(&e.ID, &e.TicketID, &e.Type, &e.ActorID, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (m *MySQL) SaveShift(ctx context.Context, s domain.Shift) error {
	_, err := m.db.ExecContext(ctx, `INSERT INTO shift(id,agent_id,team_id,start_at,end_at,state,created_at) VALUES(?,?,?,?,?,?,?)`, s.ID, s.AgentID, s.TeamID, s.StartAt, s.EndAt, s.State, time.Now().UTC())
	return err
}
func (m *MySQL) Shifts(ctx context.Context) ([]domain.Shift, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,agent_id,team_id,start_at,end_at,state FROM shift ORDER BY start_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []domain.Shift
	for rows.Next() {
		var s domain.Shift
		if err = rows.Scan(&s.ID, &s.AgentID, &s.TeamID, &s.StartAt, &s.EndAt, &s.State); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}
func (m *MySQL) SaveRoutingRule(ctx context.Context, r domain.RoutingRule) error {
	_, err := m.db.ExecContext(ctx, `INSERT INTO routing_rule(id,priority,match_json,target_team_id,active,created_at) VALUES(?,?,?,?,?,?)`, r.ID, r.Priority, JSON(map[string]any{"channel": r.Channel, "accountId": r.AccountID, "tag": r.Tag, "startMinute": r.StartMinute, "endMinute": r.EndMinute}), r.TeamID, r.Active, time.Now().UTC())
	return err
}
func (m *MySQL) RoutingRules(ctx context.Context) ([]domain.RoutingRule, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,priority,match_json,target_team_id,active FROM routing_rule ORDER BY priority DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []domain.RoutingRule
	for rows.Next() {
		var r domain.RoutingRule
		var data []byte
		if err = rows.Scan(&r.ID, &r.Priority, &data, &r.TeamID, &r.Active); err != nil {
			return nil, err
		}
		var match struct {
			Channel, AccountID, Tag string
			StartMinute, EndMinute  int
		}
		if err = json.Unmarshal(data, &match); err != nil {
			return nil, err
		}
		r.Channel, r.AccountID, r.Tag, r.StartMinute, r.EndMinute = match.Channel, match.AccountID, match.Tag, match.StartMinute, match.EndMinute
		result = append(result, r)
	}
	return result, rows.Err()
}
func (m *MySQL) TeamAgents(ctx context.Context, teamID string) ([]domain.Agent, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT a.id,a.dingtalk_user_id,a.name,a.role,a.active FROM agent a JOIN team_agent ta ON ta.agent_id=a.id WHERE ta.team_id=? AND a.active=true`, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var agents []domain.Agent
	for rows.Next() {
		var a domain.Agent
		if err = rows.Scan(&a.ID, &a.DingTalkUserID, &a.Name, &a.Role, &a.Active); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}
func (m *MySQL) SaveTeamAgent(ctx context.Context, teamID, agentID string) error {
	_, err := m.db.ExecContext(ctx, `INSERT INTO team_agent(team_id,agent_id) VALUES(?,?) ON DUPLICATE KEY UPDATE agent_id=VALUES(agent_id)`, teamID, agentID)
	return err
}
func (m *MySQL) Audit(ctx context.Context, a domain.AuditLog) error {
	return m.insertAudit(ctx, a.ActorID, a.Action, a.ResourceType, a.ResourceID, a.Detail)
}
func (m *MySQL) insertAudit(ctx context.Context, actorID, action, resourceType, resourceID string, detail json.RawMessage) error {
	_, err := m.db.ExecContext(ctx, `INSERT INTO audit_log(id,actor_id,action,resource_type,resource_id,detail_json,created_at) VALUES(?,?,?,?,?,?,?)`, domain.NewID(), actorID, action, resourceType, resourceID, detail, time.Now().UTC())
	return err
}
func insertAuditTx(ctx context.Context, tx *sql.Tx, actorID, action, resourceType, resourceID string, detail json.RawMessage) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_log(id,actor_id,action,resource_type,resource_id,detail_json,created_at) VALUES(?,?,?,?,?,?,?)`, domain.NewID(), actorID, action, resourceType, resourceID, detail, time.Now().UTC())
	return err
}
func (m *MySQL) Audits(ctx context.Context) ([]domain.AuditLog, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,actor_id,action,resource_type,resource_id,detail_json,created_at FROM audit_log ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []domain.AuditLog
	for rows.Next() {
		var a domain.AuditLog
		if err = rows.Scan(&a.ID, &a.ActorID, &a.Action, &a.ResourceType, &a.ResourceID, &a.Detail, &a.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
func (m *MySQL) RecordSLA(ctx context.Context, resourceType, resourceID, kind string) (bool, error) {
	result, err := m.db.ExecContext(ctx, `INSERT INTO sla_alert(id,resource_type,resource_id,kind,created_at) VALUES(?,?,?,?,?)`, domain.NewID(), resourceType, resourceID, kind, time.Now().UTC())
	if duplicate(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	if kind == "expired" && resourceType == "conversation" {
		_, err = m.db.ExecContext(ctx, `UPDATE conversation SET sla_escalated_at=? WHERE id=? AND sla_escalated_at IS NULL`, time.Now().UTC(), resourceID)
	}
	return n > 0, err
}
func (m *MySQL) ExpiringConversations(ctx context.Context, start, end time.Time) ([]domain.Conversation, error) {
	rows, err := m.db.QueryContext(ctx, conversationSelect+` WHERE status<>'closed' AND reply_deadline_at>? AND reply_deadline_at<=? LIMIT 500`, start.UTC(), end.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []domain.Conversation
	for rows.Next() {
		c, scanErr := scanConversation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (m *MySQL) ExpiredConversations(ctx context.Context, now time.Time) ([]domain.Conversation, error) {
	rows, err := m.db.QueryContext(ctx, conversationSelect+` WHERE status<>'closed' AND reply_deadline_at<=? LIMIT 500`, now.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []domain.Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (m *MySQL) ExpiredTickets(ctx context.Context, now time.Time) ([]domain.Ticket, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,customer_id,conversation_id,comment_id,title,description,status,priority,assignee_id,due_at,version,created_at,updated_at FROM ticket WHERE status NOT IN ('resolved','closed') AND due_at<=? LIMIT 500`, now.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tickets []domain.Ticket
	for rows.Next() {
		var ticket domain.Ticket
		if err = rows.Scan(&ticket.ID, &ticket.CustomerID, &ticket.ConversationID, &ticket.CommentID, &ticket.Title, &ticket.Description, &ticket.Status, &ticket.Priority, &ticket.AssigneeID, &ticket.DueAt, &ticket.Version, &ticket.CreatedAt, &ticket.UpdatedAt); err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	return tickets, rows.Err()
}
func (m *MySQL) Report(ctx context.Context) (Report, error) {
	var r Report
	err := m.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM conversation),(SELECT COUNT(*) FROM conversation WHERE status='closed'),(SELECT COUNT(*) FROM sla_alert WHERE kind='expired'),COALESCE((SELECT AVG(TIMESTAMPDIFF(SECOND,created_at,first_response_at)) FROM conversation WHERE first_response_at IS NOT NULL),0)`).Scan(&r.NewConversations, &r.ClosedConversations, &r.SLAExpired, &r.AverageFirstResponseSeconds)
	return r, err
}

func changed(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
func duplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
func mapSQLError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if duplicate(err) {
		return domain.ErrDuplicate
	}
	return err
}

var _ Store = (*MySQL)(nil)
