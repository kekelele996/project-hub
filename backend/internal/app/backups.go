package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type GitHubAccount struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Login       string     `json:"login"`
	TokenSuffix string     `json:"token_suffix"`
	Active      bool       `json:"active"`
	ExpiresAt   *time.Time `json:"expires_at"`
	VerifiedAt  *time.Time `json:"verified_at"`
	LastError   string     `json:"last_error"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Status      string     `json:"status"`
	Ciphertext  string     `json:"-"`
	Revision    int64      `json:"-"`
}
type BackupTarget struct {
	ID              string     `json:"id"`
	EngineeringID   string     `json:"engineering_id"`
	EngineeringName string     `json:"engineering_name"`
	AccountID       string     `json:"account_id"`
	AccountName     string     `json:"account_name"`
	Login           string     `json:"login"`
	Owner           string     `json:"owner"`
	Repo            string     `json:"repo"`
	AutoSync        bool       `json:"auto_sync"`
	Managed         bool       `json:"managed"`
	Enabled         bool       `json:"enabled"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastStatus      string     `json:"last_status"`
	LastError       string     `json:"last_error"`
	LastSyncedAt    *time.Time `json:"last_synced_at"`
	Revision        int64      `json:"-"`
}
type BackupTargetGroup struct {
	EngineeringID   string `json:"engineering_id"`
	EngineeringName string `json:"engineering_name"`
	AccountCount    int    `json:"account_count"`
	RepositoryCount int    `json:"repository_count"`
	EnabledCount    int    `json:"enabled_count"`
	DisabledCount   int    `json:"disabled_count"`
}
type BackupJob struct {
	ID              string     `json:"id"`
	TargetID        string     `json:"target_id"`
	VersionID       string     `json:"version_id"`
	VersionNumber   int        `json:"version_number"`
	EngineeringID   string     `json:"engineering_id"`
	EngineeringName string     `json:"engineering_name"`
	AccountName     string     `json:"account_name"`
	Owner           string     `json:"owner"`
	Repo            string     `json:"repo"`
	Status          string     `json:"status"`
	Kind            string     `json:"kind"`
	Error           string     `json:"error"`
	ReleaseURL      string     `json:"release_url"`
	ZipSHA256       string     `json:"zip_sha256"`
	VerifiedAt      *time.Time `json:"verified_at"`
	CreatedAt       time.Time  `json:"created_at"`
	FinishedAt      *time.Time `json:"finished_at"`
}

type BackupRepositoryGroup struct {
	EngineeringID    string    `json:"engineering_id"`
	RepositoryCount  int       `json:"repository_count"`
	AccountCount     int       `json:"account_count"`
	Owner            string    `json:"owner"`
	Repo             string    `json:"repo"`
	EngineeringName  string    `json:"engineering_name"`
	EngineeringCount int       `json:"engineering_count"`
	VersionCount     int       `json:"version_count"`
	JobCount         int       `json:"job_count"`
	ReadyCount       int       `json:"ready_count"`
	FailedCount      int       `json:"failed_count"`
	PendingCount     int       `json:"pending_count"`
	RunningCount     int       `json:"running_count"`
	LatestAt         time.Time `json:"latest_at"`
}

type BackupVersionGroup struct {
	VersionID     string      `json:"version_id"`
	VersionNumber int         `json:"version_number"`
	Jobs          []BackupJob `json:"jobs"`
}

const accountColumns = `id,name,login,token_suffix,active,expires_at,verified_at,last_error,created_at,updated_at,token_ciphertext,revision`
const targetColumns = `t.id,t.engineering_id,e.name,t.account_id,a.name,a.login,t.owner,t.repo,t.auto_sync,t.enabled,t.created_at,t.updated_at,coalesce(j.status,''),coalesce(j.error,''),(SELECT verified_at FROM backup_jobs WHERE target_id=t.id AND kind='sync' AND status='ready' AND verified_at IS NOT NULL ORDER BY verified_at DESC LIMIT 1),t.revision,t.managed`
const targetRelations = ` JOIN engineering e ON e.id=t.engineering_id JOIN github_accounts a ON a.id=t.account_id LEFT JOIN LATERAL (SELECT status,error FROM backup_jobs WHERE target_id=t.id ORDER BY created_at DESC,id DESC LIMIT 1) j ON true`
const targetJoin = ` FROM backup_targets t` + targetRelations
const jobColumns = `j.id,j.target_id,j.version_id,v.version_number,e.id,e.name,a.name,t.owner,t.repo,j.status,j.kind,j.error,j.release_url,j.zip_sha256,j.verified_at,j.created_at,j.finished_at`
const jobJoin = ` FROM backup_jobs j JOIN backup_targets t ON t.id=j.target_id JOIN github_accounts a ON a.id=t.account_id JOIN engineering_versions v ON v.id=j.version_id JOIN engineering e ON e.id=v.engineering_id`

