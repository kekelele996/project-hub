package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// ---------- 标签/工程标签测试辅助 ----------

func (f *testFixture) tagResponse(t *testing.T, identity testIdentity) testResult {
	t.Helper()
	result := f.request(t, identity, "GET", "/api/v1/tags", nil)
	expectStatus(t, result, 200)
	return result
}

func tagItems(t *testing.T, body map[string]any) []any {
	t.Helper()
	items, ok := body["items"].([]any)
	if !ok || items == nil {
		t.Fatalf("标签树 items 格式错误: %v", body)
	}
	return items
}

func tagChildren(t *testing.T, node map[string]any) []any {
	t.Helper()
	children, ok := node["children"].([]any)
	if !ok || children == nil {
		t.Fatalf("标签 children 应为数组: %v", node)
	}
	return children
}

func findTag(t *testing.T, items []any, parentName, name string) map[string]any {
	t.Helper()
	for _, raw := range items {
		node, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("标签节点格式错误: %v", raw)
		}
		if parentName == "" {
			if node["name"] == name {
				return node
			}
			continue
		}
		if node["name"] != parentName {
			continue
		}
		for _, childRaw := range tagChildren(t, node) {
			child := childRaw.(map[string]any)
			if child["name"] == name {
				return child
			}
		}
	}
	t.Fatalf("标签 %q/%q 不存在", parentName, name)
	return nil
}

func tagID(t *testing.T, items []any, parentName, name string) string {
	t.Helper()
	return findTag(t, items, parentName, name)["id"].(string)
}

func engineeringCountOf(t *testing.T, items []any, parentName, name string) float64 {
	t.Helper()
	value, ok := findTag(t, items, parentName, name)["engineering_count"].(float64)
	if !ok {
		t.Fatalf("engineering_count 不是数字: %v", findTag(t, items, parentName, name))
	}
	return value
}

func (f *testFixture) createTag(t *testing.T, body map[string]any) testResult {
	t.Helper()
	return f.request(t, f.admin, "POST", "/api/v1/admin/tags", body)
}

func (f *testFixture) mustCreateTag(t *testing.T, body map[string]any) string {
	t.Helper()
	result := f.createTag(t, body)
	expectStatus(t, result, 201)
	id, _ := result.body["id"].(string)
	if id == "" {
		t.Fatalf("创建标签缺少 id: %v", result.body)
	}
	return id
}

func expectError(t *testing.T, result testResult, status int, message string) {
	t.Helper()
	expectStatus(t, result, status)
	if result.body["error"] != message {
		t.Fatalf("error=%v expected=%q body=%v", result.body["error"], message, result.body)
	}
}

func (f *testFixture) tagTree(t *testing.T, identity testIdentity) []any {
	t.Helper()
	return tagItems(t, f.tagResponse(t, identity).body)
}

// insertTag 直接写入标签行（用于批量构造筛选上限等测试数据）。
func (f *testFixture) insertTag(t *testing.T, parentID, name string, sortOrder int) string {
	t.Helper()
	id := newID()
	if _, err := f.a.DB.Exec(context.Background(), `INSERT INTO tags(id,parent_id,name,sort_order) VALUES($1,$2::uuid,$3,$4)`, id, nullable(parentID), name, sortOrder); err != nil {
		t.Fatal(err)
	}
	return id
}

