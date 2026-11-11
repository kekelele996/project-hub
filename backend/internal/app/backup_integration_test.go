package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type backupMockAsset struct {
	GitHubAsset
	data []byte
}

type backupMock struct {
	mu           sync.Mutex
	server       *httptest.Server
	releases     map[int64]GitHubRelease
	releaseRepos map[int64]string
	assets       map[int64][]backupMockAsset
	sequence     int64
	failUploads  int
	public       bool
	noPush       bool
	blocked      chan struct{}
	entered      chan struct{}
	flakyUsed    map[string]bool
	unauthorized bool
	managedRepos map[string]GitHubRepo
	createCalls  int
	failCreates  int
}

func newBackupMock(t *testing.T) *backupMock {
	t.Helper()
	m := &backupMock{releases: map[int64]GitHubRelease{}, releaseRepos: map[int64]string{}, assets: map[int64][]backupMockAsset{}, flakyUsed: map[string]bool{}, managedRepos: map[string]GitHubRepo{}}
	m.server = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.server.Close)
	return m
}

func (m *backupMock) serve(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.URL.Path == "/__demo/fail-next" && r.Method == "POST" {
		m.failUploads = 1
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if m.unauthorized || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || strings.Contains(r.Header.Get("Authorization"), "invalid") {
		w.WriteHeader(401)
		return
	}
	if r.URL.Path == "/user" {
		user := GitHubUser{ID: 42, Login: "mock-owner", Name: "本地备份账户"}
		if strings.HasSuffix(r.Header.Get("Authorization"), "9012") {
			user.ID, user.Login = 43, "mock-backup"
		}
		writeJSON(w, 200, user)
		return
	}
	if r.URL.Path == "/user/repos" && r.Method == "POST" {
		m.createCalls++
		if m.failCreates > 0 {
			m.failCreates--
			w.WriteHeader(403)
			return
		}
		var input struct {
			Name, Description string
			Private           bool
			AutoInit          bool `json:"auto_init"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || !input.Private || !input.AutoInit {
			w.WriteHeader(400)
			return
		}
		owner := "mock-owner"
		if strings.HasSuffix(r.Header.Get("Authorization"), "9012") {
			owner = "mock-backup"
		}
		fullName := owner + "/" + input.Name
		if _, exists := m.managedRepos[fullName]; exists {
			w.WriteHeader(422)
			return
		}
		repo := GitHubRepo{ID: 10, FullName: fullName, Private: input.Private, Description: input.Description, DefaultBranch: "main"}
		repo.Permissions.Push = true
		m.managedRepos[fullName] = repo
		writeJSON(w, 201, repo)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "repos" {
		w.WriteHeader(404)
		return
	}
	base := "/repos/" + parts[1] + "/" + parts[2]
	if len(parts) == 3 {
		if strings.HasPrefix(parts[2], "ph-") {
			if repo, ok := m.managedRepos[parts[1]+"/"+parts[2]]; ok {
				writeJSON(w, 200, repo)
			} else {
				w.WriteHeader(404)
			}
			return
		}
		repo := GitHubRepo{ID: 10, FullName: parts[1] + "/" + parts[2], Private: !m.public, DefaultBranch: "main", HTMLURL: "https://github.com/" + parts[1] + "/" + parts[2]}
		repo.Permissions.Push = !m.noPush
		writeJSON(w, 200, repo)
		return
	}
	if len(parts) == 5 && parts[3] == "commits" {
		writeJSON(w, 200, map[string]string{"sha": strings.Repeat("a", 40)})
		return
	}
	if parts[3] != "releases" {
		w.WriteHeader(404)
		return
	}
	if len(parts) == 4 && r.Method == "POST" {
		var release GitHubRelease
		if json.NewDecoder(r.Body).Decode(&release) != nil {
			w.WriteHeader(400)
			return
		}
		for id, current := range m.releases {
			if m.releaseRepos[id] == base && current.TagName == release.TagName {
				w.WriteHeader(422)
				return
			}
		}
		m.sequence++
		release.ID = m.sequence
		release.UploadURL = fmt.Sprintf("%s%s/releases/%d/assets{?name,label}", m.server.URL, base, release.ID)
		release.HTMLURL = "https://github.com/" + parts[1] + "/" + parts[2] + "/releases/tag/" + release.TagName
		m.releases[release.ID] = release
		m.releaseRepos[release.ID] = base
		writeJSON(w, 201, release)
		return
	}
	if len(parts) == 6 && parts[4] == "tags" {
		for id, release := range m.releases {
			if m.releaseRepos[id] == base && release.TagName == parts[5] {
				writeJSON(w, 200, release)
				return
			}
		}
		w.WriteHeader(404)
		return
	}
	if len(parts) == 6 && parts[4] == "assets" {
		id, _ := strconv.ParseInt(parts[5], 10, 64)
		for releaseID, assets := range m.assets {
			if m.releaseRepos[releaseID] != base {
				continue
			}
			for _, asset := range assets {
				if asset.ID == id {
					w.Header().Set("Content-Type", "application/octet-stream")
					_, _ = w.Write(asset.data)
					return
				}
			}
		}
		w.WriteHeader(404)
		return
	}
	id, _ := strconv.ParseInt(parts[4], 10, 64)
	release, found := m.releases[id]
	if !found || m.releaseRepos[id] != base {
		w.WriteHeader(404)
		return
	}
	if len(parts) == 5 {
		writeJSON(w, 200, release)
		return
	}
	if len(parts) == 6 && parts[5] == "assets" {
		if r.Method == "GET" {
			assets := []GitHubAsset{}
			for _, asset := range m.assets[id] {
				assets = append(assets, asset.GitHubAsset)
			}
			writeJSON(w, 200, assets)
			return
		}
		if r.Method == "POST" {
			if m.failUploads > 0 || (parts[2] == "flaky" && !m.flakyUsed[base]) {
				if m.failUploads > 0 {
					m.failUploads--
				}
				m.flakyUsed[base] = true
				w.WriteHeader(503)
				return
			}
			for _, current := range m.assets[id] {
				if current.Name == r.URL.Query().Get("name") {
					w.WriteHeader(422)
					return
				}
			}
			if m.blocked != nil {
				blocked, entered := m.blocked, m.entered
				m.blocked = nil
				m.mu.Unlock()
				close(entered)
				select {
				case <-blocked:
				case <-r.Context().Done():
				}
				m.mu.Lock()
			}
			data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				w.WriteHeader(400)
				return
			}
			m.sequence++
			hash := sha256.Sum256(data)
			asset := GitHubAsset{ID: m.sequence, Name: r.URL.Query().Get("name"), ContentType: r.Header.Get("Content-Type"), Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(hash[:]), URL: fmt.Sprintf("%s%s/releases/assets/%d", m.server.URL, base, m.sequence)}
			m.assets[id] = append(m.assets[id], backupMockAsset{GitHubAsset: asset, data: data})
			writeJSON(w, 201, asset)
			return
		}
	}
	w.WriteHeader(404)
}

func backupFixture(t *testing.T) (*testFixture, *backupMock, []byte, []byte) {
	t.Helper()
	f := integrationFixture(t)
	m := newBackupMock(t)
	f.a.BackupKey = bytes.Repeat([]byte{42}, 32)
	f.a.GitHubAPIBase = m.server.URL
	f.a.GitHubHTTPClient = m.server.Client()
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	file, err := zw.Create("main.go")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("package main\nfunc main() {}\n"))
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	var screenshot bytes.Buffer
	canvas := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			canvas.Set(x, y, color.RGBA{R: 95, G: 120, B: 92, A: 255})
		}
	}
	if err = png.Encode(&screenshot, canvas); err != nil {
		t.Fatal(err)
	}
	for filename, data := range map[string][]byte{"fixture.zip": archive.Bytes(), "fixture.png": screenshot.Bytes()} {
		if err = os.WriteFile(filepath.Join(f.a.UploadDir, filename), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	hash := sha256.Sum256(archive.Bytes())
	if _, err = f.a.DB.Exec(context.Background(), `UPDATE engineering_versions SET zip_sha256=$2,zip_size=$3,source_file_count=1,total_file_count=1 WHERE id=$1`, f.version, hex.EncodeToString(hash[:]), archive.Len()); err != nil {
		t.Fatal(err)
	}
	return f, m, archive.Bytes(), screenshot.Bytes()
}

func createBackupFixtureTarget(t *testing.T, f *testFixture, auto bool) (string, string) {
	t.Helper()
	account := f.request(t, f.admin, "POST", "/api/v1/admin/github-accounts", map[string]any{"name": "研发备份", "token": "ghp_local_fake_1234"})
	expectStatus(t, account, 201)
	aid := account.body["id"].(string)
	target := f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets", map[string]any{"engineering_id": f.engineering, "account_id": aid, "owner": "mock-owner", "repo": "archive", "auto_sync": auto})
	expectStatus(t, target, 201)
	return aid, target.body["id"].(string)
}

func runBackupFixtureJob(t *testing.T, f *testFixture, id, status string) BackupJob {
	t.Helper()
	if found, err := f.a.processBackupJob(context.Background()); !found || err != nil {
		t.Fatalf("worker found=%v err=%v", found, err)
	}
	job, err := f.a.readBackupJob(context.Background(), id)
	if err != nil || job.Status != status {
		t.Fatalf("job=%+v err=%v wanted=%s", job, err, status)
	}
	return job
}

func TestBackupPostgresConfigurationAccountsAndTargets(t *testing.T) {
	f, m, _, _ := backupFixture(t)
	f.a.BackupKey = nil
	list := f.request(t, f.admin, "GET", "/api/v1/admin/github-accounts", nil)
	expectStatus(t, list, 200)
	if list.body["configured"] != false {
		t.Fatal("missing key must be explicit")
	}
	for _, path := range []string{"/api/v1/admin/github-accounts", "/api/v1/admin/github-accounts/" + newID() + "/verify", "/api/v1/admin/backup-targets", "/api/v1/admin/backup-targets/" + newID() + "/sync", "/api/v1/admin/backup-jobs/" + newID() + "/restore"} {
		expectStatus(t, f.request(t, f.admin, "POST", path, map[string]any{}), 503)
	}
	f.a.BackupKey = bytes.Repeat([]byte{42}, 32)
	account, target := createBackupFixtureTarget(t, f, false)
	stored, err := f.a.readAccount(context.Background(), account)
	if err != nil || strings.Contains(stored.Ciphertext, "ghp_local_fake_1234") || stored.TokenSuffix != "1234" || stored.Login != "mock-owner" || stored.VerifiedAt == nil {
		t.Fatalf("unsafe or invalid account %+v err=%v", stored, err)
	}
	response := f.request(t, f.admin, "GET", "/api/v1/admin/github-accounts", nil)
	raw, _ := json.Marshal(response.body)
	if bytes.Contains(raw, []byte("ghp_local_fake_1234")) || bytes.Contains(raw, []byte(stored.Ciphertext)) {
		t.Fatal("account response exposed secret token or encrypted credential")
	}
	m.mu.Lock()
	m.unauthorized = true
	m.mu.Unlock()
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/github-accounts/"+account+"/verify", nil), 422)
	stored, err = f.a.readAccount(context.Background(), account)
	if err != nil || stored.LastError == "" {
		t.Fatal("failed verification was not persisted", err)
	}
	m.mu.Lock()
	m.unauthorized = false
	m.mu.Unlock()
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/github-accounts/"+account+"/verify", nil), 200)
	m.mu.Lock()
	m.public = true
	m.mu.Unlock()
	in := map[string]any{"engineering_id": f.engineering, "account_id": account, "owner": "mock-owner", "repo": "another"}
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets", in), 422)
	m.mu.Lock()
	m.public, m.noPush = false, true
	m.mu.Unlock()
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets", in), 422)
	m.mu.Lock()
	m.noPush = false
	m.mu.Unlock()
	in["repo"] = "archive"
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets", in), 409)
	in["repo"] = "second"
	defaultTarget := f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets", in)
	expectStatus(t, defaultTarget, 201)
	if defaultTarget.body["auto_sync"] != true {
		t.Fatal("omitted auto_sync should default true")
	}
	expectStatus(t, f.request(t, f.memberA, "GET", "/api/v1/admin/backup-jobs", nil), 403)
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/github-accounts/"+account, map[string]any{"active": false}), 200)
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets/"+target+"/sync", map[string]any{}), 409)
}

func TestBackupPostgresSyncRestoreAndIntegrity(t *testing.T) {
	f, m, zipBytes, pngBytes := backupFixture(t)
	_, target := createBackupFixtureTarget(t, f, false)
	claim := f.request(t, f.memberA, "POST", f.engineeringPath()+"/claim", map[string]any{})
	expectStatus(t, claim, 201)
	enqueue := f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets/"+target+"/sync", map[string]any{})
	expectStatus(t, enqueue, 202)
	id := enqueue.body["id"].(string)
	again := f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets/"+target+"/sync", map[string]any{})
	expectStatus(t, again, 202)
	if id != again.body["id"] {
		t.Fatal("same version must not enqueue duplicate sync")
	}
	job := runBackupFixtureJob(t, f, id, "ready")
	if job.VerifiedAt == nil || job.ReleaseURL == "" {
		t.Fatalf("sync missing verification: %+v", job)
	}
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-jobs/"+id+"/retry", nil), 409)
	var remoteRaw []byte
	if err := f.a.DB.QueryRow(context.Background(), `SELECT remote_assets FROM backup_jobs WHERE id=$1`, id).Scan(&remoteRaw); err != nil {
		t.Fatal(err)
	}
	var remote backupRemote
	if err := json.Unmarshal(remoteRaw, &remote); err != nil {
		t.Fatal(err)
	}
	zipPath, pngPath := filepath.Join(f.a.UploadDir, "fixture.zip"), filepath.Join(f.a.UploadDir, "fixture.png")
	if err := os.WriteFile(zipPath, []byte("damaged-local-zip"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pngPath, []byte("damaged-local-image"), 0600); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	for releaseID, assets := range m.assets {
		for index := range assets {
			if assets[index].ID == remote.Assets[0].ID {
				assets[index].data = []byte("remote-screenshot-corrupt")
			}
		}
		m.assets[releaseID] = assets
	}
	m.mu.Unlock()
	restore := f.request(t, f.admin, "POST", "/api/v1/admin/backup-jobs/"+id+"/restore", nil)
	expectStatus(t, restore, 202)
	rid := restore.body["id"].(string)
	runBackupFixtureJob(t, f, rid, "failed")
	unchanged, _ := os.ReadFile(zipPath)
	if string(unchanged) != "damaged-local-zip" {
		t.Fatal("ZIP replaced before all screenshots verified")
	}
	m.mu.Lock()
	for releaseID, assets := range m.assets {
		for index := range assets {
			if assets[index].ID == remote.Assets[0].ID {
				assets[index].data = append([]byte{}, pngBytes...)
			}
		}
		m.assets[releaseID] = assets
	}
	m.mu.Unlock()
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-jobs/"+rid+"/retry", nil), 202)
	runBackupFixtureJob(t, f, rid, "ready")
	for path, expected := range map[string][]byte{zipPath: zipBytes, pngPath: pngBytes} {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("restore mismatch %s err=%v", path, err)
		}
	}
	var claimID, versionID string
	if err := f.a.DB.QueryRow(context.Background(), `SELECT c.id,pe.version_id FROM claims c JOIN project_engineerings pe ON pe.id=c.project_engineering_id WHERE c.released_at IS NULL`).Scan(&claimID, &versionID); err != nil || claimID != claim.body["id"] || versionID != f.version {
		t.Fatalf("claim/version changed during restore: %s %s %v", claimID, versionID, err)
	}
	files, _ := filepath.Glob(filepath.Join(f.a.UploadDir, ".restore-*"))
	if len(files) != 0 {
		t.Fatalf("restore temporary files leaked: %v", files)
	}
}

func TestBackupPostgresFailureRetryAndRevocation(t *testing.T) {
	f, m, _, _ := backupFixture(t)
	account, target := createBackupFixtureTarget(t, f, false)
	m.mu.Lock()
	m.failUploads = 1
	m.mu.Unlock()
	enqueue := f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets/"+target+"/sync", map[string]any{})
	expectStatus(t, enqueue, 202)
	id := enqueue.body["id"].(string)
	runBackupFixtureJob(t, f, id, "failed")
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-jobs/"+id+"/retry", nil), 202)
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-jobs/"+id+"/retry", nil), 409)
	blocked, entered := make(chan struct{}), make(chan struct{})
	m.mu.Lock()
	m.blocked, m.entered = blocked, entered
	m.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := f.a.processBackupJob(context.Background()); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not begin")
	}
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/github-accounts/"+account, map[string]any{"active": false}), 200)
	close(blocked)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	job, err := f.a.readBackupJob(context.Background(), id)
	if err != nil || job.Status != "failed" || !strings.Contains(job.Error, "停用") {
		t.Fatalf("disabled account cannot finish ready: %+v %v", job, err)
	}
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-jobs/"+id+"/retry", nil), 409)
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/github-accounts/"+account, map[string]any{"active": true}), 200)
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-jobs/"+id+"/retry", nil), 202)
	runBackupFixtureJob(t, f, id, "ready")
	var attempts int
	if err := f.a.DB.QueryRow(context.Background(), `SELECT attempts FROM backup_jobs WHERE id=$1`, id).Scan(&attempts); err != nil || attempts != 3 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestBackupPostgresLeaseAndAutoCatchup(t *testing.T) {
	f, _, _, _ := backupFixture(t)
	account, target := createBackupFixtureTarget(t, f, true)
	ctx := context.Background()
	if err := f.a.enqueueMissingAutoBackups(ctx); err != nil {
		t.Fatal(err)
	}
	work, err := f.a.reserveBackupJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	work.Target, _ = f.a.readTarget(ctx, target)
	work.Account, _ = f.a.readAccount(ctx, account)
	if err = f.a.checkBackupWork(ctx, work); err != nil {
		t.Fatal(err)
	}
	if found, err := f.a.processBackupJob(ctx); found || err != nil {
		t.Fatalf("live lease claimed twice: found=%v err=%v", found, err)
	}
	if _, err = f.a.DB.Exec(ctx, `UPDATE backup_jobs SET lease_expires_at=now()-interval '1 minute' WHERE id=$1`, work.ID); err != nil {
		t.Fatal(err)
	}
	next, err := f.a.reserveBackupJob(ctx)
	if err != nil || next.ID != work.ID || next.LeaseOwner == work.LeaseOwner {
		t.Fatalf("expired lease not reclaimed: %+v %v", next, err)
	}
	if err = f.a.finishBackupWork(ctx, work, backupResult{}, fmt.Errorf("stale owner")); err != nil {
		t.Fatal(err)
	}
	var owner, status string
	if err = f.a.DB.QueryRow(ctx, `SELECT lease_owner,status FROM backup_jobs WHERE id=$1`, work.ID).Scan(&owner, &status); err != nil || owner != next.LeaseOwner || status != "running" {
		t.Fatalf("stale owner changed task: %s %s %v", owner, status, err)
	}
	if _, err = f.a.DB.Exec(ctx, `UPDATE backup_jobs SET lease_expires_at=now()-interval '1 minute' WHERE id=$1`, work.ID); err != nil {
		t.Fatal(err)
	}
	runBackupFixtureJob(t, f, work.ID, "ready")
	if err = f.a.enqueueMissingAutoBackups(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.a.DB.QueryRow(ctx, `SELECT count(*) FROM backup_jobs WHERE kind='sync'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("catchup duplicated jobs count=%d err=%v", count, err)
	}
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/backup-targets/"+target, map[string]any{"enabled": false}), 200)
	if _, err = f.a.DB.Exec(ctx, `INSERT INTO engineering_versions(id,engineering_id,version_number,status,zip_path,zip_sha256,zip_size) SELECT $1,engineering_id,2,'ready',zip_path,zip_sha256,zip_size FROM engineering_versions WHERE id=$2`, newID(), f.version); err != nil {
		t.Fatal(err)
	}
	if err = f.a.enqueueMissingAutoBackups(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.a.DB.QueryRow(ctx, `SELECT count(*) FROM backup_jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("disabled target enqueued count=%d err=%v", count, err)
	}
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/backup-targets/"+target, map[string]any{"enabled": true}), 200)
	if err = f.a.enqueueMissingAutoBackups(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.a.DB.QueryRow(ctx, `SELECT count(*) FROM backup_jobs`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("re-enabled target missed ready version count=%d err=%v", count, err)
	}
}

func TestBackupPostgresBearerAndPendingDisable(t *testing.T) {
	f, m, _, _ := backupFixture(t)
	ctx := context.Background()
	created := f.request(t, f.admin, "POST", "/api/v1/tokens", map[string]any{"name": "备份自动化"})
	expectStatus(t, created, 201)
	request, err := http.NewRequest("POST", f.server.URL+"/api/v1/admin/github-accounts", strings.NewReader(`{"name":"Bearer账户","token":"ghp_test_fake_5678"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+created.body["token"].(string))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 201 {
		t.Fatal("administrator bearer must work without CSRF", response.StatusCode)
	}
	account, target := createBackupFixtureTarget(t, f, false)
	enqueue := f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets/"+target+"/sync", map[string]any{})
	expectStatus(t, enqueue, 202)
	id := enqueue.body["id"].(string)
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/backup-targets/"+target, map[string]any{"enabled": false}), 200)
	runBackupFixtureJob(t, f, id, "failed")
	m.mu.Lock()
	if len(m.releases) != 0 {
		t.Fatal("disabled pending target made external writes")
	}
	m.mu.Unlock()
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/backup-targets/"+target, map[string]any{"enabled": true}), 200)
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-jobs/"+id+"/retry", nil), 202)
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/github-accounts/"+account, map[string]any{"expires_at": time.Now().Add(-time.Hour)}), 200)
	job := runBackupFixtureJob(t, f, id, "failed")
	if !strings.Contains(job.Error, "到期") {
		t.Fatal("expired account did not surface expiry", job.Error)
	}
	if _, err = f.a.DB.Exec(ctx, `UPDATE api_tokens SET revoked_at=now() WHERE id=$1`, created.body["id"].(string)); err != nil {
		t.Fatal(err)
	}
	request, _ = http.NewRequest("GET", f.server.URL+"/api/v1/admin/github-accounts", nil)
	request.Header.Set("Authorization", "Bearer "+created.body["token"].(string))
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("revoked administrator token retained backup access", response.StatusCode)
	}
}

func TestBackupPostgresMultipleAccountsAndRepositoryIsolation(t *testing.T) {
	f, mock, _, _ := backupFixture(t)
	ctx := context.Background()
	firstAccount, firstTarget := createBackupFixtureTarget(t, f, true)
	if err := f.a.enqueueMissingAutoBackups(ctx); err != nil {
		t.Fatal(err)
	}
	var firstID string
	if err := f.a.DB.QueryRow(ctx, `SELECT id FROM backup_jobs WHERE target_id=$1 AND version_id=$2`, firstTarget, f.version).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	first := runBackupFixtureJob(t, f, firstID, "ready")
	account := f.request(t, f.admin, "POST", "/api/v1/admin/github-accounts", map[string]any{"name": "第二备份账户", "token": "ghp_local_fake_9012"})
	expectStatus(t, account, 201)
	secondAccount := account.body["id"].(string)
	if firstAccount == secondAccount || account.body["login"] != "mock-backup" {
		t.Fatal("second GitHub account must have an independent identity", account.body)
	}
	target := f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets", map[string]any{"engineering_id": f.engineering, "account_id": secondAccount, "owner": "mock-owner", "repo": "flaky", "auto_sync": false})
	expectStatus(t, target, 201)
	secondTarget := target.body["id"].(string)
	enqueue := f.request(t, f.admin, "POST", "/api/v1/admin/backup-targets/"+secondTarget+"/sync", map[string]any{})
	expectStatus(t, enqueue, 202)
	secondID := enqueue.body["id"].(string)
	runBackupFixtureJob(t, f, secondID, "failed")
	if secondID == firstID {
		t.Fatal("different repository targets reused one database job")
	}
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-jobs/"+secondID+"/retry", nil), 202)
	second := runBackupFixtureJob(t, f, secondID, "ready")
	if first.VersionID != second.VersionID || first.VersionID != f.version || first.ReleaseURL == second.ReleaseURL || !strings.Contains(first.ReleaseURL, "/mock-owner/archive/releases/") || !strings.Contains(second.ReleaseURL, "/mock-owner/flaky/releases/") {
		t.Fatalf("same immutable version should have independent repository releases: first=%+v second=%+v", first, second)
	}
	var firstRelease, secondRelease int64
	var remoteRaw []byte
	if err := f.a.DB.QueryRow(ctx, `SELECT release_id,remote_assets FROM backup_jobs WHERE id=$1`, firstID).Scan(&firstRelease, &remoteRaw); err != nil {
		t.Fatal(err)
	}
	var firstRemote backupRemote
	if err := json.Unmarshal(remoteRaw, &firstRemote); err != nil {
		t.Fatal(err)
	}
	if err := f.a.DB.QueryRow(ctx, `SELECT release_id FROM backup_jobs WHERE id=$1`, secondID).Scan(&secondRelease); err != nil {
		t.Fatal(err)
	}
	if firstRelease == secondRelease {
		t.Fatal("two repositories reused one remote release ID")
	}
	mock.mu.Lock()
	count := len(mock.releases)
	firstTag, secondTag := mock.releases[firstRelease].TagName, mock.releases[secondRelease].TagName
	firstAssets, secondAssets := len(mock.assets[firstRelease]), len(mock.assets[secondRelease])
	mock.mu.Unlock()
	if count != 2 || firstTag != secondTag || firstAssets != 3 || secondAssets != 3 {
		t.Fatalf("independent releases must each contain ZIP, screenshot and manifest: releases=%d tags=%s/%s assets=%d/%d", count, firstTag, secondTag, firstAssets, secondAssets)
	}
	for _, request := range []struct{ method, path string }{
		{"GET", "/repos/another-owner/archive/releases/tags/" + firstTag},
		{"GET", "/repos/mock-owner/missing/releases/tags/" + firstTag},
		{"GET", fmt.Sprintf("/repos/mock-owner/flaky/releases/%d", firstRelease)},
		{"GET", fmt.Sprintf("/repos/mock-owner/flaky/releases/%d/assets", firstRelease)},
		{"POST", fmt.Sprintf("/repos/mock-owner/flaky/releases/%d/assets?name=foreign.zip", firstRelease)},
		{"GET", fmt.Sprintf("/repos/mock-owner/flaky/releases/assets/%d", firstRemote.ZIP.ID)},
	} {
		r, err := http.NewRequest(request.method, mock.server.URL+request.path, strings.NewReader("foreign"))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer ghp_local_fake_1234")
		res, err := mock.server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatalf("mock allowed cross-repository %s %s: status=%d", request.method, request.path, res.StatusCode)
		}
	}
}

// Opt-in local browser harness; credentials and GitHub endpoints exist only in this test process.
func TestBackupBrowserDemo(t *testing.T) {
	if os.Getenv("BACKUP_BROWSER_DEMO") != "1" {
		t.Skip("本机浏览器验收 helper 未启用")
	}
	f, m, _, _ := backupFixture(t)
	scaleDemo := os.Getenv("BACKUP_SCALE_DEMO") == "1"
	if scaleDemo {
		seedBackupScale(t, f)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if _, err := f.a.DB.Exec(ctx, `UPDATE users SET username=$2,name=$3,password_hash=$4 WHERE id=$1`, f.admin.user.ID, "admin", "备份验收管理员", passwordHash("BackupDemo123!")); err != nil {
		t.Fatal(err)
	}
	f.server.Close()
	address := os.Getenv("BACKUP_DEMO_LISTEN")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	f.server = httptest.NewUnstartedServer(f.a.Routes())
	f.server.Listener = listener
	f.server.Start()
	if !scaleDemo {
		f.a.StartAnalysisWorker(ctx)
		f.a.StartBackupWorker(ctx)
	}
	t.Logf("API=%s MockGitHub=%s UploadDir=%s admin=admin password=BackupDemo123! token=ghp_local_fake_1234 owner=mock-owner repo=archive; repo=flaky 首次上传会失败；POST %s/__demo/fail-next 使下一次上传失败", f.server.URL, m.server.URL, f.a.UploadDir, m.server.URL)
	select {
	case <-ctx.Done():
	case <-time.After(time.Hour):
	}
}