func scanAccount(row pgx.Row) (GitHubAccount, error) {
	var x GitHubAccount
	err := row.Scan(&x.ID, &x.Name, &x.Login, &x.TokenSuffix, &x.Active, &x.ExpiresAt, &x.VerifiedAt, &x.LastError, &x.CreatedAt, &x.UpdatedAt, &x.Ciphertext, &x.Revision)
	x.Status = "active"
	if !x.Active {
		x.Status = "disabled"
	} else if x.ExpiresAt != nil && !x.ExpiresAt.After(time.Now()) {
		x.Status = "expired"
	} else if x.LastError != "" {
		x.Status = "error"
	}
	return x, err
}
func scanTarget(row pgx.Row) (BackupTarget, error) {
	var x BackupTarget
	err := row.Scan(&x.ID, &x.EngineeringID, &x.EngineeringName, &x.AccountID, &x.AccountName, &x.Login, &x.Owner, &x.Repo, &x.AutoSync, &x.Enabled, &x.CreatedAt, &x.UpdatedAt, &x.LastStatus, &x.LastError, &x.LastSyncedAt, &x.Revision, &x.Managed)
	return x, err
}
func scanBackupJob(row pgx.Row) (BackupJob, error) {
	var x BackupJob
	err := row.Scan(&x.ID, &x.TargetID, &x.VersionID, &x.VersionNumber, &x.EngineeringID, &x.EngineeringName, &x.AccountName, &x.Owner, &x.Repo, &x.Status, &x.Kind, &x.Error, &x.ReleaseURL, &x.ZipSHA256, &x.VerifiedAt, &x.CreatedAt, &x.FinishedAt)
	return x, err
}
func (a *App) backupConfigured() bool { return len(a.BackupKey) == 32 }
func (a *App) requireBackupConfiguration(w http.ResponseWriter) bool {
	if !a.backupConfigured() {
		fail(w, 503, "未配置 GitHub 备份加密密钥，请联系管理员")
		return false
	}
	return true
}
func (a *App) githubClient(token string) *GitHubClient {
	return &GitHubClient{BaseURL: a.GitHubAPIBase, HTTPClient: a.GitHubHTTPClient, Token: token}
}
func (a *App) authorizeRemote(ctx context.Context, u User) error {
	tx, err := a.beginAuthorized(ctx, u, true)
	if err != nil {
		return err
	}
	return tx.Rollback(ctx)
}
func (a *App) readAccount(ctx context.Context, id string) (GitHubAccount, error) {
	return scanAccount(a.DB.QueryRow(ctx, `SELECT `+accountColumns+` FROM github_accounts WHERE id=$1`, id))
}
func (a *App) readTarget(ctx context.Context, id string) (BackupTarget, error) {
	return scanTarget(a.DB.QueryRow(ctx, `SELECT `+targetColumns+targetJoin+` WHERE t.id=$1`, id))
}
func (a *App) readBackupJob(ctx context.Context, id string) (BackupJob, error) {
	return scanBackupJob(a.DB.QueryRow(ctx, `SELECT `+jobColumns+jobJoin+` WHERE j.id=$1`, id))
}
func usableAccount(x GitHubAccount) error {
	if !x.Active {
		return errors.New("GitHub 账户已停用")
	}
	if x.ExpiresAt != nil && !x.ExpiresAt.After(time.Now()) {
		return errors.New("GitHub Token 已到期，请更新账户凭证")
	}
	return nil
}
func (a *App) registerBackups(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(requireAdmin)
		r.Get("/api/v1/admin/github-accounts", a.listGitHubAccounts)
		r.Post("/api/v1/admin/github-accounts", a.createGitHubAccount)
		r.Patch("/api/v1/admin/github-accounts/{id}", a.updateGitHubAccount)
		r.Post("/api/v1/admin/github-accounts/{id}/verify", a.verifyGitHubAccount)
		r.Get("/api/v1/admin/backup-targets", a.listBackupTargets)
		r.Post("/api/v1/admin/backup-targets", a.createBackupTarget)
		r.Patch("/api/v1/admin/backup-targets/{id}", a.updateBackupTarget)
		r.Post("/api/v1/admin/backup-targets/{id}/sync", a.syncBackupTarget)
		r.Get("/api/v1/admin/backup-jobs", a.listBackupJobs)
		r.Post("/api/v1/admin/backup-jobs/{id}/retry", a.retryBackupJob)
		r.Post("/api/v1/admin/backup-jobs/{id}/restore", a.restoreBackupJob)
	})
}
func (a *App) listGitHubAccounts(w http.ResponseWriter, r *http.Request) {
	n, o := pagination(r)
	var total, usableTotal int
	if err := a.DB.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE active AND verified_at IS NOT NULL AND (expires_at IS NULL OR expires_at>clock_timestamp())) FROM github_accounts`).Scan(&total, &usableTotal); err != nil {
		dbError(w, err)
		return
	}
	rows, err := a.DB.Query(r.Context(), `SELECT `+accountColumns+` FROM github_accounts ORDER BY created_at DESC LIMIT $1 OFFSET $2`, n, o)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []GitHubAccount{}
	for rows.Next() {
		x, err := scanAccount(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "usable_total": usableTotal, "configured": a.backupConfigured()})
}
func (a *App) createGitHubAccount(w http.ResponseWriter, r *http.Request) {
	if !a.requireBackupConfiguration(w) {
		return
	}
	var in struct {
		Name      string     `json:"name"`
		Token     string     `json:"token"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Token = strings.TrimSpace(in.Token)
	if in.Name == "" || len(in.Name) > 200 || len(in.Token) < 8 || len(in.Token) > 1000 || (in.ExpiresAt != nil && !in.ExpiresAt.After(time.Now())) {
		fail(w, 400, "请填写有效的账户名称、Token 和未来到期时间")
		return
	}
	u := userFrom(r)
	if err := a.authorizeRemote(r.Context(), u); err != nil {
		dbError(w, err)
		return
	}
	remote, err := a.githubClient(in.Token).User(r.Context())
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	id := newID()
	cipher, err := encryptBackupToken(a.BackupKey, id, in.Token)
	if err != nil {
		fail(w, 503, "备份加密密钥无效")
		return
	}
	tx, err := a.beginAuthorized(r.Context(), u, true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `INSERT INTO github_accounts(id,name,login,token_ciphertext,token_suffix,expires_at,verified_at) VALUES($1,$2,$3,$4,$5,$6,now())`, id, in.Name, remote.Login, cipher, maskBackupToken(in.Token), in.ExpiresAt)
	if err == nil {
		err = a.audit(r.Context(), tx, u.ID, "github.account.create", "github_account", id, "", "", map[string]string{"name": in.Name, "login": remote.Login})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	x, err := a.readAccount(r.Context(), id)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 201, x)
}
func (a *App) updateGitHubAccount(w http.ResponseWriter, r *http.Request) {
	if !a.requireBackupConfiguration(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "GitHub 账户不存在")
		return
	}
	var in struct {
		Name      *string         `json:"name"`
		Token     *string         `json:"token"`
		Active    *bool           `json:"active"`
		ExpiresAt json.RawMessage `json:"expires_at"`
	}
	if !decode(w, r, &in) {
		return
	}
	current, err := a.readAccount(r.Context(), id)
	if err != nil {
		fail(w, 404, "GitHub 账户不存在")
		return
	}
	expires := current.ExpiresAt
	if in.ExpiresAt != nil {
		expires = nil
		if string(in.ExpiresAt) != "null" {
			var value time.Time
			if json.Unmarshal(in.ExpiresAt, &value) != nil {
				fail(w, 400, "到期时间格式无效")
				return
			}
			expires = &value
		}
	}
	if in.Name != nil {
		*in.Name = strings.TrimSpace(*in.Name)
		if *in.Name == "" || len(*in.Name) > 200 {
			fail(w, 400, "账户名称无效")
			return
		}
	}
	u := userFrom(r)
	cipher, login, suffix := current.Ciphertext, current.Login, current.TokenSuffix
	verified := current.VerifiedAt
	lastError := current.LastError
	if in.Token != nil {
		*in.Token = strings.TrimSpace(*in.Token)
		if len(*in.Token) < 8 || len(*in.Token) > 1000 {
			fail(w, 400, "GitHub Token 无效")
			return
		}
		if err = a.authorizeRemote(r.Context(), u); err != nil {
			dbError(w, err)
			return
		}
		remote, err := a.githubClient(*in.Token).User(r.Context())
		if err != nil {
			fail(w, 422, err.Error())
			return
		}
		cipher, err = encryptBackupToken(a.BackupKey, id, *in.Token)
		if err != nil {
			fail(w, 503, "备份加密密钥无效")
			return
		}
		login, suffix = remote.Login, maskBackupToken(*in.Token)
		now := time.Now()
		verified = &now
		lastError = ""
	}
	tx, err := a.beginAuthorized(r.Context(), u, true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var revision int64
	err = tx.QueryRow(r.Context(), `SELECT revision FROM github_accounts WHERE id=$1 FOR UPDATE`, id).Scan(&revision)
	if err != nil {
		dbError(w, err)
		return
	}
	if revision != current.Revision {
		fail(w, 409, "账户信息已变化，请刷新后重试")
		return
	}
	if in.Name != nil {
		current.Name = *in.Name
	}
	if in.Active != nil {
		current.Active = *in.Active
	}
	_, err = tx.Exec(r.Context(), `UPDATE github_accounts SET name=$2,active=$3,expires_at=$4,token_ciphertext=$5,login=$6,token_suffix=$7,verified_at=$8,last_error=$9,revision=revision+1,updated_at=now() WHERE id=$1`, id, current.Name, current.Active, expires, cipher, login, suffix, verified, lastError)
	if err == nil {
		err = a.audit(r.Context(), tx, u.ID, "github.account.update", "github_account", id, "", "", map[string]any{"name": current.Name, "active": current.Active, "token_updated": in.Token != nil})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	x, err := a.readAccount(r.Context(), id)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, x)
}
func (a *App) verifyGitHubAccount(w http.ResponseWriter, r *http.Request) {
	if !a.requireBackupConfiguration(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "GitHub 账户不存在")
		return
	}
	x, err := a.readAccount(r.Context(), id)
	if err != nil {
		fail(w, 404, "GitHub 账户不存在")
		return
	}
	u := userFrom(r)
	if err = a.authorizeRemote(r.Context(), u); err != nil {
		dbError(w, err)
		return
	}
	token, err := decryptBackupToken(a.BackupKey, id, x.Ciphertext)
	if err != nil {
		fail(w, 503, "账户凭证无法解密，请确认备份密钥")
		return
	}
	remote, remoteErr := a.githubClient(token).User(r.Context())
	tx, err := a.beginAuthorized(r.Context(), u, true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var revision int64
	err = tx.QueryRow(r.Context(), `SELECT revision FROM github_accounts WHERE id=$1 FOR UPDATE`, id).Scan(&revision)
	if err != nil {
		dbError(w, err)
		return
	}
	if revision != x.Revision {
		fail(w, 409, "账户信息已变化，请刷新后重试")
		return
	}
	message := ""
	if remoteErr != nil {
		message = remoteErr.Error()
	}
	if remoteErr == nil {
		_, err = tx.Exec(r.Context(), `UPDATE github_accounts SET login=$2,verified_at=now(),last_error='',updated_at=now() WHERE id=$1`, id, remote.Login)
	} else {
		_, err = tx.Exec(r.Context(), `UPDATE github_accounts SET last_error=$2,updated_at=now() WHERE id=$1`, id, message)
	}
	if err == nil {
		err = a.audit(r.Context(), tx, u.ID, "github.account.verify", "github_account", id, "", "", map[string]any{"success": remoteErr == nil})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if remoteErr != nil {
		fail(w, 422, message)
		return
	}
	x, err = a.readAccount(r.Context(), id)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, x)
}
func backupFilterID(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	id := r.URL.Query().Get(key)
	if id != "" && !validID(id) {
		fail(w, 400, "筛选标识无效")
		return "", false
	}
	return id, true
}
func (a *App) listBackupTargets(w http.ResponseWriter, r *http.Request) {
	eid, ok := backupFilterID(w, r, "engineering_id")
	if !ok {
		return
	}
	aid, ok := backupFilterID(w, r, "account_id")
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	enabled := r.URL.Query().Get("enabled")
	if len(q) > 200 || (enabled != "" && enabled != "true" && enabled != "false") {
		fail(w, 400, "搜索词或启用状态无效")
		return
	}
	n, o := pagination(r)
	if groupBy := r.URL.Query().Get("group_by"); groupBy != "" {
		if groupBy != "engineering" {
			fail(w, 400, "备份仓库分组方式无效")
			return
		}
		a.listBackupTargetGroups(w, r, eid, aid, q, enabled, n, o)
		return
	}
	where := ` WHERE ($1::uuid IS NULL OR t.engineering_id=$1) AND ($2::uuid IS NULL OR t.account_id=$2)
 AND ($3='' OR strpos(lower(e.name || ' ' || t.owner || '/' || t.repo),lower($3))>0)
 AND ($4::boolean IS NULL OR t.enabled=$4)`
	var enabledValue any
	if enabled != "" {
		enabledValue = enabled == "true"
	}
	args := []any{nullable(eid), nullable(aid), q, enabledValue}
	filterJoin := ` FROM backup_targets t JOIN engineering e ON e.id=t.engineering_id`
	var total int
	if err := a.DB.QueryRow(r.Context(), `SELECT count(*)`+filterJoin+where, args...).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	// Resolve the page before reading per-target job summaries.
	rows, err := a.DB.Query(r.Context(), `WITH page AS MATERIALIZED (SELECT t.*`+filterJoin+where+` ORDER BY t.created_at DESC,t.id DESC LIMIT $5 OFFSET $6)
 SELECT `+targetColumns+` FROM page t`+targetRelations+` ORDER BY t.created_at DESC,t.id DESC`, append(args, n, o)...)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		x, err := scanTarget(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		dbError(w, err)
		return
	}
	listJSON(w, items, total)
}

