package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
)

const backupLease = 3 * time.Minute

type backupFile struct {
	ID          string `json:"id,omitempty"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Path        string `json:"-"`
}

type backupManifest struct {
	SchemaVersion int          `json:"schema_version"`
	EngineeringID string       `json:"engineering_id"`
	VersionID     string       `json:"version_id"`
	VersionNumber int          `json:"version_number"`
	ZIP           backupFile   `json:"zip"`
	Assets        []backupFile `json:"assets"`
}

type backupRemote struct {
	Manifest GitHubAsset   `json:"manifest"`
	ZIP      GitHubAsset   `json:"zip"`
	Assets   []GitHubAsset `json:"assets"`
}

type backupWork struct {
	ID, TargetID, VersionID, Kind, LeaseOwner string
	Account                                   GitHubAccount
	Target                                    BackupTarget
	ReleaseID                                 *int64
	ReleaseURL, ZipSHA256                     string
	Manifest, Remote                          []byte
}

type backupResult struct {
	ReleaseID  int64
	ReleaseURL string
	ZIPHash    string
	Manifest   []byte
	Remote     []byte
	Staged     []stagedBackupFile
}

type stagedBackupFile struct{ temporary, destination string }

func (a *App) StartBackupWorker(ctx context.Context) {
	if !a.backupConfigured() {
		return
	}
	go func() {
		// Reconciliation must continue while another goroutine uploads large files.
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			if err := a.enqueueMissingAutoBackups(ctx); err != nil && ctx.Err() == nil {
				log.Printf("backup auto enqueue: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			for ctx.Err() == nil {
				found, err := a.processBackupJob(ctx)
				if err != nil {
					log.Printf("backup worker: %v", err)
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

func (a *App) reserveBackupJob(ctx context.Context) (backupWork, error) {
	var work backupWork
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return work, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT id,target_id,version_id,kind,release_id,release_url,zip_sha256,manifest,remote_assets FROM backup_jobs WHERE status='pending' OR (status='running' AND lease_expires_at<clock_timestamp()) ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&work.ID, &work.TargetID, &work.VersionID, &work.Kind, &work.ReleaseID, &work.ReleaseURL, &work.ZipSHA256, &work.Manifest, &work.Remote)
	if err != nil {
		return work, err
	}
	work.LeaseOwner = newID()
	_, err = tx.Exec(ctx, `UPDATE backup_jobs SET status='running',error='',lease_owner=$2,lease_expires_at=clock_timestamp()+$3::interval,attempts=attempts+1,finished_at=NULL WHERE id=$1`, work.ID, work.LeaseOwner, backupLease.String())
	if err == nil {
		err = tx.Commit(ctx)
	}
	return work, err
}

func (a *App) processBackupJob(ctx context.Context) (bool, error) {
	if !a.backupConfigured() {
		return false, nil
	}
	work, err := a.reserveBackupJob(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	work.Target, err = a.readTarget(ctx, work.TargetID)
	if err == nil {
		work.Account, err = a.readAccount(ctx, work.Target.AccountID)
	}
	jobCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				tag, renewErr := a.DB.Exec(jobCtx, `UPDATE backup_jobs SET lease_expires_at=clock_timestamp()+$3::interval WHERE id=$1 AND status='running' AND lease_owner=$2 AND lease_expires_at>clock_timestamp()`, work.ID, work.LeaseOwner, backupLease.String())
				if renewErr != nil || tag.RowsAffected() == 0 {
					cancel()
					return
				}
			}
		}
	}()
	var result backupResult
	if err == nil {
		err = a.checkBackupWork(jobCtx, work)
	}
	if err == nil {
		var token string
		token, err = decryptBackupToken(a.BackupKey, work.Account.ID, work.Account.Ciphertext)
		if err != nil {
			err = errors.New("账户凭证无法解密，请确认备份密钥")
		} else if work.Kind == "restore" {
			result, err = a.restoreBackupSnapshot(jobCtx, work, a.githubClient(token))
		} else {
			result, err = a.syncBackupSnapshot(jobCtx, work, a.githubClient(token))
		}
	}
	cancel()
	<-done
	defer func() {
		for _, file := range result.Staged {
			_ = os.Remove(file.temporary)
		}
	}()
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	return true, a.finishBackupWork(ctx, work, result, err)
}

// Account, target, then job is the common lock order for lease validation and completion.
func (a *App) validateBackupWork(ctx context.Context, tx pgx.Tx, work backupWork) error {
	account, err := scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM github_accounts WHERE id=$1 FOR SHARE`, work.Target.AccountID))
	if err != nil {
		return err
	}
	if err = usableAccount(account); err != nil {
		return err
	}
	if account.Revision != work.Account.Revision {
		return errors.New("GitHub 账户凭证或配置已变化，请重试")
	}
	var enabled bool
	var revision int64
	err = tx.QueryRow(ctx, `SELECT enabled,revision FROM backup_targets WHERE id=$1 FOR SHARE`, work.TargetID).Scan(&enabled, &revision)
	if err != nil {
		return err
	}
	if !enabled {
		return errors.New("备份目标已停用")
	}
	if revision != work.Target.Revision {
		return errors.New("备份目标配置已变化，请重试")
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT status='running' AND lease_owner=$2 AND lease_expires_at>clock_timestamp() FROM backup_jobs WHERE id=$1 FOR UPDATE`, work.ID, work.LeaseOwner).Scan(&valid)
	if err == nil && !valid {
		err = errors.New("备份任务租约已失效")
	}
	return err
}

func (a *App) checkBackupWork(ctx context.Context, work backupWork) error {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	return a.validateBackupWork(ctx, tx, work)
}

func (a *App) finishBackupWork(ctx context.Context, work backupWork, result backupResult, runErr error) error {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if checkErr := a.validateBackupWork(ctx, tx, work); checkErr != nil {
		runErr = checkErr
	}
	if runErr == nil {
		for _, file := range result.Staged {
			if err = os.Rename(file.temporary, file.destination); err != nil {
				runErr = errors.New("本地快照文件恢复失败，请重试")
				break
			}
			if dir, openErr := os.Open(filepath.Dir(file.destination)); openErr == nil {
				_ = dir.Sync()
				_ = dir.Close()
			}
		}
	}
	status, message := "ready", ""
	if runErr != nil {
		status, message = "failed", runErr.Error()
	}
	var tag int64
	if runErr == nil {
		updated, updateErr := tx.Exec(ctx, `UPDATE backup_jobs SET status='ready',error='',release_id=$3,release_url=$4,zip_sha256=$5,manifest=$6,remote_assets=$7,verified_at=now(),finished_at=now(),lease_owner='',lease_expires_at=NULL WHERE id=$1 AND status='running' AND lease_owner=$2 AND lease_expires_at>clock_timestamp()`, work.ID, work.LeaseOwner, result.ReleaseID, result.ReleaseURL, result.ZIPHash, result.Manifest, result.Remote)
		err, tag = updateErr, updated.RowsAffected()
	} else {
		updated, updateErr := tx.Exec(ctx, `UPDATE backup_jobs SET status='failed',error=$3,finished_at=now(),lease_owner='',lease_expires_at=NULL WHERE id=$1 AND status='running' AND lease_owner=$2 AND lease_expires_at>clock_timestamp()`, work.ID, work.LeaseOwner, message)
		err, tag = updateErr, updated.RowsAffected()
	}
	if err == nil && tag > 0 {
		err = a.audit(ctx, tx, "", "backup."+work.Kind+"."+status, "backup_job", work.ID, "", work.Target.EngineeringID, map[string]any{"target_id": work.TargetID, "version_id": work.VersionID, "error": message})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return err
}

func (a *App) snapshotManifest(ctx context.Context, id string, hashFiles bool) (backupManifest, error) {
	manifest := backupManifest{SchemaVersion: 1, Assets: []backupFile{}}
	var status, stored string
	err := a.DB.QueryRow(ctx, `SELECT engineering_id,version_number,status,zip_path,zip_sha256,zip_size FROM engineering_versions WHERE id=$1`, id).Scan(&manifest.EngineeringID, &manifest.VersionNumber, &status, &stored, &manifest.ZIP.SHA256, &manifest.ZIP.Size)
	manifest.VersionID = id
	if err != nil {
		return manifest, err
	}
	if status != "ready" || !validSHA256(manifest.ZIP.SHA256) {
		return manifest, errors.New("工程版本未 ready 或缺少 ZIP 哈希")
	}
	manifest.ZIP.Kind, manifest.ZIP.Name, manifest.ZIP.ContentType = "zip", "code.zip", "application/zip"
	manifest.ZIP.Path, err = a.storedPath(stored)
	if err != nil {
		return manifest, err
	}
	rows, err := a.DB.Query(ctx, `SELECT id,kind,path,mime_type FROM engineering_assets WHERE version_id=$1 ORDER BY id`, id)
	if err != nil {
		return manifest, err
	}
	defer rows.Close()
	for rows.Next() {
		var file backupFile
		if err = rows.Scan(&file.ID, &file.Kind, &stored, &file.ContentType); err != nil {
			return manifest, err
		}
		file.Path, err = a.storedPath(stored)
		if err != nil {
			return manifest, err
		}
		extension := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[file.ContentType]
		if extension == "" {
			return manifest, errors.New("截图媒体类型无效")
		}
		file.Name = "screenshot-" + file.ID + extension
		manifest.Assets = append(manifest.Assets, file)
	}
	if err = rows.Err(); err != nil {
		return manifest, err
	}
	if hashFiles {
		hash, size, err := hashBackupFile(manifest.ZIP.Path)
		if err != nil || hash != manifest.ZIP.SHA256 || size != manifest.ZIP.Size {
			return manifest, errors.New("本地 ZIP 缺失或与工程版本 SHA-256 不一致")
		}
		for index := range manifest.Assets {
			file := &manifest.Assets[index]
			file.SHA256, file.Size, err = hashBackupFile(file.Path)
			if err != nil {
				return manifest, errors.New("本地截图缺失或无法读取")
			}
		}
	}
	return manifest, nil
}

func hashBackupFile(filename string) (string, int64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	return hex.EncodeToString(hash.Sum(nil)), size, err
}

func (a *App) syncBackupSnapshot(ctx context.Context, work backupWork, client *GitHubClient) (backupResult, error) {
	var result backupResult
	manifest, err := a.snapshotManifest(ctx, work.VersionID, true)
	if err != nil {
		return result, err
	}
	if err = a.checkBackupWork(ctx, work); err != nil {
		return result, err
	}
	var commit string
	if work.Target.Managed {
		commit, err = client.EnsureManagedRepository(ctx, work.Target)
	} else {
		_, commit, err = client.VerifyRepository(ctx, work.Target.Owner, work.Target.Repo)
	}
	if err != nil {
		return result, err
	}
	if err = a.checkBackupWork(ctx, work); err != nil {
		return result, err
	}
	tag := fmt.Sprintf("ph-%s-v%d-%s", manifest.EngineeringID, manifest.VersionNumber, manifest.VersionID)
	release, err := client.EnsureRelease(ctx, work.Target.Owner, work.Target.Repo, tag, fmt.Sprintf("工程快照 v%d", manifest.VersionNumber), commit, manifest.EngineeringID, manifest.VersionID)
	if err != nil {
		return result, err
	}
	remote := backupRemote{Assets: []GitHubAsset{}}
	files := append([]backupFile{manifest.ZIP}, manifest.Assets...)
	for index, file := range files {
		if err = a.checkBackupWork(ctx, work); err != nil {
			return result, err
		}
		source, openErr := os.Open(file.Path)
		if openErr != nil {
			return result, errors.New("本地备份文件无法读取")
		}
		asset, uploadErr := client.EnsureAsset(ctx, work.Target.Owner, work.Target.Repo, release, file.Name, file.ContentType, source, file.Size, file.SHA256)
		_ = source.Close()
		if uploadErr != nil {
			return result, uploadErr
		}
		if index == 0 {
			remote.ZIP = asset
		} else {
			remote.Assets = append(remote.Assets, asset)
		}
	}
	result.Manifest, err = json.Marshal(manifest)
	if err != nil {
		return result, err
	}
	if err = a.checkBackupWork(ctx, work); err != nil {
		return result, err
	}
	hash := sha256.Sum256(result.Manifest)
	remote.Manifest, err = client.EnsureAsset(ctx, work.Target.Owner, work.Target.Repo, release, "manifest.json", "application/json", bytes.NewReader(result.Manifest), int64(len(result.Manifest)), hex.EncodeToString(hash[:]))
	if err != nil {
		return result, err
	}
	result.ReleaseID, result.ReleaseURL, result.ZIPHash = release.ID, release.HTMLURL, manifest.ZIP.SHA256
	result.Remote, err = json.Marshal(remote)
	return result, err
}

func (a *App) restoreBackupSnapshot(ctx context.Context, work backupWork, client *GitHubClient) (backupResult, error) {
	result := backupResult{ReleaseURL: work.ReleaseURL, ZIPHash: work.ZipSHA256, Manifest: work.Manifest, Remote: work.Remote}
	if work.ReleaseID == nil {
		return result, errors.New("远端快照缺少 Release 信息")
	}
	result.ReleaseID = *work.ReleaseID
	var manifest backupManifest
	var remote backupRemote
	if json.Unmarshal(work.Manifest, &manifest) != nil || json.Unmarshal(work.Remote, &remote) != nil || manifest.SchemaVersion != 1 || remote.Manifest.ID == 0 {
		return result, errors.New("数据库快照 manifest 无效")
	}
	var raw bytes.Buffer
	if _, err := client.DownloadAsset(ctx, work.Target.Owner, work.Target.Repo, remote.Manifest.ID, &limitedBackupWriter{Writer: &raw, Remaining: 1 << 20}); err != nil {
		return result, err
	}
	var downloaded backupManifest
	if json.Unmarshal(raw.Bytes(), &downloaded) != nil || !reflect.DeepEqual(downloaded, manifest) {
		return result, errors.New("远端 manifest 与已校验快照不一致")
	}
	local, err := a.snapshotManifest(ctx, work.VersionID, false)
	if err != nil {
		return result, err
	}
	if manifest.EngineeringID != local.EngineeringID || manifest.VersionID != local.VersionID || manifest.VersionNumber != local.VersionNumber || manifest.ZIP.SHA256 != local.ZIP.SHA256 || manifest.ZIP.Size != local.ZIP.Size || manifest.ZIP.SHA256 != work.ZipSHA256 || len(manifest.Assets) != len(local.Assets) || len(remote.Assets) != len(local.Assets) {
		return result, errors.New("快照与原工程版本元数据不一致")
	}
	files := append([]backupFile{manifest.ZIP}, manifest.Assets...)
	paths := append([]backupFile{local.ZIP}, local.Assets...)
	assets := append([]GitHubAsset{remote.ZIP}, remote.Assets...)
	for index, file := range files {
		if file.ID != paths[index].ID || file.Kind != paths[index].Kind || file.Name != paths[index].Name || file.ContentType != paths[index].ContentType || assets[index].Name != file.Name || assets[index].Size != file.Size || !validSHA256(file.SHA256) || file.Size < 0 {
			return result, errors.New("快照文件列表与原工程不一致")
		}
		staged, err := stageBackupDownload(ctx, client, work.Target, assets[index], file, paths[index].Path)
		if err != nil {
			return result, err
		}
		result.Staged = append(result.Staged, staged)
	}
	return result, nil
}

type limitedBackupWriter struct {
	io.Writer
	Remaining int64
}

func (writer *limitedBackupWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > writer.Remaining {
		return 0, errors.New("远端备份文件超过快照记录大小")
	}
	n, err := writer.Writer.Write(data)
	writer.Remaining -= int64(n)
	return n, err
}

func stageBackupDownload(ctx context.Context, client *GitHubClient, target BackupTarget, asset GitHubAsset, expected backupFile, destination string) (stagedBackupFile, error) {
	var staged stagedBackupFile
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return staged, err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".restore-*")
	if err != nil {
		return staged, err
	}
	staged = stagedBackupFile{temporary: file.Name(), destination: destination}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(file.Name())
		}
	}()
	hash := sha256.New()
	size, err := client.DownloadAsset(ctx, target.Owner, target.Repo, asset.ID, &limitedBackupWriter{Writer: io.MultiWriter(file, hash), Remaining: expected.Size})
	if err != nil {
		return staged, err
	}
	if size != expected.Size || hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
		return staged, errors.New("远端 ZIP 或截图 SHA-256 校验失败")
	}
	if err = file.Sync(); err != nil {
		return staged, err
	}
	if err = file.Close(); err != nil {
		return staged, err
	}
	success = true
	return staged, nil
}

func (a *App) enqueueAutoBackups(ctx context.Context, versionID string) error {
	if !a.backupConfigured() {
		return nil
	}
	rows, err := a.DB.Query(ctx, `SELECT t.id FROM backup_targets t JOIN engineering_versions v ON v.engineering_id=t.engineering_id WHERE v.id=$1 AND v.status='ready' AND t.auto_sync AND t.enabled`, versionID)
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
		tx, err := a.DB.Begin(ctx)
		if err != nil {
			return err
		}
		target, checkErr := a.lockUsableTarget(ctx, tx, id)
		if checkErr == nil && target.AutoSync {
			jobID := newID()
			var tag int64
			updated, insertErr := tx.Exec(ctx, `INSERT INTO backup_jobs(id,target_id,version_id,kind) VALUES($1,$2,$3,'sync') ON CONFLICT(target_id,version_id) WHERE kind='sync' DO NOTHING`, jobID, id, versionID)
			err, tag = insertErr, updated.RowsAffected()
			if err == nil && tag > 0 {
				err = a.audit(ctx, tx, "", "backup.sync.auto", "backup_job", jobID, "", target.EngineeringID, map[string]string{"version_id": versionID})
			}
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		_ = tx.Rollback(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *App) enqueueMissingAutoBackups(ctx context.Context) error {
	if !a.backupConfigured() {
		return nil
	}
	if err := a.provisionGlobalAutoBackups(ctx); err != nil {
		return err
	}
	rows, err := a.DB.Query(ctx, `SELECT DISTINCT v.id FROM engineering_versions v JOIN backup_targets t ON t.engineering_id=v.engineering_id JOIN github_accounts a ON a.id=t.account_id WHERE v.status='ready' AND t.auto_sync AND t.enabled AND a.active AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND NOT EXISTS(SELECT 1 FROM backup_jobs j WHERE j.target_id=t.id AND j.version_id=v.id AND j.kind='sync') LIMIT 100`)
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
		if err = a.enqueueAutoBackups(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
