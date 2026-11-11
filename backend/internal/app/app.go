package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

type App struct {
	DB               *pgxpool.Pool
	UploadDir        string
	SecureCookie     bool
	BackupKey        []byte
	GitHubAPIBase    string
	GitHubHTTPClient *http.Client
}
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}
type authKey struct{}
type authState struct {
	User      User
	CSRF      string
	Bearer    bool
	SessionID string
	TokenID   string
}

func userFrom(r *http.Request) User {
	state, _ := r.Context().Value(authKey{}).(authState)
	return state.User
}
func stateFrom(r *http.Request) authState {
	state, _ := r.Context().Value(authKey{}).(authState)
	return state
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if status != 204 {
		_ = json.NewEncoder(w).Encode(v)
	}
}
func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := d.Decode(v); err != nil {
		fail(w, 400, "请求数据格式不正确")
		return false
	}
	return true
}
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func randomToken() string       { b := make([]byte, 32); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func tokenHash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func validID(s string) bool {
	if len(s) != 36 {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return err == nil && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}
func intQuery(r *http.Request, name string, def int) int {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def
	}
	n, e := strconv.Atoi(s)
	if e != nil || n < 0 {
		return def
	}
	return n
}
func pagination(r *http.Request) (int, int) {
	p := intQuery(r, "page", 1)
	if p < 1 {
		p = 1
	}
	n := intQuery(r, "page_size", 20)
	if n < 1 {
		n = 20
	}
	if n > 100 {
		n = 100
	}
	return n, (p - 1) * n
}
func listJSON(w http.ResponseWriter, items []any, total int) {
	if items == nil {
		items = []any{}
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}
func (a *App) audit(ctx context.Context, tx pgx.Tx, actor, action, targetType, target, project, engineering string, details any) error {
	raw, _ := json.Marshal(details)
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_type,target_id,project_id,engineering_id,details) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, newID(), nullable(actor), action, targetType, nullable(target), nullable(project), nullable(engineering), raw)
	return err
}
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func dbError(w http.ResponseWriter, err error) {
	if errors.Is(err, errPermission) {
		fail(w, 403, "账号或权限已变化，请重新登录")
		return
	}
	log.Printf("database error: %v", err)
	fail(w, 500, "服务暂时不可用，请稍后重试")
}

var errPermission = errors.New("permission revoked")

// Lock the user before its credential; revocation uses the same order until commit.
func (a *App) beginAuthorized(ctx context.Context, user User, admin bool) (pgx.Tx, error) {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if err = authorizeTx(ctx, tx, user, admin); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func authorizeTx(ctx context.Context, tx pgx.Tx, user User, admin bool) error {
	var active bool
	var role string
	err := tx.QueryRow(ctx, `SELECT active,role FROM users WHERE id=$1 FOR UPDATE`, user.ID).Scan(&active, &role)
	if err != nil || !active || role != user.Role || (admin && role != "admin") {
		return errPermission
	}
	state, ok := ctx.Value(authKey{}).(authState)
	if !ok || state.User.ID != user.ID {
		return errPermission
	}
	var credentialUser string
	if state.Bearer {
		if role != "admin" {
			return errPermission
		}
		err = tx.QueryRow(ctx, `SELECT user_id FROM api_tokens WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL FOR SHARE`, state.TokenID, user.ID).Scan(&credentialUser)
	} else {
		err = tx.QueryRow(ctx, `SELECT user_id FROM sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() FOR SHARE`, state.SessionID, user.ID).Scan(&credentialUser)
	}
	if err != nil {
		return errPermission
	}
	return nil
}
func (a *App) Migrate(ctx context.Context) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(763490021)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version text PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var applied bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, entry.Name()).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		sql, readErr := migrations.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			return readErr
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, entry.Name()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (a *App) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := a.DB.Ping(ctx); err != nil {
			fail(w, 503, "数据库不可用")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	r.Post("/api/v1/auth/login", a.login)
	r.Group(func(r chi.Router) {
		r.Use(a.authenticate)
		r.Get("/api/v1/auth/me", a.me)
		r.Post("/api/v1/auth/logout", a.logout)
		r.Post("/api/v1/auth/password", a.changePassword)
		a.registerUsers(r)
		a.registerEngineering(r)
		a.registerProjects(r)
		a.registerTags(r)
		a.registerTokens(r)
		a.registerOverview(r)
		a.registerBackups(r)
	})
	return r
}