func (a *App) listBackupTargetGroups(w http.ResponseWriter, r *http.Request, eid, aid, q, enabled string, n, o int) {
	where := ` WHERE ($1::uuid IS NULL OR t.engineering_id=$1) AND ($2::uuid IS NULL OR t.account_id=$2)
 AND ($3='' OR strpos(lower(e.name || ' ' || t.owner || '/' || t.repo),lower($3))>0)
 AND ($4::boolean IS NULL OR t.enabled=$4)`
	var enabledValue any
	if enabled != "" {
		enabledValue = enabled == "true"
	}
	args := []any{nullable(eid), nullable(aid), q, enabledValue}
	var total int
	if err := a.DB.QueryRow(r.Context(), `SELECT count(*) FROM (SELECT e.id`+targetJoin+where+` GROUP BY e.id) groups`, args...).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	rows, err := a.DB.Query(r.Context(), `SELECT e.id,e.name,count(DISTINCT t.account_id),count(DISTINCT lower(t.owner||'/'||t.repo)),count(*) FILTER (WHERE t.enabled),count(*) FILTER (WHERE NOT t.enabled)`+targetJoin+where+` GROUP BY e.id,e.name ORDER BY e.name,e.id LIMIT $5 OFFSET $6`, append(args, n, o)...)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var x BackupTargetGroup
		if err := rows.Scan(&x.EngineeringID, &x.EngineeringName, &x.AccountCount, &x.RepositoryCount, &x.EnabledCount, &x.DisabledCount); err != nil {
			dbError(w, err)
			return
		}
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		dbError(w, err)
		return
	}
	listJSON(w, items, total)
}

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

