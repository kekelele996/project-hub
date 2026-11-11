package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (a *App) projectEngineering(w http.ResponseWriter, r *http.Request) {
	pid := chi.URLParam(r, "id")
	u := userFrom(r)
	if !a.canAccessProject(r.Context(), u, pid) {
		fail(w, 403, "项目不存在或无权访问")
		return
	}
	scope := r.URL.Query().Get("scope")
	isOpen := strings.HasPrefix(r.URL.Path, "/open/")
	if scope != "" && scope != "all" && scope != "available" && scope != "mine" {
		fail(w, 400, "工程范围无效")
		return
	}
	n, o := pagination(r)
	q := r.URL.Query()
	where := ` WHERE pe.project_id=$1 AND ($2='' OR e.name ILIKE '%'||$2||'%' OR e.description ILIKE '%'||$2||'%') AND ($3='' OR EXISTS(SELECT 1 FROM jsonb_array_elements(v.languages) l WHERE lower(l->>'name')=lower($3))) AND v.source_file_count >= $4 AND ($5=0 OR v.source_file_count <= $5) AND ($6<>'available' OR c.id IS NULL) AND ($6<>'mine' OR c.user_id=$7) AND (NOT $8 OR c.id IS NULL OR c.user_id=$7)`
	join := ` FROM project_engineerings pe JOIN engineering e ON e.id=pe.engineering_id JOIN engineering_versions v ON v.id=pe.version_id LEFT JOIN claims c ON c.project_engineering_id=pe.id AND c.released_at IS NULL LEFT JOIN users u ON u.id=c.user_id`
	where += ` AND pe.active`
	args := []any{pid, q.Get("q"), q.Get("language"), intQuery(r, "min_files", 0), intQuery(r, "max_files", 0), scope, u.ID, isOpen && u.Role != "admin"}
	var total int
	if err := a.DB.QueryRow(r.Context(), `SELECT count(*)`+join+where, args...).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	args = append(args, n, o)
	rows, err := a.DB.Query(r.Context(), `SELECT pe.engineering_id,pe.version_id,coalesce(c.id::text,''),coalesce(c.user_id::text,''),coalesce(u.name,''),c.claimed_at`+join+where+` ORDER BY e.name LIMIT $9 OFFSET $10`, args...)
	if err != nil {
		dbError(w, err)
		return
	}
	type assoc struct {
		eid, vid, cid, uid, name string
		at                       *time.Time
	}
	records := []assoc{}
	for rows.Next() {
		var x assoc
		if err = rows.Scan(&x.eid, &x.vid, &x.cid, &x.uid, &x.name, &x.at); err != nil {
			rows.Close()
			dbError(w, err)
			return
		}
		records = append(records, x)
	}
	rows.Close()
	items := []any{}
	for _, x := range records {
		v, err := a.loadVersion(r.Context(), x.vid)
		if err != nil {
			dbError(w, err)
			return
		}
		raw, _ := json.Marshal(v)
		var item map[string]any
		_ = json.Unmarshal(raw, &item)
		allowed := u.Role == "admin" || x.uid == u.ID
		if !allowed {
			delete(item, "github_url")
			delete(item, "github_ref")
			delete(item, "zip_sha256")
		}
		item["project_id"] = pid
		item["engineering_id"] = x.eid
		item["version_id"] = x.vid
		item["claim_id"] = x.cid
		item["claimed_by"] = x.uid
		item["claimed_name"] = x.name
		item["claimed_at"] = x.at
		item["can_download"] = allowed
		if allowed {
			item["zip_url"] = "/api/v1/projects/" + pid + "/engineering/" + x.eid + "/zip"
			if isOpen {
				item["zip_url"] = "/open/v1/projects/" + pid + "/engineering/" + x.eid + "/zip"
			}
		}
		items = append(items, item)
	}
	listJSON(w, items, total)
}
func (a *App) downloadProjectZip(w http.ResponseWriter, r *http.Request) {
	pid, eid := chi.URLParam(r, "id"), chi.URLParam(r, "engineeringId")
	u := userFrom(r)
	if !validID(eid) || !a.canAccessProject(r.Context(), u, pid) {
		fail(w, 403, "工程不存在或无权访问")
		return
	}
	var path, name string
	err := a.DB.QueryRow(r.Context(), `SELECT v.zip_path,e.name FROM project_engineerings pe JOIN engineering_versions v ON v.id=pe.version_id JOIN engineering e ON e.id=pe.engineering_id WHERE pe.project_id=$1 AND pe.engineering_id=$2 AND pe.active AND ($3='admin' OR EXISTS(SELECT 1 FROM claims c WHERE c.project_engineering_id=pe.id AND c.user_id=$4 AND c.released_at IS NULL))`, pid, eid, u.Role, u.ID).Scan(&path, &name)
	if err != nil {
		fail(w, 403, "领取工程后才能下载")
		return
	}
	a.serveStoredFile(w, r, path, name+".zip", "application/zip")
}
func (a *App) openEngineering(w http.ResponseWriter, r *http.Request) {
	eid, pid := chi.URLParam(r, "id"), r.URL.Query().Get("project_id")
	u := userFrom(r)
	if !validID(eid) || !a.canAccessProject(r.Context(), u, pid) {
		fail(w, 404, "工程不存在或无权访问")
		return
	}
	var vid, uid string
	err := a.DB.QueryRow(r.Context(), `SELECT pe.version_id,coalesce(c.user_id::text,'') FROM project_engineerings pe LEFT JOIN claims c ON c.project_engineering_id=pe.id AND c.released_at IS NULL WHERE pe.project_id=$1 AND pe.engineering_id=$2 AND pe.active`, pid, eid).Scan(&vid, &uid)
	if err != nil || (u.Role != "admin" && uid != "" && uid != u.ID) {
		fail(w, 404, "工程不存在或无权访问")
		return
	}
	v, err := a.loadVersion(r.Context(), vid)
	if err != nil {
		dbError(w, err)
		return
	}
	raw, _ := json.Marshal(v)
	var item map[string]any
	_ = json.Unmarshal(raw, &item)
	allowed := u.Role == "admin" || uid == u.ID
	if !allowed {
		delete(item, "github_url")
		delete(item, "github_ref")
		delete(item, "zip_sha256")
	}
	item["project_id"] = pid
	item["version_id"] = vid
	item["can_download"] = allowed
	if allowed {
		item["zip_url"] = "/open/v1/projects/" + pid + "/engineering/" + eid + "/zip"
	}
	writeJSON(w, 200, item)
}