// insertEngineering 直接创建工程与一个 ready 版本（用于需要多个工程的筛选/计数测试）。
func (f *testFixture) insertEngineering(t *testing.T, name string) string {
	t.Helper()
	id, version := newID(), newID()
	ctx := context.Background()
	if _, err := f.a.DB.Exec(ctx, `INSERT INTO engineering(id,name) VALUES($1,$2)`, id, name); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.DB.Exec(ctx, `INSERT INTO engineering_versions(id,engineering_id,version_number,status,zip_path) VALUES($1,$2,1,'ready','fixture.zip')`, version, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *testFixture) patchEngineering(t *testing.T, engineeringID string, body map[string]any) testResult {
	t.Helper()
	return f.request(t, f.admin, "PATCH", "/api/v1/admin/engineering/"+engineeringID, body)
}

func engineeringTags(t *testing.T, body map[string]any) []any {
	t.Helper()
	value, ok := body["tags"]
	if !ok {
		t.Fatalf("工程响应缺少 tags 字段: %v", body)
	}
	tags, ok := value.([]any)
	if !ok || tags == nil {
		t.Fatalf("工程 tags 应为数组（无标签为 []）: %v", body)
	}
	return tags
}

// detailTags 读取 GET /api/v1/admin/engineering/{id} 中 engineering 对象的 tags。
func (f *testFixture) detailTags(t *testing.T, engineeringID string) []any {
	t.Helper()
	result := f.request(t, f.admin, "GET", "/api/v1/admin/engineering/"+engineeringID, nil)
	expectStatus(t, result, 200)
	engineering, ok := result.body["engineering"].(map[string]any)
	if !ok {
		t.Fatalf("工程详情格式错误: %v", result.body)
	}
	return engineeringTags(t, engineering)
}

func listEngineeringItem(t *testing.T, result testResult, engineeringID string) map[string]any {
	t.Helper()
	expectStatus(t, result, 200)
	for _, raw := range result.body["items"].([]any) {
		item := raw.(map[string]any)
		if item["engineering_id"] == engineeringID {
			return item
		}
	}
	t.Fatalf("工程池列表缺少工程 %s: %v", engineeringID, result.body)
	return nil
}

func queryEscape(values url.Values) string {
	return values.Encode()
}

// ---------- 种子树 / 排序 / 计数 / 空领域 ----------

func TestPostgresTagSeedTree(t *testing.T) {
	f := integrationFixture(t)
	type treeNode struct {
		name     string
		sort     float64
		children []string
	}
	expected := []treeNode{
		{"安全", 10, []string{"漏洞检测与攻防", "其他"}},
		{"游戏、图形与多媒体", 20, []string{"音视频处理", "图形渲染", "其他"}},
		{"数据工程", 30, []string{"数据采集、爬虫与 RPA", "其他"}},
	}
	// 管理员与普通成员都能读取同一棵全局标签树；engineering_count 初始为 0。
	for _, identity := range []testIdentity{f.admin, f.memberA} {
		result := f.tagResponse(t, identity)
		if result.body["total"] != float64(10) {
			t.Fatalf("标签总数=%v 期望 10", result.body["total"])
		}
		items := tagItems(t, result.body)
		if len(items) != len(expected) {
			t.Fatalf("领域数=%d 期望 %d: %v", len(items), len(expected), items)
		}
		for ix, want := range expected {
			node := items[ix].(map[string]any)
			if node["name"] != want.name || node["sort_order"] != want.sort || node["parent_id"] != nil || node["engineering_count"] != float64(0) {
				t.Fatalf("领域 %q 字段错误: %v", want.name, node)
			}
			children := tagChildren(t, node)
			if len(children) != len(want.children) {
				t.Fatalf("领域 %q 子方向数=%d 期望 %d: %v", want.name, len(children), len(want.children), children)
			}
			for cix, childName := range want.children {
				child := children[cix].(map[string]any)
				if child["name"] != childName || child["parent_id"] != node["id"] || child["engineering_count"] != float64(0) {
					t.Fatalf("子方向 %q/%q 字段错误: %v", want.name, childName, child)
				}
				if len(tagChildren(t, child)) != 0 {
					t.Fatalf("叶子标签 %q 的 children 应为 []", childName)
				}
			}
		}
	}
	// 成员只读：GET 可用，写接口 403。
	existingID := tagID(t, f.tagTree(t, f.admin), "", "安全")
	expectStatus(t, f.request(t, f.memberA, "POST", "/api/v1/admin/tags", map[string]any{"name": "成员不能建"}), 403)
	expectStatus(t, f.request(t, f.memberA, "PATCH", "/api/v1/admin/tags/"+existingID, map[string]any{"name": "成员不能改"}), 403)
	expectStatus(t, f.request(t, f.memberA, "DELETE", "/api/v1/admin/tags/"+existingID, nil), 403)
	// 「其他」永远最后：即使它的 sort_order 小于同级普通子方向。
	domainID := f.mustCreateTag(t, map[string]any{"name": "排序验证领域"})
	f.mustCreateTag(t, map[string]any{"name": "其他", "parent_id": domainID, "sort_order": 5})
	f.mustCreateTag(t, map[string]any{"name": "普通子方向", "parent_id": domainID, "sort_order": 99})
	items := f.tagTree(t, f.memberB)
	children := tagChildren(t, findTag(t, items, "", "排序验证领域"))
	if len(children) != 2 || children[0].(map[string]any)["name"] != "普通子方向" || children[1].(map[string]any)["name"] != "其他" {
		t.Fatalf("「其他」未排在子方向最后: %v", children)
	}
	// 领域按 sort_order、name 排序；同 sort_order 时按名称。
	f.mustCreateTag(t, map[string]any{"name": "排序验证领域B", "sort_order": 100})
	f.mustCreateTag(t, map[string]any{"name": "排序验证领域A", "sort_order": 100})
	items = f.tagTree(t, f.admin)
	lastA := items[len(items)-2].(map[string]any)["name"]
	lastB := items[len(items)-1].(map[string]any)["name"]
	if lastA != "排序验证领域A" || lastB != "排序验证领域B" {
		t.Fatalf("同 sort_order 领域未按名称排序: %v / %v", lastA, lastB)
	}
}

// 回归：没有子方向的领域，GET /api/v1/tags 的 children 必须是 []，不能是 null。
func TestPostgresTagEmptyDomainChildrenArray(t *testing.T) {
	f := integrationFixture(t)
	empty := f.createTag(t, map[string]any{"name": "空领域证据"})
	expectStatus(t, empty, 201)
	emptyID := empty.body["id"].(string)
	if children, ok := empty.body["children"].([]any); !ok || children == nil || len(children) != 0 {
		t.Fatalf("创建空领域响应 children 应为 []: %v", empty.body["children"])
	}
	for _, identity := range []testIdentity{f.admin, f.memberA} {
		result := f.tagResponse(t, identity)
		if result.body["total"] != float64(11) {
			t.Fatalf("标签总数=%v 期望 11", result.body["total"])
		}
		items := tagItems(t, result.body)
		var node map[string]any
		for _, raw := range items {
			if candidate := raw.(map[string]any); candidate["name"] == "空领域证据" {
				node = candidate
				break
			}
		}
		if node == nil {
			t.Fatal("GET /api/v1/tags 缺少新建的空领域")
		}
		if node["id"] != emptyID || node["engineering_count"] != float64(0) {
			t.Fatalf("空领域字段错误: %v", node)
		}
		// 必须能从 HTTP JSON 解码成 []any 且长度为 0；null 会解码为 nil 接口。
		children, ok := node["children"].([]any)
		if !ok || children == nil {
			t.Fatalf("空领域 children 必须是数组（[]），实际: %#v", node["children"])
		}
		if len(children) != 0 {
			t.Fatalf("空领域 children 长度=%d 期望 0: %v", len(children), children)
		}
		// 叶子标签的 children 同样必须是数组。
		leaf := findTag(t, items, "安全", "漏洞检测与攻防")
		leafChildren, ok := leaf["children"].([]any)
		if !ok || leafChildren == nil || len(leafChildren) != 0 {
			t.Fatalf("叶子标签 children 必须是数组（[]），实际: %#v", leaf["children"])
		}
	}
	expectStatus(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+emptyID, nil), 204)
}

