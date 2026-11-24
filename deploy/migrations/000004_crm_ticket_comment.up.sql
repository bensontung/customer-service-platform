CREATE TABLE article_comment (
  id VARCHAR(32) PRIMARY KEY,
  channel_account_id VARCHAR(32) NOT NULL,
  msg_data_id VARCHAR(191) NOT NULL,
  article_index INT NOT NULL DEFAULT 0,
  comment_id VARCHAR(191) NOT NULL,
  author_id VARCHAR(128) NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  status VARCHAR(32) NOT NULL,
  reply_content TEXT,
  synced_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_article_comment (channel_account_id, msg_data_id, comment_id),
  KEY idx_comment_status (channel_account_id, status, synced_at),
  CONSTRAINT fk_comment_account FOREIGN KEY (channel_account_id) REFERENCES channel_account(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ticket (
  id VARCHAR(32) PRIMARY KEY,
  customer_id VARCHAR(32) NOT NULL DEFAULT '',
  conversation_id VARCHAR(32) NOT NULL DEFAULT '',
  comment_id VARCHAR(32) NOT NULL DEFAULT '',
  title VARCHAR(255) NOT NULL,
  description TEXT,
  status VARCHAR(32) NOT NULL,
  priority VARCHAR(16) NOT NULL,
  assignee_id VARCHAR(32) NOT NULL DEFAULT '',
  due_at DATETIME(3) NULL,
  version BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  KEY idx_ticket_queue (assignee_id, status, due_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ticket_event (
  id VARCHAR(32) PRIMARY KEY,
  ticket_id VARCHAR(32) NOT NULL,
  event_type VARCHAR(64) NOT NULL,
  actor_id VARCHAR(32) NOT NULL,
  payload_json JSON NOT NULL,
  created_at DATETIME(3) NOT NULL,
  KEY idx_ticket_event (ticket_id, created_at),
  CONSTRAINT fk_ticket_event_ticket FOREIGN KEY (ticket_id) REFERENCES ticket(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE audit_log (
  id VARCHAR(32) PRIMARY KEY,
  actor_id VARCHAR(32) NOT NULL,
  action VARCHAR(64) NOT NULL,
  resource_type VARCHAR(32) NOT NULL,
  resource_id VARCHAR(32) NOT NULL,
  detail_json JSON NOT NULL,
  created_at DATETIME(3) NOT NULL,
  KEY idx_audit_resource (resource_type, resource_id, created_at),
  KEY idx_audit_actor (actor_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
