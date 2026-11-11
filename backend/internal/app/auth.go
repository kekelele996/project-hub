package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"
)

func passwordHash(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	sum := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
	return fmt.Sprintf("argon2id$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(sum))
}
func passwordValid(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[1])
	expected, e2 := base64.RawStdEncoding.DecodeString(parts[2])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(expected) != 32 {
		return false
	}
	sum := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(sum, expected) == 1
}
func validPassword(s string) bool { return len(s) >= 8 && len(s) <= 256 }
func (a *App) BootstrapAdmin(ctx context.Context, username, password, name string) error {
	var count int
	err := a.DB.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin'`).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if !validPassword(password) {
		return errors.New("ADMIN_PASSWORD must contain at least 8 characters")
	}
	_, err = a.DB.Exec(ctx, `INSERT INTO users(id,username,name,password_hash,role) VALUES($1,$2,$3,$4,'admin') ON CONFLICT(username) DO NOTHING`, newID(), username, name, passwordHash(password))
	return err
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var input struct{ Username, Password string }
	if !decode(w, r, &input) {
		return
	}
	var u User
	var hash string
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	err = tx.QueryRow(r.Context(), `SELECT id,username,name,role,active,created_at,password_hash FROM users WHERE username=$1 FOR SHARE`, strings.TrimSpace(input.Username)).Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.Active, &u.CreatedAt, &hash)
	if err != nil && err != pgx.ErrNoRows {
		dbError(w, err)
		return
	}
	if err != nil || !u.Active || !passwordValid(input.Password, hash) {
		fail(w, 401, "用户名或密码错误")
		return
	}
	token, csrf := randomToken(), randomToken()
	_, err = tx.Exec(r.Context(), `INSERT INTO sessions(id,user_id,token_hash,csrf_token,expires_at) VALUES($1,$2,$3,$4,$5)`, newID(), u.ID, tokenHash(token), csrf, time.Now().Add(365*24*time.Hour))
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	a.setCookie(w, token, 365*24*60*60)
	writeJSON(w, 200, map[string]any{"user": u, "csrf": csrf})
}
func (a *App) setCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: "projecthub_session", Value: token, Path: "/", HttpOnly: true, Secure: a.SecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}
func (a *App) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var state authState
		var err error
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			state, err = a.authenticateCredential(r.Context(), tokenHash(strings.TrimPrefix(auth, "Bearer ")), true)
		} else {
			var c *http.Cookie
			c, err = r.Cookie("projecthub_session")
			if err == nil {
				state, err = a.authenticateCredential(r.Context(), tokenHash(c.Value), false)
				if err == nil {
					a.setCookie(w, c.Value, 365*24*60*60)
				}
			}
		}
		if err != nil {
			fail(w, 401, "请先登录")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/open/") && !state.Bearer {
			fail(w, 401, "开放接口需要 Bearer Token")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if state.Bearer && state.User.Role != "admin" {
				fail(w, 403, "成员 API 令牌仅可读取")
				return
			}
			if !state.Bearer && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(state.CSRF)) != 1 {
				fail(w, 403, "请求校验失败，请刷新后重试")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authKey{}, state)))
	})
}

