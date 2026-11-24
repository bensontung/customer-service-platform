package store

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/customer-service-platform/pkg/domain"
)

type memoryOutboxEvent struct {
	event        domain.OutboxEvent
	availableAt  time.Time
	createdAt    time.Time
	published    bool
	attempts     int
	claimedUntil time.Time
}

type notificationClaim struct {
	owner        string
	claimedUntil time.Time
	completed    bool
}

type slaKey struct {
	resourceType string
	resourceID   string
	kind         string
}

type Memory struct {
	mu                 sync.RWMutex
	now                func() time.Time
	accounts           map[string]domain.ChannelAccount
	customers          map[string]domain.Customer
	conversations      map[string]domain.Conversation
	messages           map[string]domain.Message
	agents             map[string]domain.Agent
	teamAgents         map[string]map[string]struct{}
	comments           map[string]domain.Comment
	tickets            map[string]domain.Ticket
	ticketEvents       map[string][]domain.TicketEvent
	shifts             map[string]domain.Shift
	routingRules       map[string]domain.RoutingRule
	audits             []domain.AuditLog
	inboxEvents        map[string]struct{}
	customerKeys       map[string]string
	outbox             map[string]*memoryOutboxEvent
	messageClaims      map[string]time.Time
	notificationClaims map[string]notificationClaim
	slaAlerts          map[slaKey]struct{}
}

func NewMemory() *Memory {
	return &Memory{
		now:                func() time.Time { return time.Now().UTC() },
		accounts:           make(map[string]domain.ChannelAccount),
		customers:          make(map[string]domain.Customer),
		conversations:      make(map[string]domain.Conversation),
		messages:           make(map[string]domain.Message),
		agents:             make(map[string]domain.Agent),
		teamAgents:         make(map[string]map[string]struct{}),
		comments:           make(map[string]domain.Comment),
		tickets:            make(map[string]domain.Ticket),
		ticketEvents:       make(map[string][]domain.TicketEvent),
		shifts:             make(map[string]domain.Shift),
		routingRules:       make(map[string]domain.RoutingRule),
		inboxEvents:        make(map[string]struct{}),
		customerKeys:       make(map[string]string),
		outbox:             make(map[string]*memoryOutboxEvent),
		messageClaims:      make(map[string]time.Time),
		notificationClaims: make(map[string]notificationClaim),
		slaAlerts:          make(map[slaKey]struct{}),
	}
}

func memoryError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func copyBytes(data []byte) []byte {
	return append([]byte(nil), data...)
}

func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func copyCustomer(c domain.Customer) domain.Customer {
	c.Tags = append([]string(nil), c.Tags...)
	return c
}

func copyConversation(c domain.Conversation) domain.Conversation {
	c.FirstResponseAt = copyTime(c.FirstResponseAt)
	c.SLAEscalatedAt = copyTime(c.SLAEscalatedAt)
	return c
}

func copyMessage(m domain.Message) domain.Message {
	m.Content = copyBytes(m.Content)
	m.NextRetryAt = copyTime(m.NextRetryAt)
	return m
}

func copyTicket(t domain.Ticket) domain.Ticket {
	t.DueAt = copyTime(t.DueAt)
	return t
}

func copyAudit(a domain.AuditLog) domain.AuditLog {
	a.Detail = copyBytes(a.Detail)
	return a
}

func copyOutbox(event domain.OutboxEvent) domain.OutboxEvent {
	event.Payload = copyBytes(event.Payload)
	return event
}

func memoryUniqueKey(values ...string) string {
	return strings.Join(values, "\x00")
}

func (m *Memory) AccountByKey(ctx context.Context, key string) (domain.ChannelAccount, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, account := range m.accounts {
		if account.AccountKey == key && account.Status == "active" {
			return account, memoryError(ctx)
		}
	}
	return domain.ChannelAccount{}, domain.ErrNotFound
}

func (m *Memory) AccountByID(ctx context.Context, id string) (domain.ChannelAccount, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	account, ok := m.accounts[id]
	if !ok {
		return domain.ChannelAccount{}, domain.ErrNotFound
	}
	return account, memoryError(ctx)
}

func (m *Memory) SaveAccount(ctx context.Context, a domain.ChannelAccount) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, account := range m.accounts {
		if id != a.ID && account.AccountKey == a.AccountKey {
			return domain.ErrDuplicate
		}
	}
	m.accounts[a.ID] = a
	return nil
}

