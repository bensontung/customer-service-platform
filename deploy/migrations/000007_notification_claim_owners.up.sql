ALTER TABLE dingtalk_delivery ADD COLUMN claim_token VARCHAR(36) NOT NULL DEFAULT '' AFTER status;
ALTER TABLE dingtalk_delivery ADD COLUMN claimed_until DATETIME(3) NULL AFTER claim_token;
ALTER TABLE dingtalk_delivery ADD KEY idx_dingtalk_claim (status, claimed_until);
UPDATE dingtalk_delivery SET claim_token='' WHERE status<>'pending';
