package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type APIToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	Scope      string     `json:"scope"`
}

func (a *App) registerTokens(r chi.Router) {
	r.Get("/api/v1/tokens", a.listTokens)
	r.Post("/api/v1/tokens", a.createToken)
	r.Delete("/api/v1/tokens/{id}", a.deleteToken)
}
func (a *App) listTokens(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	rows, err := a.DB.Query(r.Context(), `SELECT id,name,prefix,created_at,last_used_at,revoked_at FROM api_tokens WHERE user_id=$1 ORDER BY created_at DESC`, u.ID)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var t APIToken
		if err = rows.Scan(&t.ID, &t.Name, &t.Prefix, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt); err != nil {
			dbError(w, err)
			return
		}
		t.Scope = "read"
		if u.Role == "admin" {
			t.Scope = "admin"
		}
		items = append(items, t)
	}
	listJSON(w, items, len(items))
}
func (a *App) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		fail(w, 400, "请填写令牌名称")
		return
	}
	u := userFrom(r)
	value := "ph_" + randomToken()
	t := APIToken{ID: newID(), Name: in.Name, Prefix: value[:11], Scope: "read"}
	if u.Role == "admin" {
		t.Scope = "admin"
	}
	tx, err := a.beginAuthorized(r.Context(), u, false)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	err = tx.QueryRow(r.Context(), `INSERT INTO api_tokens(id,user_id,name,prefix,token_hash) VALUES($1,$2,$3,$4,$5) RETURNING created_at`, t.ID, u.ID, t.Name, t.Prefix, tokenHash(value)).Scan(&t.CreatedAt)
	if err == nil {
		err = a.audit(r.Context(), tx, u.ID, "token.create", "api_token", t.ID, "", "", map[string]string{"name": t.Name})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"token": value, "id": t.ID, "name": t.Name, "prefix": t.Prefix, "created_at": t.CreatedAt, "scope": t.Scope})
}
func (a *App) deleteToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "令牌不存在")
		return
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), false)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `UPDATE api_tokens SET revoked_at=coalesce(revoked_at,now()) WHERE id=$1 AND user_id=$2`, id, userFrom(r).ID)
	if err != nil {
		dbError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, 404, "令牌不存在")
		return
	}
	err = a.audit(r.Context(), tx, userFrom(r).ID, "token.revoke", "api_token", id, "", "", map[string]any{})
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