// ---------- 管理员 CRUD ----------

func TestPostgresTagAdminCRUD(t *testing.T) {
	f := integrationFixture(t)
	// 未传 sort_order：根级默认 max(30)+10 = 40；名称 trim。
	domain := f.createTag(t, map[string]any{"name": "  验收领域  "})
	expectStatus(t, domain, 201)
	if domain.body["name"] != "验收领域" || domain.body["parent_id"] != nil || domain.body["sort_order"] != float64(40) || domain.body["engineering_count"] != float64(0) {
		t.Fatalf("创建领域字段错误: %v", domain.body)
	}
	if len(tagChildren(t, domain.body)) != 0 {
		t.Fatalf("新领域 children 应为 []: %v", domain.body)
	}
	domainID := domain.body["id"].(string)

	// 子方向默认 max+10（无同父级子方向时 10）。
	first := f.createTag(t, map[string]any{"name": "验收子方向", "parent_id": domainID})
	expectStatus(t, first, 201)
	if first.body["parent_id"] != domainID || first.body["sort_order"] != float64(10) {
		t.Fatalf("创建子方向字段错误: %v", first.body)
	}
	firstID := first.body["id"].(string)
	second := f.createTag(t, map[string]any{"name": "验收子方向二", "parent_id": domainID, "sort_order": 5})
	expectStatus(t, second, 201)
	if second.body["sort_order"] != float64(5) {
		t.Fatalf("显式 sort_order 未生效: %v", second.body)
	}
	// 默认排序取同级最大 sort_order + 10。
	third := f.createTag(t, map[string]any{"name": "验收子方向三", "parent_id": domainID})
	expectStatus(t, third, 201)
	if third.body["sort_order"] != float64(20) {
		t.Fatalf("默认 sort_order=%v 期望 20", third.body["sort_order"])
	}

	// 更新名称与排序；不支持移动父级。
	patched := f.request(t, f.admin, "PATCH", "/api/v1/admin/tags/"+firstID, map[string]any{"name": "验收子方向改", "sort_order": 7})
	expectStatus(t, patched, 200)
	if patched.body["name"] != "验收子方向改" || patched.body["sort_order"] != float64(7) || patched.body["parent_id"] != domainID || len(tagChildren(t, patched.body)) != 0 {
		t.Fatalf("更新标签结果错误: %v", patched.body)
	}
	expectError(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/tags/"+firstID, map[string]any{"parent_id": tagID(t, f.tagTree(t, f.admin), "", "安全")}), 400, "不支持移动标签所属领域")

	items := f.tagTree(t, f.memberA)
	children := tagChildren(t, findTag(t, items, "", "验收领域"))
	if len(children) != 3 {
		t.Fatalf("验收领域子方向数=%d 期望 3: %v", len(children), children)
	}
	// sort 5 的子方向在前。
	if children[0].(map[string]any)["name"] != "验收子方向二" || children[1].(map[string]any)["name"] != "验收子方向改" || children[2].(map[string]any)["name"] != "验收子方向三" {
		t.Fatalf("子方向排序错误: %v", children)
	}

	// 删除子方向后领域可以删除，树恢复 10 个种子标签。
	expectStatus(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+firstID, nil), 204)
	expectStatus(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+second.body["id"].(string), nil), 204)
	expectStatus(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+third.body["id"].(string), nil), 204)
	expectStatus(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+domainID, nil), 204)
	if total := f.tagResponse(t, f.admin).body["total"]; total != float64(10) {
		t.Fatalf("删除后标签总数=%v 期望 10", total)
	}
}

// ---------- 两级限制 / 重名 / 名称校验 ----------

