CREATE TABLE dingtalk_delivery (
  id VARCHAR(32) PRIMARY KEY,
  conversation_id VARCHAR(32) NOT NULL,
  agent_id VARCHAR(32) NOT NULL,
  kind VARCHAR(32) NOT NULL,
  out_track_id VARCHAR(191) NOT NULL,
  task_id VARCHAR(191) NOT NULL DEFAULT '',
  status VARCHAR(24) NOT NULL,
  error_message VARCHAR(512) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_dingtalk_out_track (out_track_id),
  KEY idx_dingtalk_conversation (conversation_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
