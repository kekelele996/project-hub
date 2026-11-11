package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	_ "golang.org/x/image/webp"
)

type EngineeringAsset struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

type EngineeringVersion struct {
	ID              string             `json:"id"`
	EngineeringID   string             `json:"engineering_id"`
	VersionNumber   int                `json:"version_number"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	TechStack       []string           `json:"tech_stack"`
	Status          string             `json:"status"`
	SourceFileCount int                `json:"source_file_count"`
	TotalFileCount  int                `json:"total_file_count"`
	Languages       []LanguageStat     `json:"languages"`
	ZipSHA256       string             `json:"zip_sha256,omitempty"`
	ZipSize         int64              `json:"zip_size"`
	ZipPath         string             `json:"-"`
	GithubURL       string             `json:"github_url,omitempty"`
	GithubRef       string             `json:"github_ref,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	Assets          []EngineeringAsset `json:"assets"`
	Archived        bool               `json:"archived"`
	Tags            []EngineeringTag   `json:"tags"`
}

const versionColumns = `v.id::text,v.engineering_id::text,v.version_number,e.name,e.description,v.tech_stack,v.status,v.source_file_count,v.total_file_count,v.languages,v.zip_sha256,v.zip_size,v.zip_path,v.github_url,v.github_ref,v.created_at,e.archived,(SELECT coalesce(jsonb_agg(jsonb_build_object('id',t.id::text,'name',t.name,'parent_id',t.parent_id::text,'parent_name',coalesce(p.name,'')) ORDER BY coalesce(p.sort_order,t.sort_order),CASE WHEN t.name='其他' THEN 1 ELSE 0 END,t.sort_order,t.name),'[]'::jsonb) FROM engineering_tags et JOIN tags t ON t.id=et.tag_id LEFT JOIN tags p ON p.id=t.parent_id WHERE et.engineering_id=e.id)`

func scanVersion(row pgx.Row) (EngineeringVersion, error) {
	v := EngineeringVersion{TechStack: []string{}, Languages: []LanguageStat{}, Assets: []EngineeringAsset{}, Tags: []EngineeringTag{}}
	var tech, languages, tags []byte
	err := row.Scan(&v.ID, &v.EngineeringID, &v.VersionNumber, &v.Name, &v.Description, &tech, &v.Status, &v.SourceFileCount, &v.TotalFileCount, &languages, &v.ZipSHA256, &v.ZipSize, &v.ZipPath, &v.GithubURL, &v.GithubRef, &v.CreatedAt, &v.Archived, &tags)
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(tech, &v.TechStack); err != nil {
		return v, err
	}
	if err = json.Unmarshal(languages, &v.Languages); err != nil {
		return v, err
	}
	_ = json.Unmarshal(tags, &v.Tags)
	if v.Tags == nil {
		v.Tags = []EngineeringTag{}
	}
	return v, nil
}

func (a *App) loadVersion(ctx context.Context, id string) (EngineeringVersion, error) {
	v, err := scanVersion(a.DB.QueryRow(ctx, `SELECT `+versionColumns+` FROM engineering_versions v JOIN engineering e ON e.id=v.engineering_id WHERE v.id=$1`, id))
	if err != nil {
		return v, err
	}
	v.Assets, err = a.loadAssets(ctx, id)
	return v, err
}

func (a *App) loadAssets(ctx context.Context, id string) ([]EngineeringAsset, error) {
	assets := []EngineeringAsset{}
	rows, err := a.DB.Query(ctx, `SELECT id::text,kind FROM engineering_assets WHERE version_id=$1 ORDER BY kind DESC,created_at,id`, id)
	if err != nil {
		return assets, err
	}
	defer rows.Close()
	for rows.Next() {
		var item EngineeringAsset
		if err := rows.Scan(&item.ID, &item.Kind); err != nil {
			return assets, err
		}
		item.URL = "/api/v1/assets/" + item.ID
		assets = append(assets, item)
	}
	return assets, rows.Err()
}