func (a *App) createBackupTarget(w http.ResponseWriter, r *http.Request) {
	if !a.requireBackupConfiguration(w) {
		return
	}
	var in struct {
		EngineeringID string `json:"engineering_id"`
		AccountID     string `json:"account_id"`
		Owner         string `json:"owner"`
		Repo          string `json:"repo"`
		AutoSync      *bool  `json:"auto_sync"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Owner = strings.ToLower(strings.TrimSpace(in.Owner))
	in.Repo = strings.ToLower(strings.TrimSpace(in.Repo))
	autoSync := true
	if in.AutoSync != nil {
		autoSync = *in.AutoSync
	}
	if !validID(in.EngineeringID) || !validID(in.AccountID) || !repositoryName.MatchString(in.Owner) || !repositoryName.MatchString(in.Repo) {
		fail(w, 400, "工程、账户或仓库名称无效")
		return
	}
	account, err := a.readAccount(r.Context(), in.AccountID)
	if err != nil {
		fail(w, 404, "GitHub 账户不存在")
		return
	}
	if err = usableAccount(account); err != nil {
		fail(w, 409, err.Error())
		return
	}
	u := userFrom(r)
	if err = a.authorizeRemote(r.Context(), u); err != nil {
		dbError(w, err)
		return
	}
	token, err := decryptBackupToken(a.BackupKey, account.ID, account.Ciphertext)
	if err != nil {
		fail(w, 503, "账户凭证无法解密，请确认备份密钥")
		return
	}
	if _, _, err = a.githubClient(token).VerifyRepository(r.Context(), in.Owner, in.Repo); err != nil {
		fail(w, 422, err.Error())
		return
	}
	tx, err := a.beginAuthorized(r.Context(), u, true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	locked, err := scanAccount(tx.QueryRow(r.Context(), `SELECT `+accountColumns+` FROM github_accounts WHERE id=$1 FOR SHARE`, account.ID))
	if err != nil || locked.Revision != account.Revision {
		fail(w, 409, "账户信息已变化，请重试")
		return
	}
	if err = usableAccount(locked); err != nil {
		fail(w, 409, err.Error())
		return
	}
	var found bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM engineering WHERE id=$1)`, in.EngineeringID).Scan(&found); err != nil || !found {
		fail(w, 404, "工程不存在")
		return
	}
	id := newID()
	_, err = tx.Exec(r.Context(), `INSERT INTO backup_targets(id,engineering_id,account_id,owner,repo,auto_sync) VALUES($1,$2,$3,$4,$5,$6)`, id, in.EngineeringID, account.ID, in.Owner, in.Repo, autoSync)
	if err != nil {
		fail(w, 409, "该工程已配置此仓库目标")
		return
	}
	err = a.audit(r.Context(), tx, u.ID, "backup.target.create", "backup_target", id, "", in.EngineeringID, map[string]any{"owner": in.Owner, "repo": in.Repo, "auto_sync": autoSync})
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	x, err := a.readTarget(r.Context(), id)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 201, x)
}
func (a *App) updateBackupTarget(w http.ResponseWriter, r *http.Request) {
	if !a.requireBackupConfiguration(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "备份目标不存在")
		return
	}
	var in struct {
		AutoSync *bool `json:"auto_sync"`
		Enabled  *bool `json:"enabled"`
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
	var engineering string
	err = tx.QueryRow(r.Context(), `UPDATE backup_targets SET auto_sync=coalesce($2,auto_sync),enabled=coalesce($3,enabled),revision=revision+1,updated_at=now() WHERE id=$1 RETURNING engineering_id`, id, in.AutoSync, in.Enabled).Scan(&engineering)
	if err == pgx.ErrNoRows {
		fail(w, 404, "备份目标不存在")
		return
	}
	if err == nil {
		err = a.audit(r.Context(), tx, userFrom(r).ID, "backup.target.update", "backup_target", id, "", engineering, in)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	x, err := a.readTarget(r.Context(), id)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, x)
}
func (a *App) listBackupJobs(w http.ResponseWriter, r *http.Request) {
	eid, ok := backupFilterID(w, r, "engineering_id")
	if !ok {
		return
	}
	tid, ok := backupFilterID(w, r, "target_id")
	if !ok {
		return
	}
	aid, ok := backupFilterID(w, r, "account_id")
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status, kind := r.URL.Query().Get("status"), r.URL.Query().Get("kind")
	groupBy, repository := r.URL.Query().Get("group_by"), r.URL.Query().Get("repository")
	if len(q) > 200 || (status != "" && status != "pending" && status != "running" && status != "ready" && status != "failed") || (kind != "" && kind != "sync" && kind != "restore") || (groupBy != "" && groupBy != "repository" && groupBy != "engineering" && groupBy != "version") || len(repository) > 201 || (repository != "" && (strings.Count(repository, "/") != 1 || strings.HasPrefix(repository, "/") || strings.HasSuffix(repository, "/"))) {
		fail(w, 400, "搜索词、任务状态或类型无效")
		return
	}
	n, o := pagination(r)
	where := ` WHERE ($1::uuid IS NULL OR t.engineering_id=$1) AND ($2::uuid IS NULL OR j.target_id=$2)
 AND ($3::uuid IS NULL OR t.account_id=$3) AND ($4='' OR strpos(lower(e.name || ' ' || t.owner || '/' || t.repo),lower($4))>0)
 AND ($5='' OR j.status=$5) AND ($6='' OR j.kind=$6)
 AND ($7='' OR lower(t.owner || '/' || t.repo)=lower($7))`
	args := []any{nullable(eid), nullable(tid), nullable(aid), q, status, kind, repository}
	if groupBy == "repository" || groupBy == "engineering" {
		a.listBackupRepositoryGroups(w, r, where, args, n, o)
		return
	}
	if groupBy == "version" {
		if eid == "" {
			fail(w, 400, "版本分组需要 engineering_id")
			return
		}
		a.listBackupVersionGroups(w, r, where, args, n, o)
		return
	}
	var total int
	if err := a.DB.QueryRow(r.Context(), `SELECT count(*)`+jobJoin+where, args...).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	rows, err := a.DB.Query(r.Context(), `SELECT `+jobColumns+jobJoin+where+` ORDER BY CASE WHEN $7<>'' THEN v.version_number END DESC,j.created_at DESC,j.id DESC LIMIT $8 OFFSET $9`, append(args, n, o)...)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		x, err := scanBackupJob(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		dbError(w, err)
		return
	}
	listJSON(w, items, total)
}

