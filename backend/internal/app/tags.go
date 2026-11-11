package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	tagNameMaxRunes    = 40
	maxEngineeringTags = 32
	maxTagFilterIDs    = 50
	tagOtherLeafName   = "其他"
)

// Tag 是 GET /api/v1/tags 返回的两级标签树节点。
type Tag struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	ParentID         *string `json:"parent_id"`
	SortOrder        int     `json:"sort_order"`
	EngineeringCount int     `json:"engineering_count"`
	Children         []Tag   `json:"children"`
}

// EngineeringTag 是挂在工程上的子方向标签（含所属领域名称）。
type EngineeringTag struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	ParentID   *string `json:"parent_id"`
	ParentName string  `json:"parent_name"`
}

func (a *App) registerTags(r chi.Router) {
	r.Get("/api/v1/tags", a.listTags)
	r.Group(func(r chi.Router) {
		r.Use(requireAdmin)
		r.Post("/api/v1/admin/tags", a.createTag)
		r.Patch("/api/v1/admin/tags/{id}", a.updateTag)
		r.Delete("/api/v1/admin/tags/{id}", a.deleteTag)
	})
}

// listTags 返回完整两级树。engineering_count 为该标签及其全部后代所关联的不同工程数。
func (a *App) listTags(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `WITH RECURSIVE subtree AS (
 SELECT t.id AS root_id,t.id AS tag_id FROM tags t
 UNION
 SELECT s.root_id,c.id FROM tags c JOIN subtree s ON c.parent_id=s.tag_id
), counted AS (
 SELECT s.root_id,count(DISTINCT et.engineering_id) AS engineering_count
 FROM subtree s JOIN engineering_tags et ON et.tag_id=s.tag_id GROUP BY s.root_id
)
SELECT t.id::text,t.name,t.parent_id::text,t.sort_order,coalesce(c.engineering_count,0)
FROM tags t LEFT JOIN counted c ON c.root_id=t.id`)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	total := 0
	roots := []Tag{}
	children := map[string][]Tag{}
	for rows.Next() {
		var tag Tag
		tag.Children = []Tag{}
		if err = rows.Scan(&tag.ID, &tag.Name, &tag.ParentID, &tag.SortOrder, &tag.EngineeringCount); err != nil {
			dbError(w, err)
			return
		}
		total++
		if tag.ParentID == nil {
			roots = append(roots, tag)
		} else {
			children[*tag.ParentID] = append(children[*tag.ParentID], tag)
		}
	}
	if err = rows.Err(); err != nil {
		dbError(w, err)
		return
	}
	sort.Slice(roots, func(i, j int) bool { return tagLess(roots[i], roots[j]) })
	for i := range roots {
		kids := children[roots[i].ID]
		if kids == nil {
			// map 查找无子方向时返回 nil；JSON 契约要求 children 始终是数组。
			kids = []Tag{}
		}
		sort.Slice(kids, func(i, j int) bool {
			leftOther, rightOther := kids[i].Name == tagOtherLeafName, kids[j].Name == tagOtherLeafName
			if leftOther != rightOther {
				return !leftOther
			}
			return tagLess(kids[i], kids[j])
		})
		roots[i].Children = kids
	}
	items := make([]any, 0, len(roots))
	for _, tag := range roots {
		items = append(items, tag)
	}
	listJSON(w, items, total)
}

func tagLess(left, right Tag) bool {
	if left.SortOrder != right.SortOrder {
		return left.SortOrder < right.SortOrder
	}
	return left.Name < right.Name
}

func tagNameProblem(raw string) (string, string) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", "标签名称不能为空"
	}
	if utf8.RuneCountInString(name) > tagNameMaxRunes {
		return "", "标签名称不能超过40个字符"
	}
	return name, ""
}

func tagConflictMessage(parentID *string) string {
	if parentID == nil {
		return "已存在同名所属领域"
	}
	return "同一所属领域下已存在同名子方向"
}

// tagReader 同时接受连接池与事务，用于读写单条标签。
type tagReader interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// tagQueryer 同时接受连接池与事务，用于读取多行标签。
type tagQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// loadTag 读取单个标签（含后代聚合 engineering_count，children 固定为 []）。
func loadTag(ctx context.Context, reader tagReader, id string) (Tag, error) {
	tag := Tag{Children: []Tag{}}
	err := reader.QueryRow(ctx, `WITH RECURSIVE subtree AS (
 SELECT id FROM tags WHERE id=$1
 UNION
 SELECT c.id FROM tags c JOIN subtree s ON c.parent_id=s.id
)
SELECT t.id::text,t.name,t.parent_id::text,t.sort_order,
 (SELECT count(DISTINCT et.engineering_id) FROM engineering_tags et JOIN subtree s ON s.id=et.tag_id)
FROM tags t WHERE t.id=$1`, id).Scan(&tag.ID, &tag.Name, &tag.ParentID, &tag.SortOrder, &tag.EngineeringCount)
	return tag, err
}

