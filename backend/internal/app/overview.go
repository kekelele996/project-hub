package app

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type AuditEvent struct {
	ID            string    `json:"id"`
	ActorName     string    `json:"actor_name"`
	Action        string    `json:"action"`
	TargetType    string    `json:"target_type"`
	TargetID      *string   `json:"target_id"`
	ProjectID     *string   `json:"project_id"`
	EngineeringID *string   `json:"engineering_id"`
	Details       any       `json:"details"`
	CreatedAt     time.Time `json:"created_at"`
}

func (a *App) registerOverview(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(requireAdmin)
		r.Get("/api/v1/admin/overview", a.overview)
		r.Get("/api/v1/admin/audit-events", a.auditEvents)
	})
}
func (a *App) overview(w http.ResponseWriter, r *http.Request) {
	var engineering, ready, pending, projects, active, claims, users int
	err := a.DB.QueryRow(r.Context(), `SELECT (SELECT count(*) FROM engineering WHERE NOT archived),(SELECT count(DISTINCT engineering_id) FROM engineering_versions WHERE status='ready'),(SELECT count(*) FROM analysis_jobs WHERE status IN ('pending','processing')),(SELECT count(*) FROM projects),(SELECT count(*) FROM projects WHERE status='active' AND start_at<=now() AND end_at>=now()),(SELECT count(*) FROM claims WHERE released_at IS NULL),(SELECT count(*) FROM users WHERE active)`).Scan(&engineering, &ready, &pending, &projects, &active, &claims, &users)
	if err != nil {
		dbError(w, err)
		return
	}
	events, _, err := a.readAudit(r, 8, 0)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"engineering_count": engineering, "ready_count": ready, "pending_count": pending, "project_count": projects, "active_project_count": active, "claim_count": claims, "user_count": users, "recent_events": events})
}
func (a *App) readAudit(r *http.Request, n, o int) ([]any, int, error) {
	q := r.URL.Query()
	where := ` WHERE ($1='' OR ae.action ILIKE '%'||$1||'%' OR u.name ILIKE '%'||$1||'%') AND ($2='' OR ae.actor_id::text=$2) AND ($3='' OR ae.project_id::text=$3) AND ($4='' OR ae.engineering_id::text=$4) AND ($5='' OR ae.action=$5)`
	args := []any{q.Get("q"), q.Get("user_id"), q.Get("project_id"), q.Get("engineering_id"), q.Get("action")}
	var total int
	err := a.DB.QueryRow(r.Context(), `SELECT count(*) FROM audit_events ae LEFT JOIN users u ON u.id=ae.actor_id`+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	args = append(args, n, o)
	rows, err := a.DB.Query(r.Context(), `SELECT ae.id,coalesce(u.name,'系统'),ae.action,ae.target_type,ae.target_id::text,ae.project_id::text,ae.engineering_id::text,ae.details,ae.created_at FROM audit_events ae LEFT JOIN users u ON u.id=ae.actor_id`+where+` ORDER BY ae.created_at DESC LIMIT $6 OFFSET $7`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var event AuditEvent
		var details []byte
		if err = rows.Scan(&event.ID, &event.ActorName, &event.Action, &event.TargetType, &event.TargetID, &event.ProjectID, &event.EngineeringID, &details, &event.CreatedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(details, &event.Details)
		items = append(items, event)
	}
	return items, total, rows.Err()
}
func (a *App) auditEvents(w http.ResponseWriter, r *http.Request) {
	n, o := pagination(r)
	items, total, err := a.readAudit(r, n, o)
	if err != nil {
		dbError(w, err)
		return
	}
	listJSON(w, items, total)
}
