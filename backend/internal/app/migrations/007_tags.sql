CREATE TABLE IF NOT EXISTS tags (
 id uuid PRIMARY KEY, parent_id uuid REFERENCES tags(id) ON DELETE RESTRICT,
 name text NOT NULL, sort_order integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now()
);
-- 根标签名称唯一；同一父标签下名称唯一；均按 btrim + lower 去重
CREATE UNIQUE INDEX IF NOT EXISTS tags_root_name ON tags(lower(btrim(name))) WHERE parent_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS tags_child_name ON tags(parent_id, lower(btrim(name))) WHERE parent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS tags_parent_sort ON tags(parent_id, sort_order);
CREATE TABLE IF NOT EXISTS project_tags (
 project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 tag_id uuid NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(project_id, tag_id)
);
CREATE INDEX IF NOT EXISTS project_tags_tag ON project_tags(tag_id, project_id);
-- 固定 UUID 的种子标签树：安全 / 游戏、图形与多媒体 / 数据工程，每个领域都含「其他」子方向。
INSERT INTO tags(id,parent_id,name,sort_order) VALUES
 ('00000000-0000-4000-8000-000000000010'::uuid,NULL,'安全',10),
 ('00000000-0000-4000-8000-000000000011'::uuid,'00000000-0000-4000-8000-000000000010'::uuid,'漏洞检测与攻防',10),
 ('00000000-0000-4000-8000-000000000012'::uuid,'00000000-0000-4000-8000-000000000010'::uuid,'其他',90),
 ('00000000-0000-4000-8000-000000000020'::uuid,NULL,'游戏、图形与多媒体',20),
 ('00000000-0000-4000-8000-000000000021'::uuid,'00000000-0000-4000-8000-000000000020'::uuid,'音视频处理',10),
 ('00000000-0000-4000-8000-000000000022'::uuid,'00000000-0000-4000-8000-000000000020'::uuid,'图形渲染',20),
 ('00000000-0000-4000-8000-000000000023'::uuid,'00000000-0000-4000-8000-000000000020'::uuid,'其他',90),
 ('00000000-0000-4000-8000-000000000030'::uuid,NULL,'数据工程',30),
 ('00000000-0000-4000-8000-000000000031'::uuid,'00000000-0000-4000-8000-000000000030'::uuid,'数据采集、爬虫与 RPA',10),
 ('00000000-0000-4000-8000-000000000032'::uuid,'00000000-0000-4000-8000-000000000030'::uuid,'其他',90)
ON CONFLICT (id) DO NOTHING;
