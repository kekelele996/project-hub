package app

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Global accounts are the policy. Per-engineering targets remain the durable
// repository mapping, and disabled mappings must not be silently re-enabled.
func (a *App) ensureGlobalBackupTargets(ctx context.Context, tx pgx.Tx, engineeringID, slug, actor string) error {
	rows, err := tx.Query(ctx, `SELECT `+accountColumns+` FROM github_accounts WHERE active AND verified_at IS NOT NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY id FOR SHARE`)
	if err != nil {
		return err
	}
	accounts := []GitHubAccount{}
	for rows.Next() {
		account, scanErr := scanAccount(rows)
		if scanErr != nil {
			rows.Close()
			return scanErr
		}
		accounts = append(accounts, account)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	repo, err := managedBackupRepoName(slug, engineeringID)
	if err != nil {
		return err
	}
	for _, account := range accounts {
		id := newID()
		result, insertErr := tx.Exec(ctx, `INSERT INTO backup_targets(id,engineering_id,account_id,owner,repo,auto_sync,managed)
 SELECT $1,$2,$3,$4,$5,true,true WHERE NOT EXISTS(SELECT 1 FROM backup_targets WHERE engineering_id=$2 AND (account_id=$3 OR lower(owner)=lower($4)))
 ON CONFLICT DO NOTHING`, id, engineeringID, account.ID, account.Login, repo)
		if insertErr != nil {
			return insertErr
		}
		if result.RowsAffected() > 0 {
			if err = a.audit(ctx, tx, actor, "backup.target.auto_create", "backup_target", id, "", engineeringID, map[string]string{"owner": account.Login, "repo": repo, "policy": "global_accounts"}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) provisionGlobalAutoBackups(ctx context.Context) error {
	rows, err := a.DB.Query(ctx, `SELECT e.id FROM engineering e WHERE NOT e.archived AND EXISTS(SELECT 1 FROM engineering_versions v WHERE v.engineering_id=e.id AND v.status='ready') AND EXISTS(
 SELECT 1 FROM github_accounts a WHERE a.active AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM backup_targets t WHERE t.engineering_id=e.id AND (t.account_id=a.id OR lower(t.owner)=lower(a.login)))) ORDER BY e.created_at,e.id LIMIT 100`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		tx, beginErr := a.DB.Begin(ctx)
		if beginErr != nil {
			return beginErr
		}
		var slug string
		err = tx.QueryRow(ctx, `SELECT backup_repo_name FROM engineering WHERE id=$1 AND NOT archived FOR UPDATE`, id).Scan(&slug)
		if err == nil {
			err = a.ensureGlobalBackupTargets(ctx, tx, id, slug, "")
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		_ = tx.Rollback(ctx)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
	}
	return nil
}