func (a *App) latestReadyVersion(ctx context.Context, engineeringID string) (string, error) {
	var id string
	err := a.DB.QueryRow(ctx, `SELECT v.id::text FROM engineering_versions v JOIN engineering e ON e.id=v.engineering_id WHERE v.engineering_id=$1 AND v.status='ready' AND NOT e.archived ORDER BY v.version_number DESC LIMIT 1`, engineeringID).Scan(&id)
	return id, err
}

func engineeringAdmin(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r).Role != "admin" {
			fail(w, http.StatusForbidden, "此操作需要管理员权限")
			return
		}
		handler(w, r)
	}
}

func (a *App) registerEngineering(r chi.Router) {
	r.Get("/api/v1/admin/engineering", engineeringAdmin(a.listEngineering))
	r.Get("/api/v1/admin/engineering/{id}", engineeringAdmin(a.engineeringDetail))
	r.Patch("/api/v1/admin/engineering/{id}", engineeringAdmin(a.updateEngineering))
	r.Post("/api/v1/admin/engineering-versions", engineeringAdmin(a.uploadEngineering))
	r.Get("/api/v1/admin/engineering-versions/{id}/zip", engineeringAdmin(a.adminVersionZIP))
	r.Get("/api/v1/admin/analysis-jobs/{id}", engineeringAdmin(a.analysisJob))
	r.Post("/api/v1/admin/analysis-jobs/{id}/retry", engineeringAdmin(a.retryAnalysis))
	r.Get("/api/v1/assets/{id}", a.serveAsset)
}