func (m *Memory) insertOutbox(event domain.OutboxEvent, now time.Time) {
	m.outbox[event.ID] = &memoryOutboxEvent{event: copyOutbox(event), availableAt: now, createdAt: now}
}

func (m *Memory) insertAudit(a domain.AuditLog, now time.Time) {
	if a.ID == "" {
		a.ID = domain.NewID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	m.audits = append(m.audits, copyAudit(a))
}

func (m *Memory) ReceiveInbound(ctx context.Context, in domain.Inbound, a domain.ChannelAccount) (bool, error) {
	if err := memoryError(ctx); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryUniqueKey("wechat:"+a.ID, in.EventID)
	if _, exists := m.inboxEvents[key]; exists {
		return false, nil
	}
	m.inboxEvents[key] = struct{}{}
	now := m.now()
	customerKey := memoryUniqueKey(a.Channel, a.AppID, in.OpenID)
	customerID, exists := m.customerKeys[customerKey]
	if !exists {
		customerID = domain.NewID()
		m.customers[customerID] = domain.Customer{
			ID: customerID, Channel: a.Channel, AppID: a.AppID, OpenID: in.OpenID, Tags: []string{},
		}
		m.customerKeys[customerKey] = customerID
	}
	conversationID := ""
	latest := time.Time{}
	for _, conversation := range m.conversations {
		if conversation.CustomerID == customerID && conversation.ChannelAccountID == a.ID &&
			conversation.Status != domain.StatusClosed && (conversationID == "" || conversation.CreatedAt.After(latest)) {
			conversationID = conversation.ID
			latest = conversation.CreatedAt
		}
	}
	if conversationID == "" {
		conversationID = domain.NewID()
		m.conversations[conversationID] = domain.Conversation{
			ID: conversationID, Source: domain.SourcePrivate, ChannelAccountID: a.ID,
			CustomerID: customerID, Status: domain.StatusWaiting, CreatedAt: now, UpdatedAt: now,
		}
	}
	messageID := domain.NewID()
	m.messages[messageID] = domain.Message{
		ID: messageID, ConversationID: conversationID, ChannelAccountID: a.ID, Channel: a.Channel,
		Direction: "inbound", ChannelMessageID: in.EventID, Type: in.Type,
		Content: JSON(map[string]string{"text": in.Content, "mediaId": in.MediaID}),
		Status:  "received", CreatedAt: now, UpdatedAt: now,
	}
	conversation := m.conversations[conversationID]
	conversation.LastInboundAt = in.ReceivedAt
	conversation.ReplyDeadlineAt = domain.NewReplyWindow(in.ReceivedAt)
	conversation.RepliesInWindow = 0
	conversation.UpdatedAt = now
	m.conversations[conversationID] = conversation
	m.insertOutbox(domain.OutboxEvent{
		ID: domain.NewID(), Type: "MessageReceived", AggregateID: conversationID,
		Payload: JSON(map[string]string{"messageId": messageID, "conversationId": conversationID}),
	}, now)
	return true, nil
}

func (m *Memory) AgentByID(ctx context.Context, id string) (domain.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	agent, ok := m.agents[id]
	if !ok {
		return domain.Agent{}, domain.ErrNotFound
	}
	return agent, memoryError(ctx)
}

func (m *Memory) AgentByDingTalkID(ctx context.Context, id string) (domain.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, agent := range m.agents {
		if agent.DingTalkUserID == id && agent.Active {
			return agent, memoryError(ctx)
		}
	}
	return domain.Agent{}, domain.ErrNotFound
}

func (m *Memory) SaveAgent(ctx context.Context, a domain.Agent) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, agent := range m.agents {
		if id != a.ID && agent.DingTalkUserID == a.DingTalkUserID {
			return domain.ErrDuplicate
		}
	}
	m.agents[a.ID] = a
	return nil
}

func (m *Memory) Conversation(ctx context.Context, id string) (domain.Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conversation, ok := m.conversations[id]
	if !ok {
		return domain.Conversation{}, domain.ErrNotFound
	}
	return copyConversation(conversation), memoryError(ctx)
}