func (a *App) listBackupRepositoryGroups(w http.ResponseWriter, r *http.Request, where string, args []any, n, o int) {
	group := ` GROUP BY lower(t.owner),lower(t.repo)`
	identity := `NULL::uuid`
	order := `lower(t.owner),lower(t.repo)`
	if r.URL.Query().Get("group_by") == "engineering" {
		group = ` GROUP BY e.id`
		identity = `e.id`
		order = `e.id`
	}
	var total int
	if err := a.DB.QueryRow(r.Context(), `SELECT count(*) FROM (SELECT 1`+jobJoin+where+group+`) repositories`, args...).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	rows, err := a.DB.Query(r.Context(), `SELECT min(t.owner),min(t.repo),min(e.name),count(DISTINCT e.id),count(DISTINCT j.version_id),count(*),
 count(*) FILTER(WHERE j.status='ready'),count(*) FILTER(WHERE j.status='failed'),
 count(*) FILTER(WHERE j.status='pending'),count(*) FILTER(WHERE j.status='running'),max(j.created_at),`+identity+`,
 count(DISTINCT lower(t.owner || '/' || t.repo)),count(DISTINCT t.account_id)`+jobJoin+where+group+`
 ORDER BY max(j.created_at) DESC,`+order+` LIMIT $8 OFFSET $9`, append(args, n, o)...)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var x BackupRepositoryGroup
		var engineeringID *string
		if err = rows.Scan(&x.Owner, &x.Repo, &x.EngineeringName, &x.EngineeringCount, &x.VersionCount, &x.JobCount, &x.ReadyCount, &x.FailedCount, &x.PendingCount, &x.RunningCount, &x.LatestAt, &engineeringID, &x.RepositoryCount, &x.AccountCount); err != nil {
			dbError(w, err)
			return
		}
		if engineeringID != nil {
			x.EngineeringID = *engineeringID
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		dbError(w, err)
		return
	}
	listJSON(w, items, total)
}