func (a *App) listEngineering(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	where := ` WHERE NOT e.archived`
	switch query.Get("archived") {
	case "", "false":
	case "true":
		where = ` WHERE e.archived`
	case "all":
		where = ` WHERE true`
	default:
		fail(w, 400, "归档状态无效")
		return
	}
	tagCSV, filterOK, filterErr := a.tagFilterCSV(r.Context(), r)
	if filterErr != nil {
		dbError(w, filterErr)
		return
	}
	if !filterOK {
		fail(w, 400, "标签筛选无效")
		return
	}
	args := []any{}
	add := func(condition string, value any) {
		args = append(args, value)
		where += " AND " + fmt.Sprintf(condition, len(args))
	}
	if tagCSV != "" {
		add(`EXISTS(WITH RECURSIVE selected AS (SELECT id FROM tags WHERE id=ANY(string_to_array($%d,',')::uuid[]) UNION ALL SELECT c.id FROM tags c JOIN selected s ON c.parent_id=s.id) SELECT 1 FROM engineering_tags et JOIN selected s ON s.id=et.tag_id WHERE et.engineering_id=e.id)`, tagCSV)
	}
	if excludedProject := query.Get("exclude_project"); excludedProject != "" {
		if !validID(excludedProject) {
			fail(w, 400, "排除项目标识无效")
			return
		}
		add(`NOT EXISTS(SELECT 1 FROM project_engineerings pe WHERE pe.project_id=$%d AND pe.engineering_id=e.id AND pe.active)`, excludedProject)
	}
	if q := strings.TrimSpace(query.Get("q")); q != "" {
		add(`(e.name ILIKE '%%'||$%[1]d||'%%' OR e.description ILIKE '%%'||$%[1]d||'%%')`, q)
	}
	if language := query.Get("language"); language != "" && !strings.EqualFold(language, "all") && language != "全部" {
		add(`EXISTS(SELECT 1 FROM jsonb_array_elements(v.languages) lang WHERE lower(lang->>'name')=lower($%d))`, language)
	}
	if stack := techStackQuery(query["tech_stack"]); len(stack) > 0 {
		add(`EXISTS(SELECT 1 FROM jsonb_array_elements_text(v.tech_stack) AS stack WHERE lower(stack)=ANY($%d::text[]))`, stack)
	}
	if status := query.Get("status"); status != "" && status != "all" {
		if status != "ready" && status != "pending" && status != "failed" {
			fail(w, 400, "分析状态无效")
			return
		}
		add(`v.status=$%d`, status)
	}
	if _, ok := query["min_files"]; ok {
		add(`v.source_file_count >= $%d`, intQuery(r, "min_files", 0))
	}
	if maximum := intQuery(r, "max_files", 0); maximum > 0 {
		add(`v.source_file_count <= $%d`, maximum)
	}
	base := ` FROM engineering e JOIN LATERAL (SELECT * FROM engineering_versions WHERE engineering_id=e.id ORDER BY version_number DESC LIMIT 1) v ON true` + where
	var total int
	if err := a.DB.QueryRow(r.Context(), `SELECT count(*)`+base, args...).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	limit, offset := pagination(r)
	args = append(args, limit, offset)
	rows, err := a.DB.Query(r.Context(), `SELECT `+versionColumns+base+fmt.Sprintf(` ORDER BY e.created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		dbError(w, err)
		return
	}
	items := []EngineeringVersion{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			rows.Close()
			dbError(w, err)
			return
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		dbError(w, err)
		return
	}
	for i := range items {
		items[i].Assets, err = a.loadAssets(r.Context(), items[i].ID)
		if err != nil {
			dbError(w, err)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}

func (a *App) engineeringDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "工程不存在")
		return
	}
	var name, description string
	var archived bool
	var created time.Time
	err := a.DB.QueryRow(r.Context(), `SELECT name,description,archived,created_at FROM engineering WHERE id=$1`, id).Scan(&name, &description, &archived, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "工程不存在")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	rows, err := a.DB.Query(r.Context(), `SELECT `+versionColumns+` FROM engineering_versions v JOIN engineering e ON e.id=v.engineering_id WHERE e.id=$1 ORDER BY version_number DESC`, id)
	if err != nil {
		dbError(w, err)
		return
	}
	versions := []EngineeringVersion{}
	for rows.Next() {
		v, scanErr := scanVersion(rows)
		if scanErr != nil {
			rows.Close()
			dbError(w, scanErr)
			return
		}
		versions = append(versions, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		dbError(w, err)
		return
	}
	for i := range versions {
		versions[i].Assets, err = a.loadAssets(r.Context(), versions[i].ID)
		if err != nil {
			dbError(w, err)
			return
		}
	}
	tags, err := loadEngineeringTags(r.Context(), a.DB, id)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"engineering": map[string]any{"id": id, "name": name, "description": description, "archived": archived, "created_at": created, "tags": tags}, "versions": versions})
}

func (a *App) updateEngineering(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "工程不存在")
		return
	}
	var input struct {
		Name        *string   `json:"name"`
		Description *string   `json:"description"`
		Archived    *bool     `json:"archived"`
		TagIDs      *[]string `json:"tag_ids"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Name != nil {
		*input.Name = strings.TrimSpace(*input.Name)
		if *input.Name == "" || len(*input.Name) > 240 {
			fail(w, 400, "工程名称不能为空且不能超过 240 字节")
			return
		}
	}
	if input.Description != nil && len(*input.Description) > 10000 {
		fail(w, 400, "工程说明过长")
		return
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	tagIDs := []string{}
	if input.TagIDs != nil {
		var tagMessage string
		tagIDs, tagMessage, err = normalizeEngineeringTagIDs(r.Context(), tx, *input.TagIDs)
		if err != nil {
			dbError(w, err)
			return
		}
		if tagMessage != "" {
			fail(w, 400, tagMessage)
			return
		}
	}
	result, err := tx.Exec(r.Context(), `UPDATE engineering SET name=coalesce($2,name),description=coalesce($3,description),archived=coalesce($4,archived) WHERE id=$1`, id, input.Name, input.Description, input.Archived)
	if err != nil {
		dbError(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		fail(w, 404, "工程不存在")
		return
	}
	if input.TagIDs != nil {
		if err = setEngineeringTags(r.Context(), tx, id, tagIDs); err != nil {
			dbError(w, err)
			return
		}
	}
	if err = a.audit(r.Context(), tx, userFrom(r).ID, "engineering.update", "engineering", id, "", id, input); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type savedAsset struct{ id, kind, path, mime string }

func techStackQuery(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.ToLower(strings.TrimSpace(item))
			if item != "" && item != "all" && item != "全部" && !seen[item] {
				result = append(result, item)
				seen[item] = true
			}
		}
	}
	return result
}

func (a *App) uploadEngineering(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxZIPBytes+8*maxScreenshotBytes+(1<<20))
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		fail(w, 400, "上传内容过大或表单格式无效")
		return
	}
	defer r.MultipartForm.RemoveAll()
	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))
	engineeringID := r.FormValue("engineering_id")
	backupAccountID := strings.TrimSpace(r.FormValue("backup_account_id"))
	backupRepoName := strings.TrimSpace(r.FormValue("backup_repo_name"))
	if backupAccountID != "" || backupRepoName != "" {
		if !a.requireBackupConfiguration(w) {
			return
		}
		if (backupAccountID != "" && !validID(backupAccountID)) || (backupRepoName != "" && (!backupSlugPattern.MatchString(backupRepoName) || strings.Contains(backupRepoName, "--"))) {
			fail(w, 400, "灾备账号标识无效，或仓库名不符合 3 至 48 位小写字母、数字、单个连字符规则")
			return
		}
	}
	if (engineeringID == "" && name == "") || len(name) > 240 || len(description) > 10000 || (engineeringID != "" && !validID(engineeringID)) {
		fail(w, 400, "请填写有效的工程名称与工程信息")
		return
	}
	var tech []string
	if raw := r.FormValue("tech_stack"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &tech); err != nil || len(tech) > 30 {
			fail(w, 400, "技术栈应为 JSON 字符串数组，最多 30 项")
			return
		}
	}
	if tech == nil {
		tech = []string{}
	}
	for _, item := range tech {
		if strings.TrimSpace(item) == "" || len(item) > 80 {
			fail(w, 400, "技术栈名称无效")
			return
		}
	}
	githubURL := strings.TrimSpace(r.FormValue("github_url"))
	githubRef := strings.TrimSpace(r.FormValue("github_ref"))
	if githubURL == "" || githubRef == "" {
		fail(w, 400, "请填写 GitHub 仓库地址及固定 commit 或 tag")
		return
	}
	if githubURL != "" {
		parsed, err := url.Parse(githubURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || len(strings.Split(strings.Trim(parsed.Path, "/"), "/")) != 2 || parsed.RawQuery != "" || parsed.Fragment != "" || githubRef == "" {
			fail(w, 400, "请填写 GitHub HTTPS 仓库地址及固定 commit 或 tag")
			return
		}
	}
	if len(githubRef) > 200 || strings.ContainsAny(githubRef, "\r\n") {
		fail(w, 400, "GitHub commit 或 tag 无效")
		return
	}
	zipHeaders := r.MultipartForm.File["zip"]
	if len(zipHeaders) != 1 || zipHeaders[0].Size > maxZIPBytes || zipHeaders[0].Size == 0 {
		fail(w, 400, "请上传一个不超过 256 MB 的 ZIP 文件")
		return
	}
	if len(r.MultipartForm.File["startup_screenshot"]) != 1 || len(r.MultipartForm.File["feature_screenshots"]) == 0 || len(r.MultipartForm.File["feature_screenshots"]) > 6 {
		fail(w, 400, "请上传 1 张启动截图和 1 至 6 张功能截图")
		return
	}
	// 只有新建工程接受标签；追加版本忽略 tag_ids。
	creating := engineeringID == ""
	newTagIDs := []string{}
	if creating {
		if raw := strings.TrimSpace(r.FormValue("tag_ids")); raw != "" {
			if err := json.Unmarshal([]byte(raw), &newTagIDs); err != nil {
				fail(w, 400, "工程标签无效")
				return
			}
		}
	}
	versionID, jobID := newID(), newID()
	relDir := filepath.Join("versions", versionID)
	directory := filepath.Join(a.UploadDir, relDir)
	if err := os.MkdirAll(directory, 0700); err != nil {
		dbError(w, err)
		return
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(directory)
		}
	}()
	zipPath := filepath.Join(relDir, newID()+".zip")
	size, err := saveMultipartFile(zipHeaders[0], filepath.Join(a.UploadDir, zipPath), maxZIPBytes)
	if err != nil {
		fail(w, 400, "ZIP 保存失败或超过大小限制")
		return
	}
	assets := []savedAsset{}
	for _, kind := range []string{"startup", "feature"} {
		field := "feature_screenshots"
		if kind == "startup" {
			field = "startup_screenshot"
		}
		for _, header := range r.MultipartForm.File[field] {
			id := newID()
			assetPath := filepath.Join(relDir, id+".image")
			fullPath := filepath.Join(a.UploadDir, assetPath)
			if _, err := saveMultipartFile(header, fullPath, maxScreenshotBytes); err != nil {
				fail(w, 400, "截图保存失败，每张截图不能超过 12 MB")
				return
			}
			mimeType, err := validateScreenshot(fullPath)
			if err != nil {
				fail(w, 400, err.Error())
				return
			}
			assets = append(assets, savedAsset{id: id, kind: kind, path: assetPath, mime: mimeType})
		}
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if backupAccountID != "" {
		var backupAccount GitHubAccount
		backupAccount, err = scanAccount(tx.QueryRow(r.Context(), `SELECT `+accountColumns+` FROM github_accounts WHERE id=$1`, backupAccountID))
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, 404, "灾备账号不存在")
			return
		}
		if err != nil {
			dbError(w, err)
			return
		}
		if err = usableAccount(backupAccount); err != nil {
			fail(w, 409, err.Error())
			return
		}
	}
	if creating {
		var tagMessage string
		newTagIDs, tagMessage, err = normalizeEngineeringTagIDs(r.Context(), tx, newTagIDs)
		if err != nil {
			dbError(w, err)
			return
		}
		if tagMessage != "" {
			fail(w, 400, tagMessage)
			return
		}
	}
	slug := backupRepoName
	if engineeringID == "" {
		if slug == "" {
			slug = "engineering"
		}
		engineeringID = newID()
		_, err = tx.Exec(r.Context(), `INSERT INTO engineering(id,name,description,backup_repo_name) VALUES($1,$2,$3,$4)`, engineeringID, name, description, slug)
	} else {
		var archived bool
		err = tx.QueryRow(r.Context(), `SELECT archived,backup_repo_name FROM engineering WHERE id=$1 FOR UPDATE`, engineeringID).Scan(&archived, &slug)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, 404, "工程不存在")
			return
		}
		if archived {
			fail(w, 409, "归档工程不可上传新版本")
			return
		}
		if err == nil && backupRepoName != "" && slug != backupRepoName {
			var bound bool
			err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM backup_targets WHERE engineering_id=$1 AND managed)`, engineeringID).Scan(&bound)
			if err == nil && bound {
				fail(w, 409, "该工程已自动绑定灾备仓库，后续版本沿用原仓库名")
				return
			}
			if err == nil {
				slug = backupRepoName
				_, err = tx.Exec(r.Context(), `UPDATE engineering SET backup_repo_name=$2 WHERE id=$1`, engineeringID, slug)
			}
		}
	}
	if err == nil && creating {
		err = setEngineeringTags(r.Context(), tx, engineeringID, newTagIDs)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	backupTargets := []map[string]string{}
	if a.backupConfigured() {
		if err = a.ensureGlobalBackupTargets(r.Context(), tx, engineeringID, slug, userFrom(r).ID); err != nil {
			dbError(w, err)
			return
		}
		rows, queryErr := tx.Query(r.Context(), `SELECT t.id,t.owner,t.repo FROM backup_targets t JOIN github_accounts a ON a.id=t.account_id WHERE t.engineering_id=$1 AND t.enabled AND t.auto_sync AND a.active AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) ORDER BY t.created_at,t.id`, engineeringID)
		if queryErr != nil {
			dbError(w, queryErr)
			return
		}
		for rows.Next() {
			var id, owner, repo string
			if err = rows.Scan(&id, &owner, &repo); err != nil {
				break
			}
			backupTargets = append(backupTargets, map[string]string{"id": id, "repository_url": "https://github.com/" + owner + "/" + repo})
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			dbError(w, err)
			return
		}
	}
	var number int
	if err = tx.QueryRow(r.Context(), `SELECT coalesce(max(version_number),0)+1 FROM engineering_versions WHERE engineering_id=$1`, engineeringID).Scan(&number); err != nil {
		dbError(w, err)
		return
	}
	techJSON, _ := json.Marshal(tech)
	_, err = tx.Exec(r.Context(), `INSERT INTO engineering_versions(id,engineering_id,version_number,tech_stack,zip_size,zip_path,github_url,github_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, versionID, engineeringID, number, techJSON, size, zipPath, githubURL, githubRef)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO analysis_jobs(id,version_id) VALUES($1,$2)`, jobID, versionID)
	}
	for _, asset := range assets {
		if err != nil {
			break
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO engineering_assets(id,version_id,kind,path,mime_type) VALUES($1,$2,$3,$4,$5)`, asset.id, versionID, asset.kind, asset.path, asset.mime)
	}
	if err == nil {
		err = a.audit(r.Context(), tx, userFrom(r).ID, "engineering.version.upload", "engineering_version", versionID, "", engineeringID, map[string]any{"version_number": number, "zip_size": size})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	keep = true
	result := map[string]any{"engineering_id": engineeringID, "version_id": versionID, "job_id": jobID, "backup_targets": backupTargets, "backup_status": "unconfigured"}
	if len(backupTargets) > 0 {
		result["backup_status"] = "waiting_analysis"
		result["backup_target_id"] = backupTargets[0]["id"]
		result["backup_repository_url"] = backupTargets[0]["repository_url"]
	}
	writeJSON(w, 201, result)
}