func (m *Memory) Conversations(ctx context.Context, filter ConversationFilter) ([]domain.Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.Conversation, 0)
	for _, conversation := range m.conversations {
		if filter.Status != "" && conversation.Status != filter.Status ||
			filter.Source != "" && conversation.Source != filter.Source ||
			filter.Assignee != "" && conversation.AssigneeID != filter.Assignee {
			continue
		}
		result = append(result, copyConversation(conversation))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].LastInboundAt.Equal(result[j].LastInboundAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].LastInboundAt.After(result[j].LastInboundAt)
	})
	if len(result) > 200 {
		result = result[:200]
	}
	return result, memoryError(ctx)
}

func (m *Memory) Messages(ctx context.Context, id string) ([]domain.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.Message, 0)
	for _, message := range m.messages {
		if message.ConversationID == id {
			result = append(result, copyMessage(message))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	if len(result) > 1000 {
		result = result[:1000]
	}
	return result, memoryError(ctx)
}

func (m *Memory) Customer(ctx context.Context, id string) (domain.Customer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	customer, ok := m.customers[id]
	if !ok {
		return domain.Customer{}, domain.ErrNotFound
	}
	return copyCustomer(customer), memoryError(ctx)
}

func (m *Memory) UpdateCustomer(ctx context.Context, c domain.Customer) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	customer, ok := m.customers[c.ID]
	if !ok {
		return domain.ErrNotFound
	}
	customer.Nickname, customer.Note, customer.Tags = c.Nickname, c.Note, append([]string(nil), c.Tags...)
	m.customers[c.ID] = customer
	return nil
}

