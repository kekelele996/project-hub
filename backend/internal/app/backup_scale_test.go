package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"
)

func seedBackupScale(t *testing.T, f *testFixture) []string {
	t.Helper()
	accounts := []string{}
	for index, token := range []string{"ghp_local_fake_1234", "ghp_local_fake_9012"} {
		result := f.request(t, f.admin, "POST", "/api/v1/admin/github-accounts", map[string]string{"name": fmt.Sprintf("规模账号%d", index+1), "token": token})
		expectStatus(t, result, 201)
		accounts = append(accounts, result.body["id"].(string))
	}
	ctx := context.Background()
	statements := []string{
		`INSERT INTO engineering(id,name) SELECT md5('scale-e-'||n)::uuid,'规模验收工程-'||lpad(n::text,5,'0') FROM generate_series(1,1000) n`,
		`INSERT INTO engineering_versions(id,engineering_id,version_number,status,zip_path)
 SELECT md5('scale-v-'||e||'-'||v)::uuid,md5('scale-e-'||e)::uuid,v,'ready','fixture.zip' FROM generate_series(1,1000) e CROSS JOIN generate_series(1,10) v`,
		`INSERT INTO backup_targets(id,engineering_id,account_id,owner,repo,enabled,managed)
 SELECT md5('scale-t-'||e||'-'||a.id)::uuid,md5('scale-e-'||e)::uuid,a.id,a.login,'ph-scale-'||lpad(e::text,5,'0'),e%10<>0,true
 FROM generate_series(1,1000) e CROSS JOIN github_accounts a`,
		`INSERT INTO backup_jobs(id,target_id,version_id,kind,status,verified_at,zip_sha256,release_url)
 SELECT md5('scale-j-'||t.id||'-'||v.version_number)::uuid,t.id,v.id,CASE WHEN v.version_number>8 THEN 'restore' ELSE 'sync' END,
 (ARRAY['ready','failed','pending','running'])[v.version_number%4+1],CASE WHEN v.version_number%4=0 THEN now() ELSE NULL END,
 repeat('a',64),'https://github.com/'||t.owner||'/'||t.repo||'/releases/tag/scale-'||v.version_number
 FROM backup_targets t JOIN engineering_versions v ON v.engineering_id=t.engineering_id`,
		`ANALYZE backup_jobs`, `ANALYZE backup_targets`, `ANALYZE engineering`, `ANALYZE engineering_versions`,
	}
	// Equal timestamps exercise the ID tie-breaker across every page.
	tx, err := f.a.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, sql := range statements {
		if _, err = tx.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return accounts
}

func TestBackupPostgresScalePaginationAndFilters(t *testing.T) {
	f, _, _, _ := backupFixture(t)
	accounts := seedBackupScale(t, f)
	targetPath := "/api/v1/admin/backup-targets?q=规模验收&page_size=20"
	jobPath := "/api/v1/admin/backup-jobs?q=规模验收&page_size=20"
	seen := map[string]bool{}
	for page := 1; page <= 100; page++ {
		result := f.request(t, f.admin, "GET", fmt.Sprintf("%s&page=%d", targetPath, page), nil)
		expectStatus(t, result, 200)
		items := result.body["items"].([]any)
		if result.body["total"] != float64(2000) || len(items) != 20 {
			t.Fatal(result.body["total"], len(items))
		}
		for _, item := range items {
			id := item.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatal("pagination duplicated target", id)
			}
			seen[id] = true
		}
	}
	for _, check := range []struct {
		path  string
		total int
	}{
		{targetPath + "&enabled=false", 200}, {targetPath + "&enabled=true", 1800},
		{targetPath + "&account_id=" + accounts[0], 1000},
		{"/api/v1/admin/backup-targets?q=规模验收工程-00042", 2},
		{jobPath, 20000}, {jobPath + "&status=failed", 6000}, {jobPath + "&kind=restore", 4000},
		{jobPath + "&account_id=" + accounts[1], 10000},
		{"/api/v1/admin/backup-jobs?q=规模验收工程-00042&status=ready&kind=sync", 4},
	} {
		result := f.request(t, f.admin, "GET", check.path, nil)
		expectStatus(t, result, 200)
		if result.body["total"] != float64(check.total) || len(result.body["items"].([]any)) > 20 {
			t.Fatal(check.path, result.body)
		}
	}
	for _, path := range []string{targetPath + "&account_id=invalid", targetPath + "&enabled=invalid", jobPath + "&status=invalid", jobPath + "&kind=invalid"} {
		expectStatus(t, f.request(t, f.admin, "GET", path, nil), 400)
	}
	seen = map[string]bool{}
	var samples []time.Duration
	for _, page := range []int{1, 2, 3, 499, 500, 998, 999, 1000} {
		start := time.Now()
		result := f.request(t, f.admin, "GET", fmt.Sprintf("%s&page=%d", jobPath, page), nil)
		samples = append(samples, time.Since(start))
		expectStatus(t, result, 200)
		items := result.body["items"].([]any)
		if len(items) != 20 {
			t.Fatal("deep page", page, len(items))
		}
		for _, item := range items {
			id := item.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatal("pagination duplicated job", id)
			}
			seen[id] = true
		}
		raw, _ := json.Marshal(result.body)
		if len(raw) > 32<<10 {
			t.Fatal("page payload unexpectedly large", len(raw))
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	t.Logf("1000 engineering, 2000 targets, 20000 jobs; 20 rows/page; first/deep page median=%s max=%s", samples[len(samples)/2], samples[len(samples)-1])
	var plan []byte
	if err := f.a.DB.QueryRow(context.Background(), `EXPLAIN (ANALYZE,FORMAT JSON) SELECT status,error FROM backup_jobs WHERE target_id=(SELECT id FROM backup_targets LIMIT 1) ORDER BY created_at DESC,id DESC LIMIT 1`).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	t.Logf("latest-job query plan: %s", plan)
	groups := f.request(t, f.admin, "GET", jobPath+"&group_by=repository", nil)
	expectStatus(t, groups, 200)
	if groups.body["total"] != float64(2000) || len(groups.body["items"].([]any)) != 20 {
		t.Fatal("repository pagination must count repositories", groups.body)
	}
	for _, item := range groups.body["items"].([]any) {
		x := item.(map[string]any)
		if x["version_count"] != float64(10) || x["job_count"] != float64(10) || x["ready_count"] != float64(2) || x["failed_count"] != float64(3) || x["pending_count"] != float64(3) || x["running_count"] != float64(2) {
			t.Fatal("incorrect repository summary", x)
		}
	}
	engineeringGroups := f.request(t, f.admin, "GET", "/api/v1/admin/backup-jobs?group_by=engineering&q=00042", nil)
	expectStatus(t, engineeringGroups, 200)
	if engineeringGroups.body["total"] != float64(1) {
		t.Fatal("accounts must merge into one engineering group", engineeringGroups.body)
	}
	engineeringSummary := engineeringGroups.body["items"].([]any)[0].(map[string]any)
	if engineeringSummary["version_count"] != float64(10) || engineeringSummary["job_count"] != float64(20) || engineeringSummary["account_count"] != float64(2) || engineeringSummary["repository_count"] != float64(2) {
		t.Fatal("account backups inflated engineering version count", engineeringSummary)
	}
	versionPath := "/api/v1/admin/backup-jobs?group_by=version&engineering_id=" + engineeringSummary["engineering_id"].(string) + "&page_size=5"
	for page := 1; page <= 2; page++ {
		versions := f.request(t, f.admin, "GET", fmt.Sprintf("%s&page=%d", versionPath, page), nil)
		expectStatus(t, versions, 200)
		items := versions.body["items"].([]any)
		if versions.body["total"] != float64(10) || len(items) != 5 {
			t.Fatal("version pagination must count versions", versions.body)
		}
		for index, item := range items {
			x := item.(map[string]any)
			jobs := x["jobs"].([]any)
			if x["version_number"] != float64(10-(page-1)*5-index) || len(jobs) != 2 || jobs[0].(map[string]any)["owner"] == jobs[1].(map[string]any)["owner"] {
				t.Fatal("version must contain both account backups on the same page", x)
			}
		}
	}
	filteredVersions := f.request(t, f.admin, "GET", versionPath+"&status=ready&kind=sync", nil)
	expectStatus(t, filteredVersions, 200)
	if filteredVersions.body["total"] != float64(2) || len(filteredVersions.body["items"].([]any)) != 2 {
		t.Fatal(filteredVersions.body)
	}
	expectStatus(t, f.request(t, f.admin, "GET", "/api/v1/admin/backup-jobs?group_by=version", nil), 400)
	for _, check := range []struct {
		path  string
		total int
	}{
		{jobPath + "&group_by=repository&page=100", 2000},
		{"/api/v1/admin/backup-jobs?group_by=repository&q=00042&status=ready&kind=sync", 2},
		{"/api/v1/admin/backup-jobs?repository=mock-owner/ph-scale-00042&page_size=5&page=2", 10},
	} {
		x := f.request(t, f.admin, "GET", check.path, nil)
		expectStatus(t, x, 200)
		if x.body["total"] != float64(check.total) || len(x.body["items"].([]any)) == 0 {
			t.Fatal(check.path, x.body)
		}
	}
	for _, path := range []string{jobPath + "&group_by=invalid", jobPath + "&repository=invalid"} {
		expectStatus(t, f.request(t, f.admin, "GET", path, nil), 400)
	}
	// Shared repositories must stay together even across engineering targets and letter case.
	if _, err := f.a.DB.Exec(context.Background(), `UPDATE backup_targets SET owner='MOCK-OWNER',repo='PH-SCALE-00042' WHERE engineering_id=md5('scale-e-43')::uuid AND owner='mock-owner'`); err != nil {
		t.Fatal(err)
	}
	sharedPath := "/api/v1/admin/backup-jobs?repository=mock-owner/ph-scale-00042"
	shared := f.request(t, f.admin, "GET", sharedPath+"&group_by=repository", nil)
	expectStatus(t, shared, 200)
	sharedItems := shared.body["items"].([]any)
	if shared.body["total"] != float64(1) || len(sharedItems) != 1 {
		t.Fatal(shared.body)
	}
	summary := sharedItems[0].(map[string]any)
	if summary["engineering_count"] != float64(2) || summary["version_count"] != float64(20) || summary["job_count"] != float64(20) {
		t.Fatal("shared repository split into different targets", summary)
	}
	sharedVersions := f.request(t, f.admin, "GET", sharedPath+"&page_size=10&page=2", nil)
	expectStatus(t, sharedVersions, 200)
	if sharedVersions.body["total"] != float64(20) || len(sharedVersions.body["items"].([]any)) != 10 {
		t.Fatal(sharedVersions.body)
	}
	if _, err := f.a.DB.Exec(context.Background(), `INSERT INTO backup_jobs(id,target_id,version_id,kind,status) SELECT md5('repeat-restore')::uuid,target_id,version_id,'restore','ready' FROM backup_jobs WHERE target_id=(SELECT id FROM backup_targets WHERE engineering_id=md5('scale-e-42')::uuid AND owner='mock-owner') LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	repeated := f.request(t, f.admin, "GET", sharedPath+"&group_by=repository", nil)
	expectStatus(t, repeated, 200)
	repeatedSummary := repeated.body["items"].([]any)[0].(map[string]any)
	if repeatedSummary["version_count"] != float64(20) || repeatedSummary["job_count"] != float64(21) {
		t.Fatal("repeated tasks must not inflate version count", repeatedSummary)
	}
	_, err := f.a.DB.Exec(context.Background(), `INSERT INTO github_accounts(id,name,login,token_ciphertext,token_suffix,active,verified_at,expires_at)
 SELECT md5('scale-account-'||n)::uuid,'分页账号-'||n,'scale-account-'||n,'unused-test-ciphertext','0000',n>=99,
 CASE WHEN n=100 THEN NULL ELSE now() END,CASE WHEN n=99 THEN now()-interval '1 day' ELSE NULL END
 FROM generate_series(1,101) n`)
	if err != nil {
		t.Fatal(err)
	}
	accountsPage := f.request(t, f.admin, "GET", "/api/v1/admin/github-accounts?page_size=1", nil)
	expectStatus(t, accountsPage, 200)
	if accountsPage.body["total"] != float64(103) || accountsPage.body["usable_total"] != float64(3) || len(accountsPage.body["items"].([]any)) != 1 {
		t.Fatal("global usable account count must ignore pagination, disabled, expired and unverified accounts", accountsPage.body)
	}
}
