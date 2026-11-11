CREATE TABLE IF NOT EXISTS github_accounts (
 id uuid PRIMARY KEY, name text NOT NULL, login text NOT NULL, token_ciphertext text NOT NULL,
 token_suffix text NOT NULL, active boolean NOT NULL DEFAULT true, expires_at timestamptz,
 verified_at timestamptz, last_error text NOT NULL DEFAULT '', revision bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS backup_targets (
 id uuid PRIMARY KEY, engineering_id uuid NOT NULL REFERENCES engineering(id), account_id uuid NOT NULL REFERENCES github_accounts(id),
 owner text NOT NULL, repo text NOT NULL, auto_sync boolean NOT NULL DEFAULT true, enabled boolean NOT NULL DEFAULT true,
 revision bigint NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(engineering_id,owner,repo)
);
CREATE TABLE IF NOT EXISTS backup_jobs (
 id uuid PRIMARY KEY, target_id uuid NOT NULL REFERENCES backup_targets(id), version_id uuid NOT NULL REFERENCES engineering_versions(id),
 kind text NOT NULL CHECK(kind IN ('sync','restore')), status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','ready','failed')),
 source_job_id uuid REFERENCES backup_jobs(id), error text NOT NULL DEFAULT '', release_url text NOT NULL DEFAULT '',
 release_id bigint, zip_sha256 text NOT NULL DEFAULT '', manifest jsonb, remote_assets jsonb NOT NULL DEFAULT '{}',
 lease_owner text NOT NULL DEFAULT '', lease_expires_at timestamptz, attempts integer NOT NULL DEFAULT 0,
 verified_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), finished_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS backup_sync_version_target ON backup_jobs(target_id,version_id) WHERE kind='sync';
CREATE INDEX IF NOT EXISTS backup_jobs_pending ON backup_jobs(status,created_at);
CREATE INDEX IF NOT EXISTS backup_targets_engineering ON backup_targets(engineering_id);