func (m *Memory) ClaimConversation(ctx context.Context, id string, agent domain.Agent) (domain.Conversation, error) {
	if err := memoryError(ctx); err != nil {
		return domain.Conversation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conversation, ok := m.conversations[id]
	if !ok || conversation.Status != domain.StatusWaiting || conversation.AssigneeID != "" {
		return domain.Conversation{}, domain.ErrConflict
	}
	conversation.AssigneeID = agent.ID
	conversation.Status = domain.StatusOpen
	conversation.Version++
	conversation.UpdatedAt = m.now()
	m.conversations[id] = conversation
	return copyConversation(conversation), nil
}

func (m *Memory) TransferConversation(ctx context.Context, id, assigneeID, teamID string, agent domain.Agent) (domain.Conversation, error) {
	if err := memoryError(ctx); err != nil {
		return domain.Conversation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conversation, ok := m.conversations[id]
	if !ok {
		return domain.Conversation{}, domain.ErrNotFound
	}
	if !domain.CanAccess(conversation, agent) || conversation.AssigneeID == "" && agent.Role == domain.RoleAgent {
		return conversation, domain.ErrForbidden
	}
	if assigneeID != "" {
		target, exists := m.agents[assigneeID]
		if !exists || !target.Active {
			return conversation, domain.ErrInvalidRequest
		}
	}
	conversation.AssigneeID, conversation.TeamID = assigneeID, teamID
	conversation.Status = domain.StatusOpen
	conversation.Version++
	conversation.UpdatedAt = m.now()
	m.conversations[id] = conversation
	m.insertAudit(domain.AuditLog{
		ActorID: agent.ID, Action: "conversation.transfer", ResourceType: "conversation", ResourceID: id,
		Detail: JSON(map[string]string{"assigneeId": assigneeID, "teamId": teamID}),
	}, conversation.UpdatedAt)
	m.insertOutbox(domain.OutboxEvent{ID: domain.NewID(), Type: "ConversationTransferred", AggregateID: id, Payload: JSON(map[string]string{"agentId": assigneeID})}, conversation.UpdatedAt)
	return copyConversation(conversation), nil
}

func (m *Memory) AssignConversation(ctx context.Context, id, teamID, agentID string) (domain.Conversation, error) {
	if err := memoryError(ctx); err != nil {
		return domain.Conversation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conversation, ok := m.conversations[id]
	if !ok {
		return domain.Conversation{}, domain.ErrNotFound
	}
	if conversation.AssigneeID == "" && conversation.Status == domain.StatusWaiting {
		conversation.TeamID, conversation.AssigneeID, conversation.Status = teamID, agentID, domain.StatusOpen
		conversation.Version++
		conversation.UpdatedAt = m.now()
		m.conversations[id] = conversation
	}
	return copyConversation(conversation), nil
}

func (m *Memory) CloseConversation(ctx context.Context, id string, agent domain.Agent) (domain.Conversation, error) {
	if err := memoryError(ctx); err != nil {
		return domain.Conversation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conversation, ok := m.conversations[id]
	if !ok {
		return domain.Conversation{}, domain.ErrNotFound
	}
	if conversation.AssigneeID != agent.ID && agent.Role == domain.RoleAgent {
		return conversation, domain.ErrForbidden
	}
	closedAt := m.now()
	conversation.Status = domain.StatusClosed
	conversation.Version++
	conversation.UpdatedAt = closedAt
	m.conversations[id] = conversation
	m.insertAudit(domain.AuditLog{
		ActorID: agent.ID, Action: "conversation.close", ResourceType: "conversation",
		ResourceID: id, Detail: JSON(map[string]string{}),
	}, closedAt)
	return copyConversation(conversation), nil
}

func (m *Memory) QueueMessage(ctx context.Context, id string, agent domain.Agent, kind, text, mediaID string, keys ...string) (domain.Message, error) {
	if err := memoryError(ctx); err != nil {
		return domain.Message{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	conversation, ok := m.conversations[id]
	if !ok {
		return domain.Message{}, domain.ErrNotFound
	}
	now := m.now()
	if err := domain.CanReply(conversation, agent, now); err != nil {
		return domain.Message{}, err
	}
	if kind == "text" && (strings.TrimSpace(text) == "" || len(text) > 2048) ||
		kind != "text" && kind != "image" && kind != "voice" ||
		kind != "text" && mediaID == "" {
		return domain.Message{}, domain.ErrInvalidRequest
	}
	externalID := "local:" + domain.NewID()
	if len(keys) > 0 && keys[0] != "" {
		externalID = keys[0]
		for _, existing := range m.messages {
			if existing.ChannelAccountID == conversation.ChannelAccountID && existing.ChannelMessageID == externalID {
				return copyMessage(existing), nil
			}
		}
	}
	message := domain.Message{
		ID: domain.NewID(), ConversationID: id, ChannelAccountID: conversation.ChannelAccountID,
		Channel: "pending", Direction: "outbound", ChannelMessageID: externalID,
		Type: kind, Content: JSON(map[string]string{"text": text, "mediaId": mediaID}),
		Status: domain.StatusQueued, CreatedAt: now, UpdatedAt: now,
	}
	m.messages[message.ID] = message
	conversation.RepliesInWindow++
	conversation.Version++
	conversation.UpdatedAt = now
	m.conversations[id] = conversation
	m.insertOutbox(domain.OutboxEvent{
		ID: domain.NewID(), Type: "MessageSendRequested", AggregateID: message.ID,
		Payload: JSON(map[string]string{"messageId": message.ID}),
	}, now)
	if account, exists := m.accounts[conversation.ChannelAccountID]; exists {
		message.Channel = account.Channel
	} else {
		return domain.Message{}, domain.ErrNotFound
	}
	return copyMessage(message), nil
}

func (m *Memory) MessageByExternalID(ctx context.Context, accountID, externalID string) (domain.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, message := range m.messages {
		if message.ChannelAccountID == accountID && message.ChannelMessageID == externalID {
			return copyMessage(message), memoryError(ctx)
		}
	}
	return domain.Message{}, domain.ErrNotFound
}
func (m *Memory) Message(ctx context.Context, id string) (domain.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	message, ok := m.messages[id]
	if !ok {
		return domain.Message{}, domain.ErrNotFound
	}
	return copyMessage(message), memoryError(ctx)
}

func (m *Memory) ClaimMessage(ctx context.Context, id string, now time.Time) (domain.Message, bool, error) {
	if err := memoryError(ctx); err != nil {
		return domain.Message{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	message, ok := m.messages[id]
	if !ok || (message.Status != domain.StatusQueued && message.Status != domain.StatusRetrying) || m.messageClaims[id].After(now.UTC()) {
		return domain.Message{}, false, nil
	}
	m.messageClaims[id] = now.Add(30 * time.Second).UTC()
	return copyMessage(message), true, nil
}

func (m *Memory) UpdateMessage(ctx context.Context, message domain.Message) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.messages[message.ID]
	if !ok {
		return domain.ErrNotFound
	}
	current.Channel, current.Status = message.Channel, message.Status
	current.ErrorCode, current.ErrorMessage = message.ErrorCode, message.ErrorMessage
	current.RetryCount, current.NextRetryAt = message.RetryCount, copyTime(message.NextRetryAt)
	delete(m.messageClaims, message.ID)
	current.UpdatedAt = m.now()
	m.messages[message.ID] = current
	if current.Status == domain.StatusSent && current.Direction == "outbound" {
		conversation := m.conversations[current.ConversationID]
		if conversation.FirstResponseAt == nil {
			sentAt := m.now()
			conversation.FirstResponseAt = &sentAt
		}
		m.conversations[current.ConversationID] = conversation
	}
	return nil
}

func (m *Memory) RetryableMessages(ctx context.Context, now time.Time) ([]domain.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.Message, 0)
	for _, message := range m.messages {
		if message.Status == domain.StatusRetrying && message.NextRetryAt != nil && !message.NextRetryAt.After(now.UTC()) && !m.messageClaims[message.ID].After(now.UTC()) {
			result = append(result, copyMessage(message))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	if len(result) > 100 {
		result = result[:100]
	}
	return result, memoryError(ctx)
}

func (m *Memory) ClaimOutbox(ctx context.Context, now time.Time) ([]domain.OutboxEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	events := make([]*memoryOutboxEvent, 0)
	for _, event := range m.outbox {
		if !event.published && !event.availableAt.After(now.UTC()) && (event.claimedUntil.IsZero() || !event.claimedUntil.After(now.UTC())) {
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].createdAt.Equal(events[j].createdAt) {
			return events[i].event.ID < events[j].event.ID
		}
		return events[i].createdAt.Before(events[j].createdAt)
	})
	if len(events) > 100 {
		events = events[:100]
	}
	result := make([]domain.OutboxEvent, len(events))
	for i, event := range events {
		event.claimedUntil = now.Add(30 * time.Second)
		result[i] = copyOutbox(event.event)
		result[i].Attempts = event.attempts
	}
	return result, memoryError(ctx)
}

func (m *Memory) MarkOutboxPublished(ctx context.Context, id string) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if event, ok := m.outbox[id]; ok {
		event.published = true
		event.claimedUntil = time.Time{}
	}
	return nil
}

func (m *Memory) ReleaseOutbox(ctx context.Context, id string, next time.Time) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if event, ok := m.outbox[id]; ok {
		event.attempts++
		event.availableAt = next.UTC()
		event.claimedUntil = time.Time{}
	}
	return nil
}

func (m *Memory) ReserveNotification(ctx context.Context, conversationID, agentID, kind string, now time.Time) (string, bool, error) {
	if err := memoryError(ctx); err != nil {
		return "", false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryUniqueKey(conversationID, agentID, kind)
	claim, exists := m.notificationClaims[key]
	now = now.UTC()
	if claim.completed || (exists && claim.claimedUntil.After(now)) {
		return "", false, nil
	}
	claim.owner = domain.NewID()
	claim.claimedUntil = now.Add(30 * time.Second)
	claim.completed = false
	m.notificationClaims[key] = claim
	return claim.owner, true, nil
}
func (m *Memory) CompleteNotification(ctx context.Context, conversationID, agentID, kind, owner string) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryUniqueKey(conversationID, agentID, kind)
	claim := m.notificationClaims[key]
	if claim.owner != owner {
		return nil
	}
	claim.completed = true
	claim.claimedUntil = time.Time{}
	m.notificationClaims[key] = claim
	return nil
}

func (m *Memory) Comment(ctx context.Context, id string) (domain.Comment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	comment, ok := m.comments[id]
	if !ok {
		return domain.Comment{}, domain.ErrNotFound
	}
	return comment, memoryError(ctx)
}

func (m *Memory) Comments(ctx context.Context, accountID, status string) ([]domain.Comment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.Comment, 0)
	for _, comment := range m.comments {
		if (accountID == "" || comment.ChannelAccountID == accountID) && (status == "" || comment.Status == status) {
			result = append(result, comment)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SyncedAt.Equal(result[j].SyncedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].SyncedAt.After(result[j].SyncedAt)
	})
	if len(result) > 200 {
		result = result[:200]
	}
	return result, memoryError(ctx)
}

func (m *Memory) UpsertComment(ctx context.Context, c domain.Comment) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, existing := range m.comments {
		if id != c.ID && existing.ChannelAccountID == c.ChannelAccountID &&
			existing.MsgDataID == c.MsgDataID && existing.ArticleIndex == c.ArticleIndex &&
			existing.CommentID == c.CommentID {
			return domain.ErrDuplicate
		}
	}
	if current, exists := m.comments[c.ID]; exists {
		current.Content, current.AuthorID, current.SyncedAt = c.Content, c.AuthorID, c.SyncedAt
		current.UpdatedAt = m.now()
		m.comments[c.ID] = current
	} else {
		m.comments[c.ID] = c
	}
	return nil
}

func (m *Memory) UpdateComment(ctx context.Context, c domain.Comment) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	comment, ok := m.comments[c.ID]
	if !ok {
		return domain.ErrNotFound
	}
	comment.Status, comment.ReplyContent, comment.UpdatedAt = c.Status, c.ReplyContent, m.now()
	m.comments[c.ID] = comment
	return nil
}

func (m *Memory) CreateTicket(ctx context.Context, t domain.Ticket, actorID string) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tickets[t.ID]; exists {
		return domain.ErrDuplicate
	}
	m.tickets[t.ID] = copyTicket(t)
	m.ticketEvents[t.ID] = []domain.TicketEvent{{
		ID: domain.NewID(), TicketID: t.ID, Type: "created", ActorID: actorID,
		Payload: JSON(map[string]string{"status": t.Status}), CreatedAt: t.CreatedAt,
	}}
	m.insertAudit(domain.AuditLog{
		ActorID: actorID, Action: "ticket.create", ResourceType: "ticket", ResourceID: t.ID,
		Detail: JSON(map[string]string{"conversationId": t.ConversationID, "commentId": t.CommentID}),
	}, m.now())
	return nil
}

func (m *Memory) UpdateTicket(ctx context.Context, t domain.Ticket, actorID string) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ticket, ok := m.tickets[t.ID]
	if !ok || ticket.Version != t.Version {
		return domain.ErrConflict
	}
	now := m.now()
	ticket.Status, ticket.Priority, ticket.AssigneeID = t.Status, t.Priority, t.AssigneeID
	ticket.DueAt = copyTime(t.DueAt)
	ticket.Version++
	ticket.UpdatedAt = now
	m.tickets[t.ID] = ticket
	m.ticketEvents[t.ID] = append(m.ticketEvents[t.ID], domain.TicketEvent{
		ID: domain.NewID(), TicketID: t.ID, Type: "updated", ActorID: actorID,
		Payload:   JSON(map[string]string{"status": t.Status, "priority": t.Priority, "assigneeId": t.AssigneeID}),
		CreatedAt: now,
	})
	return nil
}