// loadEngineeringTags 读取工程上的标签，按领域 sort_order、子方向「其他最后」排序。
func loadEngineeringTags(ctx context.Context, reader tagQueryer, engineeringID string) ([]EngineeringTag, error) {
	rows, err := reader.Query(ctx, `SELECT t.id::text,t.name,t.parent_id::text,coalesce(p.name,'')
FROM engineering_tags et JOIN tags t ON t.id=et.tag_id LEFT JOIN tags p ON p.id=t.parent_id
WHERE et.engineering_id=$1
ORDER BY coalesce(p.sort_order,t.sort_order),CASE WHEN t.name='其他' THEN 1 ELSE 0 END,t.sort_order,t.name`, engineeringID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []EngineeringTag{}
	for rows.Next() {
		var tag EngineeringTag
		if err = rows.Scan(&tag.ID, &tag.Name, &tag.ParentID, &tag.ParentName); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (a *App) createTag(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string  `json:"name"`
		ParentID  *string `json:"parent_id"`
		SortOrder *int    `json:"sort_order"`
	}
	if !decode(w, r, &in) {
		return
	}
	name, message := tagNameProblem(in.Name)
	if message != "" {
		fail(w, 400, message)
		return
	}
	if in.ParentID != nil {
		parent := strings.ToLower(strings.TrimSpace(*in.ParentID))
		if parent == "" {
			in.ParentID = nil
		} else if !validID(parent) {
			fail(w, 400, "所属领域不存在")
			return
		} else {
			in.ParentID = &parent
		}
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if in.ParentID != nil {
		var grandparent *string
		err = tx.QueryRow(r.Context(), `SELECT parent_id::text FROM tags WHERE id=$1`, *in.ParentID).Scan(&grandparent)
		if err == pgx.ErrNoRows {
			fail(w, 400, "所属领域不存在")
			return
		}
		if err != nil {
			dbError(w, err)
			return
		}
		if grandparent != nil {
			fail(w, 400, "标签树最多两级")
			return
		}
	}
	tag := Tag{ID: newID(), Name: name, ParentID: in.ParentID, Children: []Tag{}}
	if in.SortOrder != nil {
		tag.SortOrder = *in.SortOrder
	} else if err = tx.QueryRow(r.Context(), `SELECT coalesce(max(sort_order),0)+10 FROM tags WHERE parent_id IS NOT DISTINCT FROM $1::uuid`, nullableTagID(in.ParentID)).Scan(&tag.SortOrder); err != nil {
		dbError(w, err)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO tags(id,parent_id,name,sort_order) VALUES($1,$2,$3,$4)`, tag.ID, tag.ParentID, tag.Name, tag.SortOrder)
	if err != nil {
		if tagUniqueViolation(err) {
			fail(w, 409, tagConflictMessage(tag.ParentID))
			return
		}
		dbError(w, err)
		return
	}
	err = a.audit(r.Context(), tx, userFrom(r).ID, "tag.create", "tag", tag.ID, "", "", map[string]any{"name": tag.Name, "parent_id": tag.ParentID})
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 201, tag)
}

func (a *App) updateTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "标签不存在")
		return
	}
	var in struct {
		Name      *string         `json:"name"`
		SortOrder *int            `json:"sort_order"`
		ParentID  json.RawMessage `json:"parent_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.ParentID) > 0 && string(in.ParentID) != "null" {
		fail(w, 400, "不支持移动标签所属领域")
		return
	}
	if in.Name != nil {
		name, message := tagNameProblem(*in.Name)
		if message != "" {
			fail(w, 400, message)
			return
		}
		in.Name = &name
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var current Tag
	err = tx.QueryRow(r.Context(), `SELECT name,parent_id::text,sort_order FROM tags WHERE id=$1 FOR UPDATE`, id).Scan(&current.Name, &current.ParentID, &current.SortOrder)
	if err == pgx.ErrNoRows {
		fail(w, 404, "标签不存在")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	name, order := current.Name, current.SortOrder
	if in.Name != nil {
		name = *in.Name
	}
	if in.SortOrder != nil {
		order = *in.SortOrder
	}
	_, err = tx.Exec(r.Context(), `UPDATE tags SET name=$2,sort_order=$3 WHERE id=$1`, id, name, order)
	if err != nil {
		if tagUniqueViolation(err) {
			fail(w, 409, tagConflictMessage(current.ParentID))
			return
		}
		dbError(w, err)
		return
	}
	details := map[string]any{}
	if in.Name != nil {
		details["name"] = *in.Name
	}
	if in.SortOrder != nil {
		details["sort_order"] = *in.SortOrder
	}
	if err = a.audit(r.Context(), tx, userFrom(r).ID, "tag.update", "tag", id, "", "", details); err != nil {
		dbError(w, err)
		return
	}
	tag, err := loadTag(r.Context(), tx, id)
	if err != nil {
		dbError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, tag)
}

func (a *App) deleteTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "标签不存在")
		return
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var name string
	var parentID *string
	err = tx.QueryRow(r.Context(), `SELECT name,parent_id::text FROM tags WHERE id=$1 FOR UPDATE`, id).Scan(&name, &parentID)
	if err == pgx.ErrNoRows {
		fail(w, 404, "标签不存在")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	var hasChildren bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM tags WHERE parent_id=$1)`, id).Scan(&hasChildren); err != nil {
		dbError(w, err)
		return
	}
	if hasChildren {
		fail(w, 409, "请先删除该领域下的子方向")
		return
	}
	var used bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM engineering_tags WHERE tag_id=$1)`, id).Scan(&used); err != nil {
		dbError(w, err)
		return
	}
	if used {
		fail(w, 409, "该工程正使用此标签，无法删除")
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM tags WHERE id=$1`, id); err != nil {
		dbError(w, err)
		return
	}
	err = a.audit(r.Context(), tx, userFrom(r).ID, "tag.delete", "tag", id, "", "", map[string]any{"name": name, "parent_id": parentID})
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 204, nil)
}

func tagUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// nullableTagID 把可选父标签 ID 转为 SQL 参数。
func nullableTagID(id *string) any {
	if id == nil {
		return nil
	}
	return *id
}

// parseTagFilterIDs 展开重复参数与逗号分隔的 tag_ids；空值视为不过滤。
// 返回 ok=false 表示存在无效 UUID 或超过数量上限。
func parseTagFilterIDs(values []string) ([]string, bool) {
	ids := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.ToLower(strings.TrimSpace(item))
			if item == "" {
				continue
			}
			if !validID(item) {
				return nil, false
			}
			if seen[item] {
				continue
			}
			seen[item] = true
			ids = append(ids, item)
		}
	}
	if len(ids) > maxTagFilterIDs {
		return nil, false
	}
	return ids, true
}

// tagFilterCSV 校验 tag_ids 查询参数并返回逗号分隔的 UUID CSV（空串表示不过滤）。
// ok=false 表示请求不合法；err 表示数据库错误。
func (a *App) tagFilterCSV(ctx context.Context, r *http.Request) (string, bool, error) {
	ids, ok := parseTagFilterIDs(r.URL.Query()["tag_ids"])
	if !ok {
		return "", false, nil
	}
	if len(ids) == 0 {
		return "", true, nil
	}
	rows, err := a.DB.Query(ctx, `SELECT id::text FROM tags WHERE id=ANY(string_to_array($1,',')::uuid[])`, strings.Join(ids, ","))
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	existing := 0
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return "", false, err
		}
		existing++
	}
	if err = rows.Err(); err != nil {
		return "", false, err
	}
	if existing != len(ids) {
		return "", false, nil
	}
	return strings.Join(ids, ","), true, nil
}

// normalizeEngineeringTagIDs 校验并去重工程的 tag_ids：只允许存在的叶子标签，最多 32 个。
// 返回非空 message 表示请求不合法；err 表示数据库错误。
func normalizeEngineeringTagIDs(ctx context.Context, tx pgx.Tx, ids []string) ([]string, string, error) {
	result := []string{}
	seen := map[string]bool{}
	for _, raw := range ids {
		id := strings.ToLower(strings.TrimSpace(raw))
		if !validID(id) {
			return nil, "工程标签无效", nil
		}
		if seen[id] {
			continue
		}
		var parentID *string
		err := tx.QueryRow(ctx, `SELECT parent_id::text FROM tags WHERE id=$1`, id).Scan(&parentID)
		if err == pgx.ErrNoRows {
			return nil, "工程标签无效", nil
		}
		if err != nil {
			return nil, "", err
		}
		if parentID == nil {
			return nil, "只能为工程选择子方向标签", nil
		}
		seen[id] = true
		result = append(result, id)
	}
	if len(result) > maxEngineeringTags {
		return nil, "工程标签最多 32 个", nil
	}
	return result, "", nil
}

// setEngineeringTags 在事务内整体替换工程标签。
func setEngineeringTags(ctx context.Context, tx pgx.Tx, engineeringID string, ids []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM engineering_tags WHERE engineering_id=$1`, engineeringID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO engineering_tags(engineering_id,tag_id) VALUES($1,$2)`, engineeringID, id); err != nil {
			return err
		}
	}
	return nil
}