func saveMultipartFile(header *multipart.FileHeader, filename string, limit int64) (int64, error) {
	if header.Size > limit {
		return 0, errors.New("file size limit")
	}
	source, err := header.Open()
	if err != nil {
		return 0, err
	}
	defer source.Close()
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(file, io.LimitReader(source, limit+1))
	closeErr := file.Close()
	if err != nil {
		return n, err
	}
	if closeErr != nil {
		return n, closeErr
	}
	if n > limit {
		return n, errors.New("file size limit")
	}
	return n, nil
}

func validateScreenshot(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	config, format, err := image.DecodeConfig(file)
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 16000 || config.Height > 16000 || int64(config.Width)*int64(config.Height) > 40000000 {
		return "", errors.New("截图必须是有效的 PNG、JPEG、WebP 或 GIF，且不超过 4000 万像素")
	}
	if _, err = file.Seek(0, 0); err != nil {
		return "", err
	}
	if _, _, err = image.Decode(file); err != nil {
		return "", errors.New("截图图片数据损坏")
	}
	switch format {
	case "png":
		return "image/png", nil
	case "jpeg":
		return "image/jpeg", nil
	case "gif":
		return "image/gif", nil
	case "webp":
		return "image/webp", nil
	default:
		return "", errors.New("不支持此截图格式")
	}
}