func (m *Memory) Ticket(ctx context.Context, id string) (domain.Ticket, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ticket, ok := m.tickets[id]
	if !ok {
		return domain.Ticket{}, domain.ErrNotFound
	}
	return copyTicket(ticket), memoryError(ctx)
}

func (m *Memory) Tickets(ctx context.Context) ([]domain.Ticket, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.Ticket, 0, len(m.tickets))
	for _, ticket := range m.tickets {
		result = append(result, copyTicket(ticket))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, memoryError(ctx)
}

func (m *Memory) TicketsByConversation(ctx context.Context, id string) ([]domain.Ticket, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.Ticket, 0)
	for _, ticket := range m.tickets {
		if ticket.ConversationID == id {
			result = append(result, copyTicket(ticket))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, memoryError(ctx)
}

func (m *Memory) TicketEvents(ctx context.Context, id string) ([]domain.TicketEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stored := append([]domain.TicketEvent(nil), m.ticketEvents[id]...)
	sort.Slice(stored, func(i, j int) bool {
		if stored[i].CreatedAt.Equal(stored[j].CreatedAt) {
			return stored[i].ID < stored[j].ID
		}
		return stored[i].CreatedAt.Before(stored[j].CreatedAt)
	})
	for i := range stored {
		stored[i].Payload = copyBytes(stored[i].Payload)
	}
	return stored, memoryError(ctx)
}

func (m *Memory) SaveShift(ctx context.Context, s domain.Shift) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shifts[s.ID] = s
	return nil
}

