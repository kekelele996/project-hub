CREATE TABLE IF NOT EXISTS users (
 id uuid PRIMARY KEY, username text NOT NULL UNIQUE, name text NOT NULL, password_hash text NOT NULL,
 role text NOT NULL CHECK(role IN ('admin','member')), active boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS sessions (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id), token_hash text NOT NULL UNIQUE,
 csrf_token text NOT NULL, expires_at timestamptz NOT NULL, revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS engineering (
 id uuid PRIMARY KEY, name text NOT NULL, description text NOT NULL DEFAULT '', archived boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS engineering_versions (
 id uuid PRIMARY KEY, engineering_id uuid NOT NULL REFERENCES engineering(id), version_number integer NOT NULL,
 tech_stack jsonb NOT NULL DEFAULT '[]', status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','ready','failed')),
 source_file_count integer NOT NULL DEFAULT 0, total_file_count integer NOT NULL DEFAULT 0,
 languages jsonb NOT NULL DEFAULT '[]', zip_sha256 text NOT NULL DEFAULT '', zip_size bigint NOT NULL DEFAULT 0,
 zip_path text NOT NULL, github_url text NOT NULL DEFAULT '', github_ref text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(engineering_id,version_number)
);
CREATE TABLE IF NOT EXISTS engineering_assets (
 id uuid PRIMARY KEY, version_id uuid NOT NULL REFERENCES engineering_versions(id), kind text NOT NULL CHECK(kind IN ('startup','feature')),
 path text NOT NULL, mime_type text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS analysis_jobs (
 id uuid PRIMARY KEY, version_id uuid NOT NULL REFERENCES engineering_versions(id), status text NOT NULL DEFAULT 'pending',
 error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS projects (
 id uuid PRIMARY KEY, name text NOT NULL, description text NOT NULL DEFAULT '', start_at timestamptz NOT NULL,
 end_at timestamptz NOT NULL, tech_stack jsonb NOT NULL DEFAULT '[]', min_files integer NOT NULL DEFAULT 0,
 max_files integer NOT NULL DEFAULT 0, status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','paused','archived')),
 created_at timestamptz NOT NULL DEFAULT now(), CHECK(end_at >= start_at), CHECK(min_files >= 0 AND max_files >= 0)
);
CREATE TABLE IF NOT EXISTS project_engineerings (
 id uuid PRIMARY KEY, project_id uuid NOT NULL REFERENCES projects(id), engineering_id uuid NOT NULL REFERENCES engineering(id),
 version_id uuid NOT NULL REFERENCES engineering_versions(id), active boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(project_id,engineering_id)
);
CREATE TABLE IF NOT EXISTS project_members (
 project_id uuid NOT NULL REFERENCES projects(id), user_id uuid NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(project_id,user_id)
);
CREATE TABLE IF NOT EXISTS claims (
 id uuid PRIMARY KEY, project_engineering_id uuid NOT NULL REFERENCES project_engineerings(id),
 user_id uuid NOT NULL REFERENCES users(id), claimed_at timestamptz NOT NULL DEFAULT now(),
 released_at timestamptz, release_reason text NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS claims_one_active ON claims(project_engineering_id) WHERE released_at IS NULL;
CREATE TABLE IF NOT EXISTS api_tokens (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id), name text NOT NULL, prefix text NOT NULL,
 token_hash text NOT NULL UNIQUE, created_at timestamptz NOT NULL DEFAULT now(), last_used_at timestamptz, revoked_at timestamptz
);
CREATE TABLE IF NOT EXISTS audit_events (
 id uuid PRIMARY KEY, actor_id uuid REFERENCES users(id), action text NOT NULL, target_type text NOT NULL, target_id uuid,
 project_id uuid REFERENCES projects(id), engineering_id uuid REFERENCES engineering(id), details jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS project_members_user ON project_members(user_id);
CREATE INDEX IF NOT EXISTS versions_engineering ON engineering_versions(engineering_id,version_number DESC);
CREATE INDEX IF NOT EXISTS audit_created ON audit_events(created_at DESC);