func (a *App) analysisJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "分析任务不存在")
		return
	}
	var status, jobError, versionID string
	err := a.DB.QueryRow(r.Context(), `SELECT status,error,version_id::text FROM analysis_jobs WHERE id=$1`, id).Scan(&status, &jobError, &versionID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "分析任务不存在")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"id": id, "status": status, "error": jobError, "version_id": versionID})
}

func (a *App) retryAnalysis(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "分析任务不存在")
		return
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var status, versionID, engineeringID string
	err = tx.QueryRow(r.Context(), `SELECT j.status,j.version_id::text,v.engineering_id::text FROM analysis_jobs j JOIN engineering_versions v ON v.id=j.version_id WHERE j.id=$1 FOR UPDATE OF j,v`, id).Scan(&status, &versionID, &engineeringID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "分析任务不存在")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if status != "failed" {
		fail(w, 409, "只能重试失败的分析任务")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE analysis_jobs SET status='pending',error='',updated_at=now() WHERE id=$1`, id)
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE engineering_versions SET status='pending' WHERE id=$1 AND status='failed'`, versionID)
	}
	if err == nil {
		err = a.audit(r.Context(), tx, userFrom(r).ID, "engineering.analysis.retry", "analysis_job", id, "", engineeringID, map[string]string{"version_id": versionID})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"id": id, "status": "pending", "version_id": versionID})
}