func (m *Memory) Shifts(ctx context.Context) ([]domain.Shift, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.Shift, 0, len(m.shifts))
	for _, shift := range m.shifts {
		result = append(result, shift)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].StartAt.Equal(result[j].StartAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].StartAt.After(result[j].StartAt)
	})
	if len(result) > 500 {
		result = result[:500]
	}
	return result, memoryError(ctx)
}

func (m *Memory) SaveRoutingRule(ctx context.Context, r domain.RoutingRule) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routingRules[r.ID] = r
	return nil
}

func (m *Memory) RoutingRules(ctx context.Context) ([]domain.RoutingRule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.RoutingRule, 0, len(m.routingRules))
	for _, rule := range m.routingRules {
		result = append(result, rule)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority == result[j].Priority {
			return result[i].ID < result[j].ID
		}
		return result[i].Priority > result[j].Priority
	})
	return result, memoryError(ctx)
}

func (m *Memory) TeamAgents(ctx context.Context, teamID string) ([]domain.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.Agent, 0)
	for agentID := range m.teamAgents[teamID] {
		if agent, ok := m.agents[agentID]; ok && agent.Active {
			result = append(result, agent)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, memoryError(ctx)
}

func (m *Memory) SaveTeamAgent(ctx context.Context, teamID, agentID string) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.teamAgents[teamID] == nil {
		m.teamAgents[teamID] = make(map[string]struct{})
	}
	m.teamAgents[teamID][agentID] = struct{}{}
	return nil
}

