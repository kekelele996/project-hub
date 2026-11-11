ALTER TABLE backup_targets ADD COLUMN IF NOT EXISTS managed boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX IF NOT EXISTS backup_managed_engineering_account ON backup_targets(engineering_id,account_id) WHERE managed;