func (a *App) StartAnalysisWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			for ctx.Err() == nil {
				found, err := a.processAnalysisJob(ctx)
				if err != nil {
					log.Printf("analysis worker: %v", err)
					break
				}
				if !found {
					break
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (a *App) processAnalysisJob(ctx context.Context) (bool, error) {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var jobID, versionID, relativePath string
	err = tx.QueryRow(ctx, `SELECT j.id::text,j.version_id::text,v.zip_path FROM analysis_jobs j JOIN engineering_versions v ON v.id=j.version_id WHERE j.status='pending' ORDER BY j.created_at LIMIT 1 FOR UPDATE OF j SKIP LOCKED`).Scan(&jobID, &versionID, &relativePath)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	filename, analysisErr := a.storedPath(relativePath)
	var result archiveAnalysis
	if analysisErr == nil {
		result, analysisErr = analyzeArchiveContext(ctx, filename)
	}
	status, message := "ready", ""
	if analysisErr != nil {
		status, message = "failed", analysisErr.Error()
	}
	if analysisErr == nil {
		languages, _ := json.Marshal(result.Languages)
		_, err = tx.Exec(ctx, `UPDATE engineering_versions SET status='ready',source_file_count=$2,total_file_count=$3,languages=$4,zip_sha256=$5,zip_size=$6 WHERE id=$1 AND status='pending'`, versionID, result.SourceFileCount, result.TotalFileCount, languages, result.SHA256, result.Size)
	} else {
		_, err = tx.Exec(ctx, `UPDATE engineering_versions SET status='failed' WHERE id=$1 AND status='pending'`, versionID)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE analysis_jobs SET status=$2,error=$3,updated_at=now() WHERE id=$1`, jobID, status, message)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err == nil && analysisErr == nil {
		err = a.enqueueAutoBackups(ctx, versionID)
	}
	return true, err
}

func (a *App) storedPath(relativePath string) (string, error) {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return "", errors.New("无效的存储路径")
	}
	root, err := filepath.Abs(a.UploadDir)
	if err != nil {
		return "", err
	}
	filename := filepath.Join(root, filepath.Clean(relativePath))
	relative, err := filepath.Rel(root, filename)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("无效的存储路径")
	}
	return filename, nil
}

func (a *App) adminVersionZIP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "工程版本不存在")
		return
	}
	v, err := a.loadVersion(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "工程版本不存在")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if v.Status != "ready" {
		fail(w, 409, "工程版本分析尚未完成")
		return
	}
	a.serveStoredFile(w, r, v.ZipPath, "engineering-"+v.EngineeringID+"-v"+strconv.Itoa(v.VersionNumber)+".zip", "application/zip")
}

func (a *App) serveStoredFile(w http.ResponseWriter, r *http.Request, stored, filename, mimeType string) {
	path, err := a.storedPath(stored)
	if err != nil {
		fail(w, 404, "文件不存在")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		fail(w, 404, "文件不存在")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		fail(w, 404, "文件不存在")
		return
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	if filename != "" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	}
	http.ServeContent(w, r, filename, info.ModTime(), file)
}

func (a *App) serveAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "截图不存在")
		return
	}
	var versionID, path, mimeType string
	err := a.DB.QueryRow(r.Context(), `SELECT version_id::text,path,mime_type FROM engineering_assets WHERE id=$1`, id).Scan(&versionID, &path, &mimeType)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "截图不存在")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	user := userFrom(r)
	if user.Role != "admin" {
		var allowed bool
		err = a.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM project_engineerings pe JOIN project_members pm ON pm.project_id=pe.project_id WHERE pe.version_id=$1 AND pe.active AND pm.user_id=$2)`, versionID, user.ID).Scan(&allowed)
		if err != nil {
			dbError(w, err)
			return
		}
		if !allowed {
			fail(w, 403, "没有访问此截图的权限")
			return
		}
	}
	a.serveStoredFile(w, r, path, "", mimeType)
}
