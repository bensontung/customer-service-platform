CREATE TABLE channel_account (
  id VARCHAR(32) PRIMARY KEY,
  account_key VARCHAR(64) NOT NULL UNIQUE,
  channel VARCHAR(32) NOT NULL,
  app_id VARCHAR(128) NOT NULL,
  token_secret_ref VARCHAR(255) NOT NULL,
  aes_key_secret_ref VARCHAR(255) NOT NULL,
  app_secret_ref VARCHAR(255) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'active',
  created_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_channel_app (channel, app_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE external_customer (
  id VARCHAR(32) PRIMARY KEY,
  channel VARCHAR(32) NOT NULL,
  app_id VARCHAR(128) NOT NULL,
  openid VARCHAR(128) NOT NULL,
  unionid VARCHAR(128) NOT NULL DEFAULT '',
  nickname VARCHAR(255) NOT NULL DEFAULT '',
  tags_json JSON NOT NULL,
  note TEXT,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_customer_identity (channel, app_id, openid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE agent (
  id VARCHAR(32) PRIMARY KEY,
  dingtalk_user_id VARCHAR(128) NOT NULL UNIQUE,
  name VARCHAR(128) NOT NULL,
  role VARCHAR(32) NOT NULL,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE team (
  id VARCHAR(32) PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  created_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE team_agent (
  team_id VARCHAR(32) NOT NULL,
  agent_id VARCHAR(32) NOT NULL,
  PRIMARY KEY (team_id, agent_id),
  CONSTRAINT fk_team_agent_team FOREIGN KEY (team_id) REFERENCES team(id),
  CONSTRAINT fk_team_agent_agent FOREIGN KEY (agent_id) REFERENCES agent(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
