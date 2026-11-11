package app

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestManagedBackupNamingAndRepositoryOwnership(t *testing.T) {
	id := newID()
	for _, slug := range []string{"ab", "ABC", "elderly--care", "-care", "care-", "社区", strings.Repeat("a", 49), "care/repo"} {
		if _, err := managedBackupRepoName(slug, id); err == nil {
			t.Fatalf("accepted invalid slug %q", slug)
		}
	}
	repoName, err := managedBackupRepoName("elderly-care", id)
	if err != nil || repoName != "ph-elderly-care-"+strings.ReplaceAll(id, "-", "") {
		t.Fatal(repoName, err)
	}
	other, _ := managedBackupRepoName("elderly-care", newID())
	if other == repoName {
		t.Fatal("engineering names must not collide")
	}

	for _, scenario := range []string{"create", "reuse", "unrelated", "public", "wrong-account", "denied"} {
		t.Run(scenario, func(t *testing.T) {
			m := newBackupMock(t)
			target := BackupTarget{Managed: true, EngineeringID: id, Owner: "mock-owner", Repo: repoName}
			repo := GitHubRepo{ID: 10, FullName: target.Owner + "/" + target.Repo, Private: true, Description: managedBackupDescription(id), DefaultBranch: "main"}
			repo.Permissions.Push = true
			switch scenario {
			case "reuse":
				m.managedRepos[repo.FullName] = repo
			case "unrelated":
				repo.Description = "unrelated"
				m.managedRepos[repo.FullName] = repo
			case "public":
				repo.Private = false
				m.managedRepos[repo.FullName] = repo
			case "wrong-account":
				target.Owner = "another-owner"
			case "denied":
				m.failCreates = 1
			}
			client := &GitHubClient{BaseURL: m.server.URL, HTTPClient: m.server.Client(), Token: "ghp_local_fake_1234"}
			sha, err := client.EnsureManagedRepository(context.Background(), target)
			ok := scenario == "create" || scenario == "reuse"
			if (err == nil) != ok || (ok && sha != strings.Repeat("a", 40)) {
				t.Fatalf("sha=%s err=%v", sha, err)
			}
			if scenario == "create" {
				if _, err = client.EnsureManagedRepository(context.Background(), target); err != nil || m.createCalls != 1 {
					t.Fatalf("retry created another repository: %v %d", err, m.createCalls)
				}
			} else if scenario != "denied" && m.createCalls != 0 {
				t.Fatal("unexpected create")
			}
		})
	}
}

