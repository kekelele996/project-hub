package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Project struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	StartAt          time.Time `json:"start_at"`
	EndAt            time.Time `json:"end_at"`
	TechStack        []string  `json:"tech_stack"`
	MinFiles         int       `json:"min_files"`
	MaxFiles         int       `json:"max_files"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	EngineeringCount int       `json:"engineering_count"`
	ClaimedCount     int       `json:"claimed_count"`
	MemberCount      int       `json:"member_count"`
}
type projectInput struct {
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	StartAt        time.Time `json:"start_at"`
	EndAt          time.Time `json:"end_at"`
	TechStack      []string  `json:"tech_stack"`
	MinFiles       int       `json:"min_files"`
	MaxFiles       int       `json:"max_files"`
	Status         string    `json:"status"`
	EngineeringIDs []string  `json:"engineering_ids"`
	MemberIDs      []string  `json:"member_ids"`
}

const projectSelect = `SELECT p.id,p.name,p.description,p.start_at,p.end_at,p.tech_stack,p.min_files,p.max_files,p.status,p.created_at,(SELECT count(*) FROM project_engineerings pe WHERE pe.project_id=p.id AND pe.active),(SELECT count(*) FROM claims c JOIN project_engineerings pe ON pe.id=c.project_engineering_id WHERE pe.project_id=p.id AND pe.active AND c.released_at IS NULL),(SELECT count(*) FROM project_members pm WHERE pm.project_id=p.id) FROM projects p`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	var stack []byte
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.StartAt, &p.EndAt, &stack, &p.MinFiles, &p.MaxFiles, &p.Status, &p.CreatedAt, &p.EngineeringCount, &p.ClaimedCount, &p.MemberCount)
	_ = json.Unmarshal(stack, &p.TechStack)
	if p.TechStack == nil {
		p.TechStack = []string{}
	}
	return p, err
}
func (a *App) canAccessProject(ctx context.Context, u User, id string) bool {
	if !validID(id) {
		return false
	}
	var allowed bool
	err := a.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects p WHERE p.id=$1 AND ($2='admin' OR EXISTS(SELECT 1 FROM project_members pm WHERE pm.project_id=p.id AND pm.user_id=$3)))`, id, u.Role, u.ID).Scan(&allowed)
	return err == nil && allowed
}
func (a *App) registerProjects(r chi.Router) {
	r.Get("/api/v1/me/projects", a.listProjects)
	r.Get("/api/v1/projects/{id}", a.getProject)
	r.Get("/api/v1/projects/{id}/engineering", a.projectEngineering)
	r.Get("/api/v1/projects/{id}/members", a.projectMembers)
	r.Post("/api/v1/projects/{id}/engineering/{engineeringId}/claim", a.claimEngineering)
	r.Post("/api/v1/projects/{id}/engineering/{engineeringId}/release", a.releaseEngineering)
	r.Get("/api/v1/projects/{id}/engineering/{engineeringId}/zip", a.downloadProjectZip)
	r.Get("/open/v1/me/projects", a.listProjects)
	r.Get("/open/v1/projects/{id}/engineering", a.projectEngineering)
	r.Get("/open/v1/projects/{id}/engineering/{engineeringId}/zip", a.downloadProjectZip)
	r.Get("/open/v1/engineering/{id}", a.openEngineering)
	r.Group(func(r chi.Router) {
		r.Use(requireAdmin)
		r.Get("/api/v1/admin/projects", a.listProjects)
		r.Post("/api/v1/admin/projects", a.createProject)
		r.Patch("/api/v1/admin/projects/{id}", a.updateProject)
		r.Post("/api/v1/admin/projects/{id}/engineering-scope", a.updateScope)
		r.Post("/api/v1/admin/projects/{id}/members", a.updateMembers)
		r.Post("/api/v1/admin/claims/{id}/reclaim", a.reclaimClaim)
		r.Post("/api/v1/admin/claims/{id}/transfer", a.transferClaim)
	})
}
func (a *App) listProjects(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	n, o := pagination(r)
	status := r.URL.Query().Get("status")
	if status == "all" {
		status = ""
	}
	if status != "" && status != "active" && status != "upcoming" && status != "expired" && status != "paused" && status != "archived" {
		fail(w, 400, "项目状态无效")
		return
	}
	where := ` WHERE ($1='admin' OR EXISTS(SELECT 1 FROM project_members pm WHERE pm.project_id=p.id AND pm.user_id=$2)) AND ($3='' OR p.name ILIKE '%'||$3||'%' OR p.description ILIKE '%'||$3||'%') AND ($4='' OR ($4='active' AND p.status='active' AND p.start_at<=now() AND p.end_at>=now()) OR ($4='upcoming' AND p.status='active' AND p.start_at>now()) OR ($4='expired' AND p.status='active' AND p.end_at<now()) OR ($4 IN ('paused','archived') AND p.status=$4))`
	var total int
	err := a.DB.QueryRow(r.Context(), `SELECT count(*) FROM projects p`+where, u.Role, u.ID, r.URL.Query().Get("q"), status).Scan(&total)
	if err != nil {
		dbError(w, err)
		return
	}
	rows, err := a.DB.Query(r.Context(), projectSelect+where+` ORDER BY p.created_at DESC LIMIT $5 OFFSET $6`, u.Role, u.ID, r.URL.Query().Get("q"), status, n, o)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		items = append(items, p)
	}
	listJSON(w, items, total)
}
func (a *App) getProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !a.canAccessProject(r.Context(), userFrom(r), id) {
		fail(w, 404, "项目不存在或无权访问")
		return
	}
	p, err := scanProject(a.DB.QueryRow(r.Context(), projectSelect+` WHERE p.id=$1`, id))
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, p)
}
func validateProject(in *projectInput) bool {
	in.Name = strings.TrimSpace(in.Name)
	if in.Status == "" {
		in.Status = "active"
	}
	return in.Name != "" && !in.StartAt.IsZero() && !in.EndAt.Before(in.StartAt) && in.MinFiles >= 0 && in.MaxFiles >= 0 && (in.MaxFiles == 0 || in.MaxFiles >= in.MinFiles) && (in.Status == "active" || in.Status == "paused" || in.Status == "archived")
}
func (a *App) createProject(w http.ResponseWriter, r *http.Request) {
	var in projectInput
	if !decode(w, r, &in) {
		return
	}
	if !validateProject(&in) {
		fail(w, 400, "项目名称、时间范围或筛选条件无效")
		return
	}
	if in.TechStack == nil {
		in.TechStack = []string{}
	}
	stack, _ := json.Marshal(in.TechStack)
	id := newID()
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `INSERT INTO projects(id,name,description,start_at,end_at,tech_stack,min_files,max_files,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, in.Name, in.Description, in.StartAt, in.EndAt, stack, in.MinFiles, in.MaxFiles, in.Status)
	if err == nil {
		err = a.addMembers(r.Context(), tx, id, in.MemberIDs)
	}
	if err == nil {
		err = a.addScope(r.Context(), tx, id, in.EngineeringIDs)
	}
	if err != nil {
		fail(w, 400, "请确认成员有效且工程已有可用版本")
		return
	}
	err = a.audit(r.Context(), tx, userFrom(r).ID, "project.create", "project", id, id, "", map[string]any{"name": in.Name})
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	p, err := scanProject(a.DB.QueryRow(r.Context(), projectSelect+` WHERE p.id=$1`, id))
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 201, p)
}
func (a *App) updateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "项目不存在")
		return
	}
	var patch map[string]json.RawMessage
	if !decode(w, r, &patch) {
		return
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var in projectInput
	var stack []byte
	err = tx.QueryRow(r.Context(), `SELECT name,description,start_at,end_at,tech_stack,min_files,max_files,status FROM projects WHERE id=$1 FOR UPDATE`, id).Scan(&in.Name, &in.Description, &in.StartAt, &in.EndAt, &stack, &in.MinFiles, &in.MaxFiles, &in.Status)
	if err == pgx.ErrNoRows {
		fail(w, 404, "项目不存在")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	_ = json.Unmarshal(stack, &in.TechStack)
	base, _ := json.Marshal(in)
	var data map[string]json.RawMessage
	_ = json.Unmarshal(base, &data)
	for k, v := range patch {
		data[k] = v
	}
	merged, _ := json.Marshal(data)
	if err = json.Unmarshal(merged, &in); err != nil || !validateProject(&in) {
		fail(w, 400, "项目字段无效")
		return
	}
	stack, _ = json.Marshal(in.TechStack)
	_, err = tx.Exec(r.Context(), `UPDATE projects SET name=$2,description=$3,start_at=$4,end_at=$5,tech_stack=$6,min_files=$7,max_files=$8,status=$9 WHERE id=$1`, id, in.Name, in.Description, in.StartAt, in.EndAt, stack, in.MinFiles, in.MaxFiles, in.Status)
	if err == nil {
		err = a.audit(r.Context(), tx, userFrom(r).ID, "project.update", "project", id, id, "", patch)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	a.getProject(w, r)
}
func (a *App) addScope(ctx context.Context, tx pgx.Tx, project string, ids []string) error {
	for _, id := range ids {
		if !validID(id) {
			return errors.New("invalid engineering id")
		}
		var v string
		err := tx.QueryRow(ctx, `SELECT v.id FROM engineering_versions v JOIN engineering e ON e.id=v.engineering_id WHERE e.id=$1 AND NOT e.archived AND v.status='ready' ORDER BY v.version_number DESC LIMIT 1`, id).Scan(&v)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO project_engineerings(id,project_id,engineering_id,version_id) VALUES($1,$2,$3,$4) ON CONFLICT(project_id,engineering_id) DO UPDATE SET active=true,version_id=EXCLUDED.version_id WHERE NOT project_engineerings.active`, newID(), project, id, v)
		if err != nil {
			return err
		}
	}
	return nil
}
func (a *App) addMembers(ctx context.Context, tx pgx.Tx, project string, ids []string) error {
	for _, id := range ids {
		if !validID(id) {
			return errors.New("invalid member id")
		}
		var exists bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND active)`, id).Scan(&exists)
		if err != nil || !exists {
			return errors.New("inactive member")
		}
		_, err = tx.Exec(ctx, `INSERT INTO project_members(project_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, project, id)
		if err != nil {
			return err
		}
	}
	return nil
}
func lockProject(ctx context.Context, tx pgx.Tx, id string) error {
	if !validID(id) {
		return pgx.ErrNoRows
	}
	var out string
	return tx.QueryRow(ctx, `SELECT id FROM projects WHERE id=$1 FOR UPDATE`, id).Scan(&out)
}
func (a *App) updateScope(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in struct {
		Add      []string `json:"add"`
		Remove   []string `json:"remove"`
		Upgrades []struct {
			EngineeringID string `json:"engineering_id"`
			VersionID     string `json:"version_id"`
		} `json:"upgrades"`
	}
	if !decode(w, r, &in) {
		return
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockProject(r.Context(), tx, id); err != nil {
		fail(w, 404, "项目不存在")
		return
	}
	if err = a.addScope(r.Context(), tx, id, in.Add); err != nil {
		fail(w, 400, "只能添加存在且分析完成的工程")
		return
	}
	for _, eid := range in.Remove {
		if !validID(eid) {
			fail(w, 400, "工程标识无效")
			return
		}
		var claimed bool
		err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM claims c JOIN project_engineerings pe ON pe.id=c.project_engineering_id WHERE pe.project_id=$1 AND pe.engineering_id=$2 AND c.released_at IS NULL)`, id, eid).Scan(&claimed)
		if err != nil {
			dbError(w, err)
			return
		}
		if claimed {
			fail(w, 409, "已领取工程须先归还或回收")
			return
		}
		_, err = tx.Exec(r.Context(), `UPDATE project_engineerings SET active=false WHERE project_id=$1 AND engineering_id=$2`, id, eid)
		if err != nil {
			dbError(w, err)
			return
		}
	}
	for _, up := range in.Upgrades {
		if !validID(up.EngineeringID) || !validID(up.VersionID) {
			fail(w, 400, "版本标识无效")
			return
		}
		var pe string
		err = tx.QueryRow(r.Context(), `SELECT pe.id FROM project_engineerings pe JOIN engineering_versions v ON v.engineering_id=pe.engineering_id WHERE pe.project_id=$1 AND pe.engineering_id=$2 AND pe.active AND v.id=$3 AND v.status='ready' AND NOT EXISTS(SELECT 1 FROM claims c WHERE c.project_engineering_id=pe.id AND c.released_at IS NULL) FOR UPDATE OF pe`, id, up.EngineeringID, up.VersionID).Scan(&pe)
		if err == pgx.ErrNoRows {
			fail(w, 409, "工程已领取或目标版本不可用")
			return
		}
		if err != nil {
			dbError(w, err)
			return
		}
		_, err = tx.Exec(r.Context(), `UPDATE project_engineerings SET version_id=$1 WHERE id=$2`, up.VersionID, pe)
		if err != nil {
			dbError(w, err)
			return
		}
	}
	err = a.audit(r.Context(), tx, userFrom(r).ID, "project.scope", "project", id, id, "", in)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) projectMembers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !a.canAccessProject(r.Context(), userFrom(r), id) {
		fail(w, 404, "项目不存在或无权访问")
		return
	}
	rows, err := a.DB.Query(r.Context(), `SELECT u.id,u.username,u.name,u.role,u.active,u.created_at FROM project_members pm JOIN users u ON u.id=pm.user_id WHERE pm.project_id=$1 ORDER BY u.name`, id)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var u User
		if err = rows.Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.Active, &u.CreatedAt); err != nil {
			dbError(w, err)
			return
		}
		items = append(items, u)
	}
	listJSON(w, items, len(items))
}
func (a *App) updateMembers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in struct {
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if !decode(w, r, &in) {
		return
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockProject(r.Context(), tx, id); err != nil {
		fail(w, 404, "项目不存在")
		return
	}
	if err = a.addMembers(r.Context(), tx, id, in.Add); err != nil {
		fail(w, 400, "成员不存在或已停用")
		return
	}
	for _, uid := range in.Remove {
		if !validID(uid) {
			fail(w, 400, "成员标识无效")
			return
		}
		_, err = tx.Exec(r.Context(), `UPDATE claims SET released_at=now(),release_reason='member_removed' WHERE user_id=$1 AND released_at IS NULL AND project_engineering_id IN (SELECT id FROM project_engineerings WHERE project_id=$2)`, uid, id)
		if err == nil {
			_, err = tx.Exec(r.Context(), `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, id, uid)
		}
		if err != nil {
			dbError(w, err)
			return
		}
	}
	err = a.audit(r.Context(), tx, userFrom(r).ID, "project.members", "project", id, id, "", in)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type Claim struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	EngineeringID string    `json:"engineering_id"`
	UserID        string    `json:"user_id"`
	ClaimedAt     time.Time `json:"claimed_at"`
}

