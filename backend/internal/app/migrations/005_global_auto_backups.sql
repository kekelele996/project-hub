ALTER TABLE engineering ADD COLUMN IF NOT EXISTS backup_repo_name text NOT NULL DEFAULT 'engineering';
UPDATE engineering e SET backup_repo_name=coalesce((
 SELECT substring(t.repo from 4 for char_length(t.repo)-36)
 FROM backup_targets t WHERE t.engineering_id=e.id AND t.managed
 AND t.repo ~ '^ph-[a-z0-9][a-z0-9-]{1,46}[a-z0-9]-[0-9a-f]{32}$'
 ORDER BY t.created_at,t.id LIMIT 1
),'engineering') WHERE e.backup_repo_name='engineering';
