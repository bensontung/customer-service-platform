CREATE TABLE shift (
  id VARCHAR(32) PRIMARY KEY,
  agent_id VARCHAR(32) NOT NULL DEFAULT '',
  team_id VARCHAR(32) NOT NULL DEFAULT '',
  start_at DATETIME(3) NOT NULL,
  end_at DATETIME(3) NOT NULL,
  state VARCHAR(24) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  KEY idx_shift_agent (agent_id, state, start_at, end_at),
  KEY idx_shift_team (team_id, state, start_at, end_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE routing_rule (
  id VARCHAR(32) PRIMARY KEY,
  priority INT NOT NULL,
  match_json JSON NOT NULL,
  target_team_id VARCHAR(32) NOT NULL,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at DATETIME(3) NOT NULL,
  KEY idx_routing_priority (active, priority)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE sla_policy (
  id VARCHAR(32) PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  first_response_seconds INT NOT NULL,
  subsequent_response_seconds INT NOT NULL,
  ticket_resolution_seconds INT NOT NULL,
  warning_seconds INT NOT NULL,
  active BOOLEAN NOT NULL DEFAULT TRUE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE sla_alert (
  id VARCHAR(32) PRIMARY KEY,
  resource_type VARCHAR(32) NOT NULL,
  resource_id VARCHAR(32) NOT NULL,
  kind VARCHAR(32) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_sla_once (resource_type, resource_id, kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