func (a *App) claimEngineering(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	pid, eid := chi.URLParam(r, "id"), chi.URLParam(r, "engineeringId")
	if !validID(pid) || !validID(eid) {
		fail(w, 404, "工程不存在")
		return
	}
	tx, err := a.beginAuthorized(r.Context(), u, false)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var start, end time.Time
	var status string
	err = tx.QueryRow(r.Context(), `SELECT start_at,end_at,status FROM projects WHERE id=$1 FOR UPDATE`, pid).Scan(&start, &end, &status)
	if err != nil {
		fail(w, 404, "项目不存在")
		return
	}
	var member bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM project_members pm JOIN users u ON u.id=pm.user_id WHERE pm.project_id=$1 AND pm.user_id=$2 AND u.active)`, pid, u.ID).Scan(&member)
	if err != nil {
		dbError(w, err)
		return
	}
	if !member {
		fail(w, 403, "请先加入项目再领取工程")
		return
	}
	now := time.Now()
	if status != "active" || now.Before(start) || now.After(end) {
		fail(w, 409, "项目当前不开放领取")
		return
	}
	var pe string
	err = tx.QueryRow(r.Context(), `SELECT pe.id FROM project_engineerings pe JOIN engineering_versions v ON v.id=pe.version_id WHERE pe.project_id=$1 AND pe.engineering_id=$2 AND pe.active AND v.status='ready' FOR UPDATE OF pe`, pid, eid).Scan(&pe)
	if err != nil {
		fail(w, 404, "工程不在项目中或尚未就绪")
		return
	}
	var existing Claim
	existing.ProjectID, existing.EngineeringID = pid, eid
	err = tx.QueryRow(r.Context(), `SELECT id,user_id,claimed_at FROM claims WHERE project_engineering_id=$1 AND released_at IS NULL`, pe).Scan(&existing.ID, &existing.UserID, &existing.ClaimedAt)
	if err != nil && err != pgx.ErrNoRows {
		dbError(w, err)
		return
	}
	if err == nil && existing.UserID == u.ID {
		writeJSON(w, 200, existing)
		return
	}
	if err == nil {
		fail(w, 409, "此工程已被领取")
		return
	}
	c := Claim{ID: newID(), ProjectID: pid, EngineeringID: eid, UserID: u.ID}
	err = tx.QueryRow(r.Context(), `INSERT INTO claims(id,project_engineering_id,user_id,version_id) SELECT $1,id,$3,version_id FROM project_engineerings WHERE id=$2 RETURNING claimed_at`, c.ID, pe, u.ID).Scan(&c.ClaimedAt)
	if err == nil {
		err = a.audit(r.Context(), tx, u.ID, "claim.create", "claim", c.ID, pid, eid, map[string]any{"user_id": u.ID})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, 409, "此工程已被领取")
		return
	}
	writeJSON(w, 201, c)
}
func (a *App) releaseEngineering(w http.ResponseWriter, r *http.Request) {
	pid, eid := chi.URLParam(r, "id"), chi.URLParam(r, "engineeringId")
	var in struct {
		ClaimID string `json:"claim_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validID(pid) || !validID(eid) || !validID(in.ClaimID) {
		fail(w, 400, "请提供当前领取记录")
		return
	}
	u := userFrom(r)
	tx, err := a.beginAuthorized(r.Context(), u, false)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockProject(r.Context(), tx, pid); err != nil {
		fail(w, 404, "项目不存在")
		return
	}
	var pe string
	err = tx.QueryRow(r.Context(), `SELECT id FROM project_engineerings WHERE project_id=$1 AND engineering_id=$2 FOR UPDATE`, pid, eid).Scan(&pe)
	if err != nil {
		fail(w, 404, "项目工程不存在")
		return
	}
	tag, err := tx.Exec(r.Context(), `UPDATE claims SET released_at=now(),release_reason='returned' WHERE id=$1 AND project_engineering_id=$2 AND user_id=$3 AND released_at IS NULL`, in.ClaimID, pe, u.ID)
	if err != nil {
		dbError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, 409, "领取记录已变化，请刷新后重试")
		return
	}
	err = a.audit(r.Context(), tx, u.ID, "claim.release", "claim", in.ClaimID, pid, eid, map[string]any{})
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 204, nil)
}
func (a *App) reclaimClaim(w http.ResponseWriter, r *http.Request)  { a.moveClaim(w, r, false) }
func (a *App) transferClaim(w http.ResponseWriter, r *http.Request) { a.moveClaim(w, r, true) }
func (a *App) moveClaim(w http.ResponseWriter, r *http.Request, transfer bool) {
	cid := chi.URLParam(r, "id")
	if !validID(cid) {
		fail(w, 404, "领取记录不存在")
		return
	}
	var target string
	if transfer {
		var in struct {
			UserID string `json:"user_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		target = in.UserID
		if !validID(target) {
			fail(w, 400, "目标成员无效")
			return
		}
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var pid, eid, pe string
	err = tx.QueryRow(r.Context(), `SELECT pe.project_id,pe.engineering_id,pe.id FROM claims c JOIN project_engineerings pe ON pe.id=c.project_engineering_id WHERE c.id=$1`, cid).Scan(&pid, &eid, &pe)
	if err != nil {
		fail(w, 404, "领取记录不存在")
		return
	}
	if err = lockProject(r.Context(), tx, pid); err != nil {
		dbError(w, err)
		return
	}
	var out string
	err = tx.QueryRow(r.Context(), `SELECT id FROM project_engineerings WHERE id=$1 FOR UPDATE`, pe).Scan(&out)
	if err != nil {
		dbError(w, err)
		return
	}
	if transfer {
		var valid bool
		err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM project_members pm JOIN users u ON u.id=pm.user_id WHERE pm.project_id=$1 AND pm.user_id=$2 AND u.active)`, pid, target).Scan(&valid)
		if err != nil || !valid {
			fail(w, 400, "目标用户必须是有效项目成员")
			return
		}
	}
	reason, action := "reclaimed", "claim.reclaim"
	if transfer {
		reason, action = "transferred", "claim.transfer"
	}
	tag, err := tx.Exec(r.Context(), `UPDATE claims SET released_at=now(),release_reason=$2 WHERE id=$1 AND released_at IS NULL`, cid, reason)
	if err != nil {
		dbError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, 409, "领取记录已变化")
		return
	}
	newClaim := ""
	if transfer {
		newClaim = newID()
		_, err = tx.Exec(r.Context(), `INSERT INTO claims(id,project_engineering_id,user_id,version_id) SELECT $1,id,$3,version_id FROM project_engineerings WHERE id=$2`, newClaim, pe, target)
	}
	if err == nil {
		err = a.audit(r.Context(), tx, userFrom(r).ID, action, "claim", cid, pid, eid, map[string]any{"user_id": target, "new_claim_id": newClaim})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "claim_id": newClaim})
}
