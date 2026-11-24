CREATE TABLE conversation (
  id VARCHAR(32) PRIMARY KEY,
  source VARCHAR(32) NOT NULL,
  channel_account_id VARCHAR(32) NOT NULL,
  customer_id VARCHAR(32) NOT NULL,
  status VARCHAR(24) NOT NULL,
  assignee_id VARCHAR(32) NOT NULL DEFAULT '',
  team_id VARCHAR(32) NOT NULL DEFAULT '',
  version BIGINT NOT NULL DEFAULT 0,
  last_inbound_at DATETIME(3) NOT NULL,
  reply_deadline_at DATETIME(3) NOT NULL,
  replies_in_window INT NOT NULL DEFAULT 0,
  first_response_at DATETIME(3) NULL,
  sla_escalated_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  KEY idx_conversation_queue (assignee_id, status, last_inbound_at),
  KEY idx_conversation_customer (customer_id, created_at),
  KEY idx_conversation_sla (status, reply_deadline_at),
  CONSTRAINT fk_conversation_account FOREIGN KEY (channel_account_id) REFERENCES channel_account(id),
  CONSTRAINT fk_conversation_customer FOREIGN KEY (customer_id) REFERENCES external_customer(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE message (
  id VARCHAR(32) PRIMARY KEY,
  conversation_id VARCHAR(32) NULL,
  channel_account_id VARCHAR(32) NOT NULL,
  channel VARCHAR(32) NOT NULL,
  direction VARCHAR(16) NOT NULL,
  channel_msg_id VARCHAR(191) NOT NULL,
  msg_type VARCHAR(32) NOT NULL,
  content_json JSON NOT NULL,
  status VARCHAR(24) NOT NULL,
  error_code VARCHAR(64) NOT NULL DEFAULT '',
  error_message VARCHAR(512) NOT NULL DEFAULT '',
  retry_count INT NOT NULL DEFAULT 0,
  next_retry_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_message_external (channel_account_id, channel_msg_id),
  KEY idx_message_timeline (conversation_id, created_at),
  KEY idx_message_retry (status, next_retry_at),
  CONSTRAINT fk_message_conversation FOREIGN KEY (conversation_id) REFERENCES conversation(id),
  CONSTRAINT fk_message_account FOREIGN KEY (channel_account_id) REFERENCES channel_account(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE inbox_dedup (
  source VARCHAR(64) NOT NULL,
  event_id VARCHAR(191) NOT NULL,
  payload_hash CHAR(64) NOT NULL,
  received_at DATETIME(3) NOT NULL,
  PRIMARY KEY (source, event_id),
  KEY idx_inbox_retention (received_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE outbox_event (
  id VARCHAR(32) PRIMARY KEY,
  event_type VARCHAR(64) NOT NULL,
  aggregate_id VARCHAR(32) NOT NULL,
  payload_json JSON NOT NULL,
  attempts INT NOT NULL DEFAULT 0,
  available_at DATETIME(3) NOT NULL,
  published_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  KEY idx_outbox_pending (published_at, available_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