func TestPostgresTagValidationAndConflicts(t *testing.T) {
	f := integrationFixture(t)
	tree := f.tagTree(t, f.admin)
	securityID := tagID(t, tree, "", "安全")
	vulnerabilityID := tagID(t, tree, "安全", "漏洞检测与攻防")
	gameID := tagID(t, tree, "", "游戏、图形与多媒体")

	expectError(t, f.createTag(t, map[string]any{"name": "   "}), 400, "标签名称不能为空")
	expectError(t, f.createTag(t, map[string]any{"name": strings.Repeat("标", 41)}), 400, "标签名称不能超过40个字符")
	// 40 字符边界允许。
	boundaryID := f.mustCreateTag(t, map[string]any{"name": strings.Repeat("标", 40)})
	// 不存在的父标签。
	expectError(t, f.createTag(t, map[string]any{"name": "子方向", "parent_id": newID()}), 400, "所属领域不存在")
	// 非法 UUID 父标签。
	expectError(t, f.createTag(t, map[string]any{"name": "子方向", "parent_id": "not-a-uuid"}), 400, "所属领域不存在")
	// 父标签本身是子方向 -> 最多两级。
	expectError(t, f.createTag(t, map[string]any{"name": "三级标签", "parent_id": vulnerabilityID}), 400, "标签树最多两级")
	// 根级重名（trim + 大小写不敏感）。
	expectError(t, f.createTag(t, map[string]any{"name": "  安全 "}), 409, "已存在同名所属领域")
	// 同一领域下重名。
	expectError(t, f.createTag(t, map[string]any{"name": "其他", "parent_id": securityID}), 409, "同一所属领域下已存在同名子方向")
	expectError(t, f.createTag(t, map[string]any{"name": " 其他 ", "parent_id": securityID}), 409, "同一所属领域下已存在同名子方向")
	// 不同领域下同名子方向允许。
	f.mustCreateTag(t, map[string]any{"name": "漏洞检测与攻防", "parent_id": gameID})

	// PATCH 重名冲突与不存在。
	expectError(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/tags/"+vulnerabilityID, map[string]any{"name": "其他"}), 409, "同一所属领域下已存在同名子方向")
	expectError(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/tags/"+newID(), map[string]any{"name": "任意"}), 404, "标签不存在")
	expectError(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/tags/not-a-uuid", map[string]any{"name": "任意"}), 404, "标签不存在")
	expectError(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/tags/"+boundaryID, map[string]any{"name": ""}), 400, "标签名称不能为空")
	// DELETE 不存在。
	expectError(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+newID(), nil), 404, "标签不存在")
	expectError(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/not-a-uuid", nil), 404, "标签不存在")
	expectStatus(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+boundaryID, nil), 204)
}

// ---------- 工程打标签 ----------

func TestPostgresEngineeringTagAssignment(t *testing.T) {
	f := integrationFixture(t)
	tree := f.tagTree(t, f.admin)
	securityID := tagID(t, tree, "", "安全")
	vulnerabilityID := tagID(t, tree, "安全", "漏洞检测与攻防")
	gameID := tagID(t, tree, "", "游戏、图形与多媒体")
	audioID := tagID(t, tree, "游戏、图形与多媒体", "音视频处理")

	expectStatus(t, f.patchEngineering(t, f.engineering, map[string]any{"tag_ids": []string{vulnerabilityID, audioID}}), 200)
	tags := f.detailTags(t, f.engineering)
	if len(tags) != 2 {
		t.Fatalf("工程标签数=%d 期望 2: %v", len(tags), tags)
	}
	first, second := tags[0].(map[string]any), tags[1].(map[string]any)
	if first["id"] != vulnerabilityID || first["name"] != "漏洞检测与攻防" || first["parent_id"] != securityID || first["parent_name"] != "安全" {
		t.Fatalf("第一个工程标签错误: %v", first)
	}
	if second["id"] != audioID || second["name"] != "音视频处理" || second["parent_id"] != gameID || second["parent_name"] != "游戏、图形与多媒体" {
		t.Fatalf("第二个工程标签错误: %v", second)
	}
	// 详情中的 versions 行同样带 tags。
	detail := f.request(t, f.admin, "GET", "/api/v1/admin/engineering/"+f.engineering, nil)
	expectStatus(t, detail, 200)
	versions := detail.body["versions"].([]any)
	if len(versions) != 1 || len(engineeringTags(t, versions[0].(map[string]any))) != 2 {
		t.Fatalf("工程版本缺少标签: %v", detail.body)
	}
	// 工程池列表带 tags。
	item := listEngineeringItem(t, f.request(t, f.admin, "GET", "/api/v1/admin/engineering", nil), f.engineering)
	if len(engineeringTags(t, item)) != 2 {
		t.Fatalf("工程池列表缺少标签: %v", item)
	}
	// PATCH 不传 tag_ids 保持原值。
	expectStatus(t, f.patchEngineering(t, f.engineering, map[string]any{"description": "工程标签保持测试"}), 200)
	if tags = f.detailTags(t, f.engineering); len(tags) != 2 {
		t.Fatalf("省略 tag_ids 时标签未保持: %v", tags)
	}
	// 重复 ID 去重。
	expectStatus(t, f.patchEngineering(t, f.engineering, map[string]any{"tag_ids": []string{vulnerabilityID, vulnerabilityID}}), 200)
	if tags = f.detailTags(t, f.engineering); len(tags) != 1 || tags[0].(map[string]any)["id"] != vulnerabilityID {
		t.Fatalf("重复标签未去重: %v", tags)
	}
	// 审计 engineering.update details 含 tag_ids。
	events := f.request(t, f.admin, "GET", "/api/v1/admin/audit-events?action=engineering.update&page_size=100", nil)
	expectStatus(t, events, 200)
	foundTagIDs := false
	for _, raw := range events.body["items"].([]any) {
		details := raw.(map[string]any)["details"].(map[string]any)
		if values, ok := details["tag_ids"].([]any); ok && len(values) > 0 && values[0] == vulnerabilityID {
			foundTagIDs = true
		}
	}
	if !foundTagIDs {
		t.Fatalf("工程更新审计缺少 tag_ids: %v", events.body)
	}

	// 领域标签、非法 UUID、不存在的标签都不可用。
	expectError(t, f.patchEngineering(t, f.engineering, map[string]any{"tag_ids": []string{securityID}}), 400, "只能为工程选择子方向标签")
	expectError(t, f.patchEngineering(t, f.engineering, map[string]any{"tag_ids": []string{"not-a-uuid"}}), 400, "工程标签无效")
	expectError(t, f.patchEngineering(t, f.engineering, map[string]any{"tag_ids": []string{newID()}}), 400, "工程标签无效")

	// 上限 32：创建 33 个叶子标签后打标签被拒绝。
	limitDomain := f.mustCreateTag(t, map[string]any{"name": "上限领域"})
	many := make([]string, 0, 33)
	for i := 0; i < 33; i++ {
		many = append(many, f.mustCreateTag(t, map[string]any{"name": fmt.Sprintf("上限子方向%02d", i), "parent_id": limitDomain}))
	}
	expectError(t, f.patchEngineering(t, f.engineering, map[string]any{"tag_ids": many}), 400, "工程标签最多 32 个")

	// tag_ids:[] 清空。
	expectStatus(t, f.patchEngineering(t, f.engineering, map[string]any{"tag_ids": []string{}}), 200)
	if tags = f.detailTags(t, f.engineering); len(tags) != 0 {
		t.Fatalf("清空标签失败: %v", tags)
	}
}

// ---------- 工程池筛选 ----------

func TestPostgresEngineeringTagFilter(t *testing.T) {
	f := integrationFixture(t)
	tree := f.tagTree(t, f.admin)
	securityID := tagID(t, tree, "", "安全")
	gameID := tagID(t, tree, "", "游戏、图形与多媒体")
	vulnerabilityID := tagID(t, tree, "安全", "漏洞检测与攻防")
	audioID := tagID(t, tree, "游戏、图形与多媒体", "音视频处理")

	// fixture 工程打「安全/漏洞检测与攻防」，另建一个工程打「游戏/音视频处理」。
	expectStatus(t, f.patchEngineering(t, f.engineering, map[string]any{"tag_ids": []string{vulnerabilityID}}), 200)
	secondEngineering := f.insertEngineering(t, "筛选工程二")
	expectStatus(t, f.patchEngineering(t, secondEngineering, map[string]any{"tag_ids": []string{audioID}}), 200)

	list := func(t *testing.T, query url.Values) testResult {
		t.Helper()
		target := "/api/v1/admin/engineering"
		if encoded := queryEscape(query); encoded != "" {
			target += "?" + encoded
		}
		result := f.request(t, f.admin, "GET", target, nil)
		expectStatus(t, result, 200)
		return result
	}
	total := func(t *testing.T, query url.Values) int {
		t.Helper()
		return int(list(t, query).body["total"].(float64))
	}

	// 无筛选：fixture 工程 + 新工程。
	if got := total(t, url.Values{}); got != 2 {
		t.Fatalf("无筛选工程数=%d 期望 2", got)
	}
	// 领域含全部子方向。
	for _, test := range []struct {
		domain string
		want   string
	}{{securityID, f.engineering}, {gameID, secondEngineering}} {
		result := list(t, url.Values{"tag_ids": {test.domain}})
		if result.body["total"] != float64(1) || result.body["items"].([]any)[0].(map[string]any)["engineering_id"] != test.want {
			t.Fatalf("按领域 %s 筛选错误: %v", test.domain, result.body)
		}
	}
	// 子方向精确命中。
	if item := listEngineeringItem(t, list(t, url.Values{"tag_ids": {vulnerabilityID}}), f.engineering); item["engineering_id"] != f.engineering {
		t.Fatalf("按子方向筛选错误: %v", item)
	}
	// 逗号分隔 OR。
	if got := total(t, url.Values{"tag_ids": {vulnerabilityID + "," + audioID}}); got != 2 {
		t.Fatalf("逗号分隔多标签工程数=%d 期望 2", got)
	}
	// 重复参数 OR。
	if got := total(t, url.Values{"tag_ids": {vulnerabilityID, audioID}}); got != 2 {
		t.Fatalf("重复参数多标签工程数=%d 期望 2", got)
	}
	// 重复同一标签去重后仍为 1。
	if got := total(t, url.Values{"tag_ids": {vulnerabilityID + "," + vulnerabilityID}}); got != 1 {
		t.Fatalf("重复标签去重失败: %d", got)
	}
	// 不匹配的领域 -> 0。
	if got := total(t, url.Values{"tag_ids": {tagID(t, tree, "", "数据工程")}}); got != 0 {
		t.Fatalf("不匹配领域工程数=%d 期望 0", got)
	}
	// 与 q 组合。
	if got := total(t, url.Values{"tag_ids": {vulnerabilityID}, "q": {"测试工程"}}); got != 1 {
		t.Fatalf("标签+q 组合工程数=%d 期望 1", got)
	}
	if got := total(t, url.Values{"tag_ids": {vulnerabilityID}, "q": {"筛选工程二"}}); got != 0 {
		t.Fatalf("标签+q 不匹配却返回 %d", got)
	}
	// 与 status 组合。
	if got := total(t, url.Values{"tag_ids": {vulnerabilityID}, "status": {"ready"}}); got != 1 {
		t.Fatalf("标签+ready 状态工程数=%d 期望 1", got)
	}
	if got := total(t, url.Values{"tag_ids": {vulnerabilityID}, "status": {"failed"}}); got != 0 {
		t.Fatalf("标签+failed 状态工程数=%d 期望 0", got)
	}
	// 与 archived 组合：归档的工程默认不出现。
	expectStatus(t, f.patchEngineering(t, secondEngineering, map[string]any{"archived": true}), 200)
	if got := total(t, url.Values{"tag_ids": {gameID}}); got != 0 {
		t.Fatalf("归档工程默认仍出现: %d", got)
	}
	if got := total(t, url.Values{"tag_ids": {gameID}, "archived": {"true"}}); got != 1 {
		t.Fatalf("archived=true 标签筛选工程数=%d 期望 1", got)
	}
	if got := total(t, url.Values{"tag_ids": {gameID}, "archived": {"all"}}); got != 1 {
		t.Fatalf("archived=all 标签筛选工程数=%d 期望 1", got)
	}
	// 与 exclude_project 组合：fixture 工程在排除项目的范围中。
	if got := total(t, url.Values{"tag_ids": {vulnerabilityID}, "exclude_project": {f.project}}); got != 0 {
		t.Fatalf("标签+exclude_project 工程数=%d 期望 0", got)
	}
	// 非法 / 不存在的标签 ID。
	expectError(t, f.request(t, f.admin, "GET", "/api/v1/admin/engineering?tag_ids=not-a-uuid", nil), 400, "标签筛选无效")
	expectError(t, f.request(t, f.admin, "GET", "/api/v1/admin/engineering?tag_ids="+newID(), nil), 400, "标签筛选无效")
	// 空值视为不过滤。
	if got := total(t, url.Values{"tag_ids": {""}}); got != 1 { // 第二个工程已归档
		t.Fatalf("空 tag_ids 应不过滤，实际 %d", got)
	}
	// 最多 50 个：直接造 50 个子方向标签 + 1 个种子标签凑满 51。
	limitDomain := f.insertTag(t, "", "筛选上限领域", 500)
	filterIDs := []string{securityID}
	for i := 0; i < 50; i++ {
		filterIDs = append(filterIDs, f.insertTag(t, limitDomain, fmt.Sprintf("筛选上限子方向%02d", i), 100+i))
	}
	tooMany := url.Values{"tag_ids": {strings.Join(filterIDs, ",")}}
	expectError(t, f.request(t, f.admin, "GET", "/api/v1/admin/engineering?"+tooMany.Encode(), nil), 400, "标签筛选无效")
	fifty := url.Values{"tag_ids": {strings.Join(filterIDs[:50], ",")}}
	expectStatus(t, f.request(t, f.admin, "GET", "/api/v1/admin/engineering?"+fifty.Encode(), nil), 200)
}

// ---------- engineering_count / 删除保护 / 审计 ----------

func TestPostgresEngineeringTagCountsDeletionAndAudit(t *testing.T) {
	f := integrationFixture(t)
	domainID := f.mustCreateTag(t, map[string]any{"name": "计数领域"})
	childA := f.mustCreateTag(t, map[string]any{"name": "计数子方向A", "parent_id": domainID})
	childB := f.mustCreateTag(t, map[string]any{"name": "计数子方向B", "parent_id": domainID})
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/tags/"+childA, map[string]any{"name": "计数子方向A改"}), 200)

	// 同一工程打同域两个子方向：领域计数按工程去重 = 1。
	expectStatus(t, f.patchEngineering(t, f.engineering, map[string]any{"tag_ids": []string{childA, childB}}), 200)
	items := f.tagTree(t, f.memberA)
	if count := engineeringCountOf(t, items, "", "计数领域"); count != 1 {
		t.Fatalf("领域 engineering_count=%v 期望 1", count)
	}
	if count := engineeringCountOf(t, items, "计数领域", "计数子方向A改"); count != 1 {
		t.Fatalf("子方向A engineering_count=%v 期望 1", count)
	}
	if count := engineeringCountOf(t, items, "计数领域", "计数子方向B"); count != 1 {
		t.Fatalf("子方向B engineering_count=%v 期望 1", count)
	}
	// 第二个工程打同一子方向：领域与子方向计数累加。
	secondEngineering := f.insertEngineering(t, "计数工程二")
	expectStatus(t, f.patchEngineering(t, secondEngineering, map[string]any{"tag_ids": []string{childA}}), 200)
	items = f.tagTree(t, f.memberA)
	if count := engineeringCountOf(t, items, "", "计数领域"); count != 2 {
		t.Fatalf("领域 engineering_count=%v 期望 2", count)
	}
	if count := engineeringCountOf(t, items, "计数领域", "计数子方向A改"); count != 2 {
		t.Fatalf("子方向A engineering_count=%v 期望 2", count)
	}
	if count := engineeringCountOf(t, items, "计数领域", "计数子方向B"); count != 1 {
		t.Fatalf("子方向B engineering_count=%v 期望 1", count)
	}
	// 种子领域仍为 0。
	if count := engineeringCountOf(t, items, "", "安全"); count != 0 {
		t.Fatalf("种子领域 engineering_count=%v 期望 0", count)
	}

	// 审计：tag.create / tag.update 事件。
	created := f.request(t, f.admin, "GET", "/api/v1/admin/audit-events?action=tag.create&page_size=100", nil)
	expectStatus(t, created, 200)
	if created.body["total"] != float64(3) {
		t.Fatalf("tag.create 审计事件数=%v 期望 3", created.body["total"])
	}
	foundDomain := false
	for _, raw := range created.body["items"].([]any) {
		event := raw.(map[string]any)
		details := event["details"].(map[string]any)
		if event["target_type"] != "tag" {
			t.Fatalf("tag.create 事件 target_type=%v", event["target_type"])
		}
		if details["name"] == "计数领域" && details["parent_id"] == nil {
			foundDomain = true
		}
	}
	if !foundDomain {
		t.Fatal("缺少创建计数领域的审计事件")
	}
	updated := f.request(t, f.admin, "GET", "/api/v1/admin/audit-events?action=tag.update&page_size=100", nil)
	expectStatus(t, updated, 200)
	if updated.body["total"] != float64(1) {
		t.Fatalf("tag.update 审计事件数=%v 期望 1", updated.body["total"])
	}
	details := updated.body["items"].([]any)[0].(map[string]any)["details"].(map[string]any)
	if details["name"] != "计数子方向A改" {
		t.Fatalf("tag.update 审计 details 错误: %v", details)
	}

	// 删除保护：领域有子方向；子方向被工程使用（新文案）。
	expectError(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+domainID, nil), 409, "请先删除该领域下的子方向")
	expectError(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+childA, nil), 409, "该工程正使用此标签，无法删除")
	expectError(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+childB, nil), 409, "该工程正使用此标签，无法删除")

	// 解绑后子方向与领域可依次删除，计数归零。
	for _, engineeringID := range []string{f.engineering, secondEngineering} {
		expectStatus(t, f.patchEngineering(t, engineeringID, map[string]any{"tag_ids": []string{}}), 200)
	}
	if count := engineeringCountOf(t, f.tagTree(t, f.admin), "计数领域", "计数子方向A改"); count != 0 {
		t.Fatalf("解绑后子方向 engineering_count=%v 期望 0", count)
	}
	expectStatus(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+childA, nil), 204)
	expectStatus(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+childB, nil), 204)
	expectStatus(t, f.request(t, f.admin, "DELETE", "/api/v1/admin/tags/"+domainID, nil), 204)

	// 审计 tag.delete 事件。
	deleted := f.request(t, f.admin, "GET", "/api/v1/admin/audit-events?action=tag.delete&page_size=100", nil)
	expectStatus(t, deleted, 200)
	if deleted.body["total"] != float64(3) {
		t.Fatalf("tag.delete 审计事件数=%v 期望 3", deleted.body["total"])
	}
	if total := f.tagResponse(t, f.admin).body["total"]; total != float64(10) {
		t.Fatalf("删除后标签总数=%v 期望 10", total)
	}
}

