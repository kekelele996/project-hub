-- 标签归属由项目迁移到工程池工程；工程标签与工程的当前版本无关。
CREATE TABLE IF NOT EXISTS engineering_tags (
 engineering_id uuid NOT NULL REFERENCES engineering(id) ON DELETE CASCADE,
 tag_id uuid NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(engineering_id, tag_id)
);
CREATE INDEX IF NOT EXISTS engineering_tags_tag ON engineering_tags(tag_id, engineering_id);
-- 旧 project_tags（v1 误方案）若存在，把标签迁移到项目引用的工程上，再删除。
DO $$
BEGIN
  IF to_regclass('project_tags') IS NOT NULL THEN
    INSERT INTO engineering_tags(engineering_id, tag_id)
    SELECT DISTINCT pe.engineering_id, pt.tag_id
    FROM project_tags pt JOIN project_engineerings pe ON pe.project_id = pt.project_id
    ON CONFLICT DO NOTHING;
    DROP TABLE project_tags;
  END IF;
END $$;