func (a *App) authenticateCredential(ctx context.Context, hash string, bearer bool) (authState, error) {
	state := authState{Bearer: bearer}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return state, err
	}
	defer tx.Rollback(ctx)
	var userID string
	if bearer {
		err = tx.QueryRow(ctx, `SELECT user_id FROM api_tokens WHERE token_hash=$1 AND revoked_at IS NULL`, hash).Scan(&userID)
	} else {
		err = tx.QueryRow(ctx, `SELECT user_id FROM sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp()`, hash).Scan(&userID)
	}
	if err != nil {
		return state, err
	}
	// Refresh metadata with the same user-before-credential order as write handlers.
	err = tx.QueryRow(ctx, `SELECT id,username,name,role,active,created_at FROM users WHERE id=$1 AND active FOR SHARE`, userID).Scan(&state.User.ID, &state.User.Username, &state.User.Name, &state.User.Role, &state.User.Active, &state.User.CreatedAt)
	if err != nil {
		return state, err
	}
	if bearer {
		err = tx.QueryRow(ctx, `SELECT id FROM api_tokens WHERE token_hash=$1 AND user_id=$2 AND revoked_at IS NULL FOR UPDATE`, hash, userID).Scan(&state.TokenID)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE api_tokens SET last_used_at=clock_timestamp() WHERE id=$1`, state.TokenID)
		}
	} else {
		err = tx.QueryRow(ctx, `SELECT id,csrf_token FROM sessions WHERE token_hash=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() FOR UPDATE`, hash, userID).Scan(&state.SessionID, &state.CSRF)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '365 days' WHERE id=$1`, state.SessionID)
		}
	}
	if err != nil {
		return state, err
	}
	err = tx.Commit(ctx)
	return state, err
}
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r).Role != "admin" {
			fail(w, 403, "仅管理员可以执行此操作")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *App) me(w http.ResponseWriter, r *http.Request) {
	s := stateFrom(r)
	writeJSON(w, 200, map[string]any{"user": s.User, "csrf": s.CSRF})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	s := stateFrom(r)
	if s.Bearer {
		fail(w, 400, "请通过令牌管理撤销令牌")
		return
	}
	tx, err := a.beginAuthorized(r.Context(), s.User, false)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE id=$1`, s.SessionID)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	a.setCookie(w, "", -1)
	writeJSON(w, 204, nil)
}
func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current  string `json:"current_password"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validPassword(in.Password) {
		fail(w, 400, "密码长度须为 8 至 256 个字符")
		return
	}
	u := userFrom(r)
	tx, err := a.beginAuthorized(r.Context(), u, false)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var hash string
	if err = tx.QueryRow(r.Context(), `SELECT password_hash FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&hash); err != nil {
		dbError(w, err)
		return
	}
	if !passwordValid(in.Current, hash) {
		fail(w, 400, "当前密码不正确")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$1 WHERE id=$2`, passwordHash(in.Password), u.ID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1`, u.ID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE api_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, u.ID)
	}
	if err == nil {
		err = a.audit(r.Context(), tx, u.ID, "password.change", "user", u.ID, "", "", map[string]any{})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	a.setCookie(w, "", -1)
	writeJSON(w, 204, nil)
}
func (a *App) registerUsers(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(requireAdmin)
		r.Get("/api/v1/admin/users", a.listUsers)
		r.Post("/api/v1/admin/users", a.createUser)
		r.Patch("/api/v1/admin/users/{id}", a.updateUser)
	})
}
func (a *App) listUsers(w http.ResponseWriter, r *http.Request) {
	n, o := pagination(r)
	role := r.URL.Query().Get("role")
	if role == "all" {
		role = ""
	}
	if role != "" && role != "admin" && role != "member" {
		fail(w, 400, "用户角色无效")
		return
	}
	var active any
	switch r.URL.Query().Get("active") {
	case "", "all":
	case "true":
		active = true
	case "false":
		active = false
	default:
		fail(w, 400, "账号状态无效")
		return
	}
	where := ` WHERE ($1='' OR username ILIKE '%'||$1||'%' OR name ILIKE '%'||$1||'%') AND ($2='' OR role=$2) AND ($3::boolean IS NULL OR active=$3)`
	var total int
	err := a.DB.QueryRow(r.Context(), `SELECT count(*) FROM users`+where, r.URL.Query().Get("q"), role, active).Scan(&total)
	if err != nil {
		dbError(w, err)
		return
	}
	rows, err := a.DB.Query(r.Context(), `SELECT id,username,name,role,active,created_at FROM users`+where+` ORDER BY created_at DESC LIMIT $4 OFFSET $5`, r.URL.Query().Get("q"), role, active, n, o)
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
	listJSON(w, items, total)
}
func (a *App) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Name, Password, Role string }
	if !decode(w, r, &in) {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	in.Name = strings.TrimSpace(in.Name)
	if in.Role == "" {
		in.Role = "member"
	}
	if len(in.Username) < 2 || len(in.Username) > 100 || in.Name == "" || !validPassword(in.Password) || (in.Role != "admin" && in.Role != "member") {
		fail(w, 400, "请填写有效用户名、姓名、角色和至少 8 位密码")
		return
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var u User
	err = tx.QueryRow(r.Context(), `INSERT INTO users(id,username,name,password_hash,role) VALUES($1,$2,$3,$4,$5) RETURNING id,username,name,role,active,created_at`, newID(), in.Username, in.Name, passwordHash(in.Password), in.Role).Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.Active, &u.CreatedAt)
	if err != nil {
		fail(w, 409, "用户名已存在")
		return
	}
	err = a.audit(r.Context(), tx, userFrom(r).ID, "user.create", "user", u.ID, "", "", map[string]any{"name": u.Name})
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 201, u)
}
func (a *App) updateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "用户不存在")
		return
	}
	var in struct {
		Name     *string
		Role     *string
		Active   *bool
		Password *string
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role != nil && *in.Role != "admin" && *in.Role != "member" {
		fail(w, 400, "角色无效")
		return
	}
	if in.Password != nil && !validPassword(*in.Password) {
		fail(w, 400, "密码至少需要 8 位")
		return
	}
	tx, err := a.DB.Begin(r.Context())
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var u User
	// Serialize admin maintenance before actor/target locks to preserve a final admin.
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(763490022)`); err == nil {
		err = authorizeTx(r.Context(), tx, userFrom(r), true)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	err = tx.QueryRow(r.Context(), `SELECT id,username,name,role,active,created_at FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.Active, &u.CreatedAt)
	if err == pgx.ErrNoRows {
		fail(w, 404, "用户不存在")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if u.Role == "admin" && ((in.Role != nil && *in.Role != "admin") || (in.Active != nil && !*in.Active)) {
		var count int
		err = tx.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE role='admin' AND active AND id<>$1`, id).Scan(&count)
		if err != nil {
			dbError(w, err)
			return
		}
		if count == 0 {
			fail(w, 409, "至少保留一个有效管理员")
			return
		}
	}
	if in.Name != nil {
		u.Name = strings.TrimSpace(*in.Name)
	}
	if u.Name == "" {
		fail(w, 400, "姓名不能为空")
		return
	}
	if in.Role != nil {
		u.Role = *in.Role
	}
	if in.Active != nil {
		u.Active = *in.Active
	}
	_, err = tx.Exec(r.Context(), `UPDATE users SET name=$1,role=$2,active=$3 WHERE id=$4`, u.Name, u.Role, u.Active, id)
	if err == nil && in.Password != nil {
		_, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$1 WHERE id=$2`, passwordHash(*in.Password), id)
	}
	if err == nil && (in.Role != nil || in.Active != nil || in.Password != nil) {
		_, err = tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1`, id)
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE api_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
		}
	}
	if err == nil {
		err = a.audit(r.Context(), tx, userFrom(r).ID, "user.update", "user", id, "", "", map[string]any{"role": u.Role, "active": u.Active})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, u)
}
