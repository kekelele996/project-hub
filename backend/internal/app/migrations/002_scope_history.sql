ALTER TABLE project_engineerings ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;
ALTER TABLE claims ADD COLUMN IF NOT EXISTS version_id uuid REFERENCES engineering_versions(id);
UPDATE claims SET version_id=pe.version_id FROM project_engineerings pe WHERE pe.id=claims.project_engineering_id AND claims.version_id IS NULL;