func (a *App) listBackupVersionGroups(w http.ResponseWriter, r *http.Request, where string, args []any, n, o int) {
	var total int
	if err := a.DB.QueryRow(r.Context(), `SELECT count(DISTINCT j.version_id)`+jobJoin+where, args...).Scan(&total); err != nil {
		dbError(w, err)
		return
	}
	// Page versions before loading account records so a version is never split across pages.
	rows, err := a.DB.Query(r.Context(), `WITH version_page AS MATERIALIZED (SELECT j.version_id,max(v.version_number) AS number`+jobJoin+where+`
 GROUP BY j.version_id ORDER BY number DESC,j.version_id LIMIT $8 OFFSET $9)
 SELECT `+jobColumns+jobJoin+` JOIN version_page p ON p.version_id=j.version_id`+where+`
 ORDER BY v.version_number DESC,j.version_id,lower(t.owner),lower(t.repo),j.created_at DESC,j.id DESC`, append(args, n, o)...)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	groups := []*BackupVersionGroup{}
	for rows.Next() {
		job, err := scanBackupJob(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		if len(groups) == 0 || groups[len(groups)-1].VersionID != job.VersionID {
			groups = append(groups, &BackupVersionGroup{VersionID: job.VersionID, VersionNumber: job.VersionNumber, Jobs: []BackupJob{}})
		}
		last := groups[len(groups)-1]
		last.Jobs = append(last.Jobs, job)
	}
	if err = rows.Err(); err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": groups, "total": total})
}
func (a *App) lockUsableTarget(ctx context.Context, tx pgx.Tx, id string) (BackupTarget, error) {
	var accountID string
	err := tx.QueryRow(ctx, `SELECT account_id FROM backup_targets WHERE id=$1`, id).Scan(&accountID)
	if err != nil {
		return BackupTarget{}, err
	}
	account, err := scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM github_accounts WHERE id=$1 FOR SHARE`, accountID))
	if err != nil {
		return BackupTarget{}, err
	}
	if err = usableAccount(account); err != nil {
		return BackupTarget{}, err
	}
	target, err := scanTarget(tx.QueryRow(ctx, `SELECT `+targetColumns+targetJoin+` WHERE t.id=$1 FOR SHARE OF t`, id))
	if err != nil {
		return target, err
	}
	if !target.Enabled {
		return target, errors.New("备份目标已停用")
	}
	return target, nil
}
func (a *App) syncBackupTarget(w http.ResponseWriter, r *http.Request) {
	if !a.requireBackupConfiguration(w) {
		return
	}
	tid := chi.URLParam(r, "id")
	if !validID(tid) {
		fail(w, 404, "备份目标不存在")
		return
	}
	var in struct {
		VersionID string `json:"version_id"`
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
	target, err := a.lockUsableTarget(r.Context(), tx, tid)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	vid := in.VersionID
	if vid != "" && !validID(vid) {
		fail(w, 400, "工程版本标识无效")
		return
	}
	var ready string
	err = tx.QueryRow(r.Context(), `SELECT id FROM engineering_versions WHERE engineering_id=$1 AND status='ready' AND ($2='' OR id::text=$2) ORDER BY version_number DESC LIMIT 1`, target.EngineeringID, vid).Scan(&ready)
	if err != nil {
		fail(w, 409, "工程尚无可备份的 ready 版本")
		return
	}
	id := newID()
	err = tx.QueryRow(r.Context(), `INSERT INTO backup_jobs(id,target_id,version_id,kind) VALUES($1,$2,$3,'sync') ON CONFLICT(target_id,version_id) WHERE kind='sync' DO NOTHING RETURNING id`, id, tid, ready).Scan(&id)
	created := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(r.Context(), `SELECT id FROM backup_jobs WHERE target_id=$1 AND version_id=$2 AND kind='sync'`, tid, ready).Scan(&id)
	}
	if err == nil && created {
		err = a.audit(r.Context(), tx, userFrom(r).ID, "backup.sync.enqueue", "backup_job", id, "", target.EngineeringID, map[string]string{"target_id": tid, "version_id": ready})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	job, err := a.readBackupJob(r.Context(), id)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 202, job)
}
func (a *App) retryBackupJob(w http.ResponseWriter, r *http.Request) {
	if !a.requireBackupConfiguration(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "备份任务不存在")
		return
	}
	job, err := a.readBackupJob(r.Context(), id)
	if err != nil {
		fail(w, 404, "备份任务不存在")
		return
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = a.lockUsableTarget(r.Context(), tx, job.TargetID); err != nil {
		fail(w, 409, err.Error())
		return
	}
	tag, err := tx.Exec(r.Context(), `UPDATE backup_jobs SET status='pending',error='',finished_at=NULL,lease_owner='',lease_expires_at=NULL WHERE id=$1 AND status='failed'`, id)
	if err != nil {
		dbError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, 409, "只能重试失败任务")
		return
	}
	err = a.audit(r.Context(), tx, userFrom(r).ID, "backup.retry", "backup_job", id, "", job.EngineeringID, map[string]string{"kind": job.Kind})
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	job, err = a.readBackupJob(r.Context(), id)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 202, job)
}
func (a *App) restoreBackupJob(w http.ResponseWriter, r *http.Request) {
	if !a.requireBackupConfiguration(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !validID(id) {
		fail(w, 404, "备份任务不存在")
		return
	}
	source, err := a.readBackupJob(r.Context(), id)
	if err != nil {
		fail(w, 404, "备份任务不存在")
		return
	}
	tx, err := a.beginAuthorized(r.Context(), userFrom(r), true)
	if err != nil {
		dbError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = a.lockUsableTarget(r.Context(), tx, source.TargetID); err != nil {
		fail(w, 409, err.Error())
		return
	}
	var manifest, remote []byte
	var release *int64
	err = tx.QueryRow(r.Context(), `SELECT manifest,remote_assets,release_id FROM backup_jobs WHERE id=$1 AND kind='sync' AND status='ready' AND verified_at IS NOT NULL FOR SHARE`, id).Scan(&manifest, &remote, &release)
	if err != nil || len(manifest) == 0 || release == nil {
		fail(w, 409, "只可恢复已校验成功的同步快照")
		return
	}
	rid := newID()
	_, err = tx.Exec(r.Context(), `INSERT INTO backup_jobs(id,target_id,version_id,kind,source_job_id,release_id,release_url,zip_sha256,manifest,remote_assets) VALUES($1,$2,$3,'restore',$4,$5,$6,$7,$8,$9)`, rid, source.TargetID, source.VersionID, id, *release, source.ReleaseURL, source.ZipSHA256, manifest, remote)
	if err == nil {
		err = a.audit(r.Context(), tx, userFrom(r).ID, "backup.restore.enqueue", "backup_job", rid, "", source.EngineeringID, map[string]string{"source_job_id": id})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		dbError(w, err)
		return
	}
	job, err := a.readBackupJob(r.Context(), rid)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, 202, job)
}