// ---------- 008 迁移：旧 project_tags 数据迁移并删除 ----------

func TestPostgresEngineeringTagMigration(t *testing.T) {
	f := integrationFixture(t)
	ctx := context.Background()
	var oldTable, newTable bool
	if err := f.a.DB.QueryRow(ctx, `SELECT to_regclass('project_tags') IS NOT NULL,to_regclass('engineering_tags') IS NOT NULL`).Scan(&oldTable, &newTable); err != nil {
		t.Fatal(err)
	}
	if oldTable {
		t.Fatal("008 迁移后 project_tags 不应存在")
	}
	if !newTable {
		t.Fatal("008 迁移后 engineering_tags 应存在")
	}
	// 手工重建 v1 的 project_tags 并造一行旧数据，再执行 008 迁移，验证数据搬到工程上。
	vulnerabilityID := tagID(t, f.tagTree(t, f.admin), "安全", "漏洞检测与攻防")
	if _, err := f.a.DB.Exec(ctx, `CREATE TABLE project_tags (
 project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 tag_id uuid NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(project_id,tag_id)
)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.DB.Exec(ctx, `INSERT INTO project_tags(project_id,tag_id) VALUES($1,$2)`, f.project, vulnerabilityID); err != nil {
		t.Fatal(err)
	}
	sql, err := migrations.ReadFile("migrations/008_engineering_tags.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.a.DB.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("重新执行 008 迁移失败: %v", err)
	}
	if err = f.a.DB.QueryRow(ctx, `SELECT to_regclass('project_tags') IS NULL`).Scan(&oldTable); err != nil {
		t.Fatal(err)
	}
	if !oldTable {
		t.Fatal("迁移后 project_tags 仍存在")
	}
	// project_tags 中的标签经 project_engineerings 迁移到引用该项目的工程上。
	var migrated int
	if err = f.a.DB.QueryRow(ctx, `SELECT count(*) FROM engineering_tags WHERE engineering_id=$1 AND tag_id=$2`, f.engineering, vulnerabilityID).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if migrated != 1 {
		t.Fatalf("engineering_tags 迁移行数=%d 期望 1", migrated)
	}
	if tags := f.detailTags(t, f.engineering); len(tags) != 1 || tags[0].(map[string]any)["id"] != vulnerabilityID {
		t.Fatalf("迁移后工程详情标签错误: %v", tags)
	}
}

// ---------- 项目回退：不再有标签 ----------

func TestPostgresProjectTagsRemoved(t *testing.T) {
	f := integrationFixture(t)
	vulnerabilityID := tagID(t, f.tagTree(t, f.admin), "安全", "漏洞检测与攻防")
	start := time.Now().Add(-time.Hour).Format(time.RFC3339)
	end := time.Now().Add(time.Hour).Format(time.RFC3339)
	body := map[string]any{
		"name": "无标签项目", "description": "", "start_at": start, "end_at": end,
		"tech_stack": []string{}, "min_files": 0, "max_files": 0, "status": "active",
		"engineering_ids": []string{}, "member_ids": []string{f.memberA.user.ID},
		"tag_ids": []string{vulnerabilityID}, // 回退后应被忽略
	}
	created := f.request(t, f.admin, "POST", "/api/v1/admin/projects", body)
	expectStatus(t, created, 201)
	if _, exists := created.body["tags"]; exists {
		t.Fatalf("项目创建响应不应再含 tags: %v", created.body)
	}
	projectID := created.body["id"].(string)
	detail := f.request(t, f.admin, "GET", "/api/v1/projects/"+projectID, nil)
	expectStatus(t, detail, 200)
	if _, exists := detail.body["tags"]; exists {
		t.Fatalf("项目详情不应含 tags: %v", detail.body)
	}
	// PATCH 传 tag_ids 被忽略，不报错也不落地。
	patched := f.request(t, f.admin, "PATCH", "/api/v1/admin/projects/"+projectID, map[string]any{"tag_ids": []string{vulnerabilityID}})
	expectStatus(t, patched, 200)
	if _, exists := patched.body["tags"]; exists {
		t.Fatalf("项目更新响应不应再含 tags: %v", patched.body)
	}
	// 项目列表不再支持 tag_ids 筛选（未知参数被忽略），且响应不含 tags。
	listed := f.request(t, f.admin, "GET", "/api/v1/admin/projects?tag_ids="+vulnerabilityID, nil)
	expectStatus(t, listed, 200)
	if got := listed.body["total"]; got != float64(2) { // fixture 项目 + 新建项目
		t.Fatalf("项目数=%v 期望 2", got)
	}
	for _, raw := range listed.body["items"].([]any) {
		if _, exists := raw.(map[string]any)["tags"]; exists {
			t.Fatalf("项目列表不应含 tags: %v", raw)
		}
	}
	// 项目内工程列表仍可带工程 tags 字段（可接受），但项目自身没有标签。
	var tagRows int
	if err := f.a.DB.QueryRow(context.Background(), `SELECT count(*) FROM project_tags`).Scan(&tagRows); err == nil {
		t.Fatal("project_tags 表不应存在")
	}
}

// ---------- 上传新工程时可带 multipart tag_ids ----------

func uploadEngineeringMultipart(t *testing.T, f *testFixture, identity testIdentity, fields map[string]string) testResult {
	t.Helper()
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	file, err := zw.Create("main.go")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("package main\nfunc main() {}\n"))
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	var screenshot bytes.Buffer
	canvas := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			canvas.Set(x, y, color.RGBA{R: 95, G: 120, B: 92, A: 255})
		}
	}
	if err = png.Encode(&screenshot, canvas); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	values := map[string]string{
		"name":        "标签上传工程",
		"description": "带标签上传",
		"github_url":  "https://github.com/example/tagged",
		"github_ref":  "v1.0.0",
	}
	for k, v := range fields {
		values[k] = v
	}
	for k, v := range values {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, part := range []struct {
		name, filename string
		data           []byte
	}{{"zip", "source.zip", archive.Bytes()}, {"startup_screenshot", "startup.png", screenshot.Bytes()}, {"feature_screenshots", "feature.png", screenshot.Bytes()}} {
		w, err := writer.CreateFormFile(part.name, part.filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(part.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest("POST", f.server.URL+"/api/v1/admin/engineering-versions", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-CSRF-Token", identity.csrf)
	request.AddCookie(&http.Cookie{Name: "projecthub_session", Value: identity.cookie})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(response.Body).Decode(&out)
	return testResult{status: response.StatusCode, body: out}
}

func TestPostgresEngineeringTagUpload(t *testing.T) {
	f := integrationFixture(t)
	tree := f.tagTree(t, f.admin)
	securityID := tagID(t, tree, "", "安全")
	vulnerabilityID := tagID(t, tree, "安全", "漏洞检测与攻防")
	audioID := tagID(t, tree, "游戏、图形与多媒体", "音视频处理")

	// 新建工程时 multipart tag_ids 生效。
	created := uploadEngineeringMultipart(t, f, f.admin, map[string]string{"tag_ids": fmt.Sprintf(`[%q,%q]`, vulnerabilityID, audioID)})
	expectStatus(t, created, 201)
	engineeringID := created.body["engineering_id"].(string)
	if tags := f.detailTags(t, engineeringID); len(tags) != 2 {
		t.Fatalf("上传时标签未写入: %v", tags)
	}
	// 领域标签被拒绝。
	expectError(t, uploadEngineeringMultipart(t, f, f.admin, map[string]string{"tag_ids": fmt.Sprintf(`[%q]`, securityID)}), 400, "只能为工程选择子方向标签")
	// 非法 JSON 被拒绝。
	expectError(t, uploadEngineeringMultipart(t, f, f.admin, map[string]string{"tag_ids": "not-json"}), 400, "工程标签无效")
	// 不存在的标签被拒绝。
	expectError(t, uploadEngineeringMultipart(t, f, f.admin, map[string]string{"tag_ids": fmt.Sprintf(`[%q]`, newID())}), 400, "工程标签无效")

	// 追加版本时 tag_ids 被忽略，不修改已有工程标签。
	appended := uploadEngineeringMultipart(t, f, f.admin, map[string]string{"engineering_id": engineeringID, "tag_ids": fmt.Sprintf(`[%q]`, tagID(t, tree, "游戏、图形与多媒体", "图形渲染"))})
	expectStatus(t, appended, 201)
	if tags := f.detailTags(t, engineeringID); len(tags) != 2 {
		t.Fatalf("追加版本不应修改工程标签: %v", tags)
	}
}