func (m *Memory) Audit(ctx context.Context, a domain.AuditLog) error {
	if err := memoryError(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.insertAudit(a, m.now())
	return nil
}

func (m *Memory) Audits(ctx context.Context) ([]domain.AuditLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.AuditLog, len(m.audits))
	for i, audit := range m.audits {
		result[i] = copyAudit(audit)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	if len(result) > 500 {
		result = result[:500]
	}
	return result, memoryError(ctx)
}

func (m *Memory) RecordSLA(ctx context.Context, resourceType, resourceID, kind string) (bool, error) {
	if err := memoryError(ctx); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := slaKey{resourceType: resourceType, resourceID: resourceID, kind: kind}
	if _, exists := m.slaAlerts[key]; exists {
		return false, nil
	}
	m.slaAlerts[key] = struct{}{}
	if kind == "expired" {
		if conversation, ok := m.conversations[resourceID]; ok && conversation.SLAEscalatedAt == nil {
			now := m.now()
			conversation.SLAEscalatedAt = &now
			m.conversations[resourceID] = conversation
		}
	}
	return true, nil
}

func (m *Memory) ExpiringConversations(ctx context.Context, start, end time.Time) ([]domain.Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.Conversation, 0)
	for _, conversation := range m.conversations {
		if conversation.Status != domain.StatusClosed && conversation.ReplyDeadlineAt.After(start) && !conversation.ReplyDeadlineAt.After(end) {
			result = append(result, copyConversation(conversation))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ReplyDeadlineAt.Before(result[j].ReplyDeadlineAt) })
	return result, memoryError(ctx)
}
func (m *Memory) ExpiredConversations(ctx context.Context, now time.Time) ([]domain.Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.Conversation, 0)
	for _, conversation := range m.conversations {
		if conversation.Status != domain.StatusClosed && !conversation.ReplyDeadlineAt.After(now.UTC()) {
			result = append(result, copyConversation(conversation))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	if len(result) > 500 {
		result = result[:500]
	}
	return result, memoryError(ctx)
}

func (m *Memory) ExpiredTickets(ctx context.Context, now time.Time) ([]domain.Ticket, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]domain.Ticket, 0)
	for _, ticket := range m.tickets {
		if ticket.DueAt != nil && !ticket.DueAt.After(now) && ticket.Status != "resolved" && ticket.Status != "closed" {
			result = append(result, copyTicket(ticket))
		}
	}
	return result, memoryError(ctx)
}
func (m *Memory) Report(ctx context.Context) (Report, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var report Report
	var responseSeconds int64
	var responded int64
	for _, conversation := range m.conversations {
		report.NewConversations++
		if conversation.Status == domain.StatusClosed {
			report.ClosedConversations++
		}
		if conversation.FirstResponseAt != nil {
			responseSeconds += int64(conversation.FirstResponseAt.Sub(conversation.CreatedAt) / time.Second)
			responded++
		}
	}
	for alert := range m.slaAlerts {
		if alert.kind == "expired" {
			report.SLAExpired++
		}
	}
	if responded > 0 {
		report.AverageFirstResponseSeconds = float64(responseSeconds) / float64(responded)
	}
	return report, memoryError(ctx)
}

var _ Store = (*Memory)(nil)