func managedUpload(t *testing.T, f *testFixture, fields map[string]string, zipData, screenshot []byte, identity testIdentity) testResult {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	values := map[string]string{"name": "自动灾备测试", "github_url": "https://github.com/example/source", "github_ref": "v1.0.0"}
	for k, v := range fields {
		values[k] = v
	}
	for k, v := range values {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, part := range []struct {
		name, filename string
		data           []byte
	}{{"zip", "source.zip", zipData}, {"startup_screenshot", "startup.png", screenshot}, {"feature_screenshots", "feature.png", screenshot}} {
		w, err := writer.CreateFormFile(part.name, part.filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(part.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest("POST", f.server.URL+"/api/v1/admin/engineering-versions", &body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("X-CSRF-Token", identity.csrf)
	r.AddCookie(&http.Cookie{Name: "projecthub_session", Value: identity.cookie})
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return testResult{status: res.StatusCode, body: out}
}

func TestManagedBackupPostgresUploadVersionRetryAndRestore(t *testing.T) {
	f, m, zipData, screenshot := backupFixture(t)
	ctx := context.Background()
	account := f.request(t, f.admin, "POST", "/api/v1/admin/github-accounts", map[string]string{"name": "auto", "token": "ghp_local_fake_1234"})
	expectStatus(t, account, 201)
	fields := map[string]string{"backup_account_id": account.body["id"].(string), "backup_repo_name": "elderly-care"}
	expectStatus(t, managedUpload(t, f, fields, zipData, screenshot, f.memberA), 403)
	bad := map[string]string{"backup_account_id": fields["backup_account_id"], "backup_repo_name": "bad--name"}
	expectStatus(t, managedUpload(t, f, bad, zipData, screenshot, f.admin), 400)
	upload := managedUpload(t, f, fields, zipData, screenshot, f.admin)
	expectStatus(t, upload, 201)
	engineering := upload.body["engineering_id"].(string)
	targetID := upload.body["backup_target_id"].(string)
	if m.createCalls != 0 {
		t.Fatal("upload must defer GitHub requests")
	}
	var versions []string
	versions = append(versions, upload.body["version_id"].(string))
	// Exercise real archive analysis and the ready transition that enqueues backups.
	if found, err := f.a.processAnalysisJob(ctx); !found || err != nil {
		t.Fatal(found, err)
	}
	var jobID string
	if err := f.a.DB.QueryRow(ctx, `SELECT id FROM backup_jobs WHERE version_id=$1`, versions[0]).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	m.failCreates = 1
	failed := runBackupFixtureJob(t, f, jobID, "failed")
	if !strings.Contains(failed.Error, "自动创建") {
		t.Fatal(failed.Error)
	}
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/backup-jobs/"+jobID+"/retry", nil), 202)
	ready := runBackupFixtureJob(t, f, jobID, "ready")
	if m.createCalls != 2 || len(m.managedRepos) != 1 {
		t.Fatal("creation retry", m.createCalls)
	}
	second := managedUpload(t, f, map[string]string{"engineering_id": engineering}, zipData, screenshot, f.admin)
	expectStatus(t, second, 201)
	versions = append(versions, second.body["version_id"].(string))
	if found, err := f.a.processAnalysisJob(ctx); !found || err != nil {
		t.Fatal(found, err)
	}
	if err := f.a.DB.QueryRow(ctx, `SELECT id FROM backup_jobs WHERE version_id=$1`, versions[1]).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	runBackupFixtureJob(t, f, jobID, "ready")
	if m.createCalls != 2 || len(m.releases) != 2 {
		t.Fatal("new version must reuse repository and retain releases")
	}
	fields["engineering_id"] = engineering
	repeat := managedUpload(t, f, fields, zipData, screenshot, f.admin)
	expectStatus(t, repeat, 201)
	if repeat.body["backup_target_id"] != targetID {
		t.Fatal("duplicate target")
	}
	fields["backup_repo_name"] = "another-name"
	expectStatus(t, managedUpload(t, f, fields, zipData, screenshot, f.admin), 409)
	var count int
	if err := f.a.DB.QueryRow(ctx, `SELECT count(*) FROM engineering_versions WHERE engineering_id=$1`, engineering).Scan(&count); err != nil || count != 3 {
		t.Fatal("invalid upload should roll back", count, err)
	}
	restore := f.request(t, f.admin, "POST", "/api/v1/admin/backup-jobs/"+ready.ID+"/restore", nil)
	expectStatus(t, restore, 202)
	runBackupFixtureJob(t, f, restore.body["id"].(string), "ready")
}

func TestGlobalBackupPostgresConfiguredAccountsAndBackfill(t *testing.T) {
	f, m, zipData, screenshot := backupFixture(t)
	ctx := context.Background()
	account := f.request(t, f.admin, "POST", "/api/v1/admin/github-accounts", map[string]string{"name": "first", "token": "ghp_local_fake_1234"})
	expectStatus(t, account, 201)
	upload := managedUpload(t, f, map[string]string{"backup_repo_name": "global-care"}, zipData, screenshot, f.admin)
	expectStatus(t, upload, 201)
	engineering := upload.body["engineering_id"].(string)
	if len(upload.body["backup_targets"].([]any)) != 1 || upload.body["backup_status"] != "waiting_analysis" {
		t.Fatal(upload.body)
	}
	if found, err := f.a.processAnalysisJob(ctx); !found || err != nil {
		t.Fatal(found, err)
	}
	var jobID string
	if err := f.a.DB.QueryRow(ctx, `SELECT id FROM backup_jobs WHERE version_id=$1`, upload.body["version_id"]).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	runBackupFixtureJob(t, f, jobID, "ready")
	second := f.request(t, f.admin, "POST", "/api/v1/admin/github-accounts", map[string]string{"name": "second", "token": "ghp_local_fake_9012"})
	expectStatus(t, second, 201)
	// Adding a global account enrolls existing engineering, without another upload.
	if err := f.a.enqueueMissingAutoBackups(ctx); err != nil {
		t.Fatal(err)
	}
	var targets, jobs int
	if err := f.a.DB.QueryRow(ctx, `SELECT count(*) FROM backup_targets WHERE engineering_id=$1`, engineering).Scan(&targets); err != nil || targets != 2 {
		t.Fatal(targets, err)
	}
	if err := f.a.DB.QueryRow(ctx, `SELECT count(*) FROM backup_jobs WHERE version_id=$1`, upload.body["version_id"]).Scan(&jobs); err != nil || jobs != 2 {
		t.Fatal(jobs, err)
	}
	for {
		found, err := f.a.processBackupJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
	}
	// Both the existing fixture and new engineering now have a copy per account.
	if len(m.managedRepos) != 4 || len(m.releases) != 4 {
		t.Fatal("global backfill", len(m.managedRepos), len(m.releases))
	}
	if err := f.a.enqueueMissingAutoBackups(ctx); err != nil {
		t.Fatal(err)
	}
	if found, err := f.a.processBackupJob(ctx); found || err != nil {
		t.Fatal("backfill must be idempotent", found, err)
	}
	version2 := managedUpload(t, f, map[string]string{"engineering_id": engineering}, zipData, screenshot, f.admin)
	expectStatus(t, version2, 201)
	if len(version2.body["backup_targets"].([]any)) != 2 {
		t.Fatal(version2.body)
	}
	if found, err := f.a.processAnalysisJob(ctx); !found || err != nil {
		t.Fatal(found, err)
	}
	for {
		found, err := f.a.processBackupJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
	}
	if len(m.managedRepos) != 4 || len(m.releases) != 6 {
		t.Fatal("new version must reuse both repositories", len(m.managedRepos), len(m.releases))
	}
	// Local account/target disablement is honored by global reconciliation.
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/github-accounts/"+second.body["id"].(string), map[string]bool{"active": false}), 200)
	var firstTarget string
	if err := f.a.DB.QueryRow(ctx, `SELECT id FROM backup_targets WHERE engineering_id=$1 AND account_id=$2`, engineering, account.body["id"]).Scan(&firstTarget); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/backup-targets/"+firstTarget, map[string]bool{"enabled": false}), 200)
	if err := f.a.enqueueMissingAutoBackups(ctx); err != nil {
		t.Fatal(err)
	}
	var enabled bool
	if err := f.a.DB.QueryRow(ctx, `SELECT enabled FROM backup_targets WHERE id=$1`, firstTarget).Scan(&enabled); err != nil || enabled {
		t.Fatal("disabled target was re-enabled", enabled, err)
	}
	newUpload := managedUpload(t, f, map[string]string{}, zipData, screenshot, f.admin)
	expectStatus(t, newUpload, 201)
	if len(newUpload.body["backup_targets"].([]any)) != 1 {
		t.Fatal("disabled account included", newUpload.body)
	}

	// Without an encryption key, the API explicitly reports that no backup was scheduled.
	f.a.BackupKey = nil
	unconfigured := managedUpload(t, f, map[string]string{}, zipData, screenshot, f.admin)
	expectStatus(t, unconfigured, 201)
	if unconfigured.body["backup_status"] != "unconfigured" || len(unconfigured.body["backup_targets"].([]any)) != 0 {
		t.Fatal(unconfigured.body)
	}
}
