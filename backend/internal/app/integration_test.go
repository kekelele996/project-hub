package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type testIdentity struct {
	user         User
	cookie, csrf string
}
type testResult struct {
	status int
	body   map[string]any
}
type testFixture struct {
	a                                    *App
	server                               *httptest.Server
	admin, memberA, memberB              testIdentity
	project, engineering, version, asset string
}

func integrationFixture(t *testing.T) *testFixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置；跳过 PostgreSQL 集成测试")
	}
	ctx := context.Background()
	bootstrap, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + strings.ReplaceAll(newID(), "-", "")
	if _, err = bootstrap.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.ConnConfig.RuntimeParams["application_name"] = schema
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{DB: db, UploadDir: t.TempDir()}
	if err = a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = a.Migrate(ctx); err != nil {
		t.Fatal("migration repeat:", err)
	}
	f := &testFixture{a: a, project: newID(), engineering: newID(), version: newID(), asset: newID()}
	f.admin = f.identity(t, "admin")
	f.memberA = f.identity(t, "member")
	f.memberB = f.identity(t, "member")
	for _, sql := range []struct {
		query string
		args  []any
	}{{`INSERT INTO engineering(id,name) VALUES($1,'测试工程')`, []any{f.engineering}}, {`INSERT INTO engineering_versions(id,engineering_id,version_number,status,zip_path,tech_stack,languages) VALUES($1,$2,1,'ready','fixture.zip','["Go"]','[{"name":"Go","code":1,"files":1,"percentage":100}]')`, []any{f.version, f.engineering}}, {`INSERT INTO engineering_assets(id,version_id,kind,path,mime_type) VALUES($1,$2,'startup','fixture.png','image/png')`, []any{f.asset, f.version}}, {`INSERT INTO projects(id,name,start_at,end_at) VALUES($1,'测试项目',$2,$3)`, []any{f.project, time.Now().Add(-time.Hour), time.Now().Add(time.Hour)}}, {`INSERT INTO project_engineerings(id,project_id,engineering_id,version_id) VALUES($1,$2,$3,$4)`, []any{newID(), f.project, f.engineering, f.version}}, {`INSERT INTO project_members(project_id,user_id) VALUES($1,$2),($1,$3)`, []any{f.project, f.memberA.user.ID, f.memberB.user.ID}}} {
		if _, err = db.Exec(ctx, sql.query, sql.args...); err != nil {
			t.Fatal(err)
		}
	}
	f.server = httptest.NewServer(a.Routes())
	t.Cleanup(func() {
		f.server.Close()
		db.Close()
		_, _ = bootstrap.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`)
		bootstrap.Close()
	})
	return f
}
func (f *testFixture) identity(t *testing.T, role string) testIdentity {
	t.Helper()
	i := testIdentity{user: User{ID: newID(), Role: role, Active: true}, cookie: randomToken(), csrf: randomToken()}
	i.user.Username = i.user.ID
	i.user.Name = role
	ctx := context.Background()
	if _, err := f.a.DB.Exec(ctx, `INSERT INTO users(id,username,name,password_hash,role) VALUES($1,$2,$3,'unused',$4)`, i.user.ID, i.user.Username, i.user.Name, role); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.DB.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,csrf_token,expires_at) VALUES($1,$2,$3,$4,$5)`, newID(), i.user.ID, tokenHash(i.cookie), i.csrf, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return i
}
func (f *testFixture) request(t *testing.T, i testIdentity, method, path string, body any) testResult {
	t.Helper()
	raw, _ := json.Marshal(body)
	r, err := http.NewRequest(method, f.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(&http.Cookie{Name: "projecthub_session", Value: i.cookie})
	r.Header.Set("X-CSRF-Token", i.csrf)
	r.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return testResult{status: res.StatusCode, body: out}
}
func (f *testFixture) engineeringPath() string {
	return "/api/v1/projects/" + f.project + "/engineering/" + f.engineering
}
func expectStatus(t *testing.T, result testResult, status int) {
	t.Helper()
	if result.status != status {
		t.Fatalf("status=%d expected=%d body=%v", result.status, status, result.body)
	}
}

func TestPostgresConcurrentClaimsAndStaleRelease(t *testing.T) {
	f := integrationFixture(t)
	var results [2]testResult
	var wg sync.WaitGroup
	for ix, i := range []testIdentity{f.memberA, f.memberB} {
		wg.Add(1)
		go func(ix int, i testIdentity) {
			defer wg.Done()
			results[ix] = f.request(t, i, "POST", f.engineeringPath()+"/claim", nil)
		}(ix, i)
	}
	wg.Wait()
	winner, loser := f.memberA, f.memberB
	winning := results[0]
	if results[0].status == 409 {
		winner, loser = f.memberB, f.memberA
		winning = results[1]
	}
	if !((results[0].status == 201 && results[1].status == 409) || (results[0].status == 409 && results[1].status == 201)) {
		t.Fatalf("claim race: %v", results)
	}
	idem := f.request(t, winner, "POST", f.engineeringPath()+"/claim", nil)
	expectStatus(t, idem, 200)
	if idem.body["id"] != winning.body["id"] {
		t.Fatal("idempotent claim changed id")
	}
	expectStatus(t, f.request(t, winner, "POST", f.engineeringPath()+"/release", map[string]any{"claim_id": winning.body["id"]}), 204)
	next := f.request(t, loser, "POST", f.engineeringPath()+"/claim", nil)
	expectStatus(t, next, 201)
	expectStatus(t, f.request(t, winner, "POST", f.engineeringPath()+"/release", map[string]any{"claim_id": winning.body["id"]}), 409)
	var count int
	if err := f.a.DB.QueryRow(context.Background(), `SELECT count(*) FROM claims WHERE released_at IS NULL`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("active claim count=%d error=%v", count, err)
	}
}
func TestPostgresScopeHistoryAndExplicitVersions(t *testing.T) {
	f := integrationFixture(t)
	claim := f.request(t, f.memberA, "POST", f.engineeringPath()+"/claim", nil)
	expectStatus(t, claim, 201)
	newVersion := newID()
	if _, err := f.a.DB.Exec(context.Background(), `INSERT INTO engineering_versions(id,engineering_id,version_number,status,zip_path) VALUES($1,$2,2,'ready','new.zip')`, newVersion, f.engineering); err != nil {
		t.Fatal(err)
	}
	scopePath := "/api/v1/admin/projects/" + f.project + "/engineering-scope"
	expectStatus(t, f.request(t, f.admin, "POST", scopePath, map[string]any{"remove": []string{f.engineering}}), 409)
	expectStatus(t, f.request(t, f.memberA, "POST", f.engineeringPath()+"/release", map[string]any{"claim_id": claim.body["id"]}), 204)
	expectStatus(t, f.request(t, f.admin, "POST", scopePath, map[string]any{"remove": []string{f.engineering}}), 200)
	expectStatus(t, f.request(t, f.memberA, "GET", "/api/v1/assets/"+f.asset, nil), 403)
	var recorded, version string
	if err := f.a.DB.QueryRow(context.Background(), `SELECT id,version_id FROM claims WHERE id=$1`, claim.body["id"]).Scan(&recorded, &version); err != nil || recorded == "" || version != f.version {
		t.Fatalf("claim history lost: id=%s version=%s err=%v", recorded, version, err)
	}
	list := f.request(t, f.admin, "GET", "/api/v1/projects/"+f.project+"/engineering", nil)
	expectStatus(t, list, 200)
	if list.body["total"] != float64(0) {
		t.Fatal("removed engineering remains visible")
	}
	expectStatus(t, f.request(t, f.admin, "POST", scopePath, map[string]any{"add": []string{f.engineering}}), 200)
	list = f.request(t, f.memberA, "GET", "/api/v1/projects/"+f.project+"/engineering", nil)
	items := list.body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["version_id"] != newVersion {
		t.Fatal("reactivation did not select latest ready version")
	}
	if err := f.a.DB.QueryRow(context.Background(), `SELECT version_id FROM claims WHERE id=$1`, claim.body["id"]).Scan(&version); err != nil || version != f.version {
		t.Fatal("old claim version changed")
	}
}
func TestPostgresMembershipRemovalAndRevocation(t *testing.T) {
	f := integrationFixture(t)
	claim := f.request(t, f.memberA, "POST", f.engineeringPath()+"/claim", nil)
	expectStatus(t, claim, 201)
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/projects/"+f.project+"/members", map[string]any{"remove": []string{f.memberA.user.ID}}), 200)
	expectStatus(t, f.request(t, f.memberA, "GET", "/api/v1/projects/"+f.project+"/engineering", nil), 403)
	expectStatus(t, f.request(t, f.memberA, "GET", "/api/v1/assets/"+f.asset, nil), 403)
	var released *time.Time
	if err := f.a.DB.QueryRow(context.Background(), `SELECT released_at FROM claims WHERE id=$1`, claim.body["id"]).Scan(&released); err != nil || released == nil {
		t.Fatalf("member removal claim not released: %v", err)
	}
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/users/"+f.memberB.user.ID, map[string]any{"active": false}), 200)
	expectStatus(t, f.request(t, f.memberB, "GET", "/api/v1/auth/me", nil), 401)
	tx, err := f.a.beginAuthorized(context.Background(), f.memberB.user, false)
	if tx != nil {
		_ = tx.Rollback(context.Background())
	}
	if err != errPermission {
		t.Fatal("transaction allowed disabled user")
	}
}
func TestPostgresTransferAndExpiration(t *testing.T) {
	f := integrationFixture(t)
	claim := f.request(t, f.memberA, "POST", f.engineeringPath()+"/claim", nil)
	expectStatus(t, claim, 201)
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/claims/"+claim.body["id"].(string)+"/transfer", map[string]any{"user_id": f.memberB.user.ID}), 200)
	expectStatus(t, f.request(t, f.memberA, "POST", f.engineeringPath()+"/release", map[string]any{"claim_id": claim.body["id"]}), 409)
	if _, err := f.a.DB.Exec(context.Background(), `UPDATE projects SET end_at=now()-interval '1 minute' WHERE id=$1`, f.project); err != nil {
		t.Fatal(err)
	}
	list := f.request(t, f.memberB, "GET", "/api/v1/projects/"+f.project+"/engineering", nil)
	expectStatus(t, list, 200)
	item := list.body["items"].([]any)[0].(map[string]any)
	if item["can_download"] != true {
		t.Fatal("expired project revoked existing owner download")
	}
	expectStatus(t, f.request(t, f.memberB, "POST", f.engineeringPath()+"/release", map[string]any{"claim_id": item["claim_id"]}), 204)
	expectStatus(t, f.request(t, f.memberA, "POST", f.engineeringPath()+"/claim", nil), 409)
}

func TestPostgresUserAndArchiveFilters(t *testing.T) {
	f := integrationFixture(t)
	if _, err := f.a.DB.Exec(context.Background(), `UPDATE users SET active=false WHERE id=$1`, f.memberA.user.ID); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		query     string
		total     float64
		itemCount int
	}{{"role=admin", 1, 1}, {"role=member&active=true", 1, 1}, {"role=member&active=false", 1, 1}, {"active=false&page=2&page_size=1", 1, 0}, {"role=member&page_size=1", 2, 1}, {"role=all&active=all", 3, 3}} {
		result := f.request(t, f.admin, "GET", "/api/v1/admin/users?"+test.query, nil)
		expectStatus(t, result, 200)
		if result.body["total"] != test.total || len(result.body["items"].([]any)) != test.itemCount {
			t.Fatalf("filter %s: %v", test.query, result.body)
		}
	}
	expectStatus(t, f.request(t, f.admin, "GET", "/api/v1/admin/users?active=invalid", nil), 400)
	expectStatus(t, f.request(t, f.admin, "GET", "/api/v1/admin/users?role=invalid", nil), 400)
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/engineering/"+f.engineering, map[string]any{"archived": true}), 200)
	for _, test := range []struct {
		query string
		total float64
	}{{"", 0}, {"archived=false", 0}, {"archived=true", 1}, {"archived=all", 1}} {
		result := f.request(t, f.admin, "GET", "/api/v1/admin/engineering?"+test.query, nil)
		expectStatus(t, result, 200)
		if result.body["total"] != test.total {
			t.Fatalf("archive filter %s: %v", test.query, result.body)
		}
	}
	expectStatus(t, f.request(t, f.admin, "GET", "/api/v1/admin/engineering?archived=invalid", nil), 400)
	expectStatus(t, f.request(t, f.admin, "PATCH", "/api/v1/admin/engineering/"+f.engineering, map[string]any{"archived": false}), 200)
	result := f.request(t, f.admin, "GET", "/api/v1/admin/engineering", nil)
	if result.body["total"] != float64(1) {
		t.Fatal("restored engineering missing", result.body)
	}
}

func TestPostgresEngineeringExcludesActiveProjectScope(t *testing.T) {
	f := integrationFixture(t)
	path := "/api/v1/admin/engineering?exclude_project=" + f.project
	result := f.request(t, f.admin, "GET", path, nil)
	expectStatus(t, result, 200)
	if result.body["total"] != float64(0) || len(result.body["items"].([]any)) != 0 {
		t.Fatal("active project scope not excluded", result.body)
	}
	expectStatus(t, f.request(t, f.admin, "POST", "/api/v1/admin/projects/"+f.project+"/engineering-scope", map[string]any{"remove": []string{f.engineering}}), 200)
	result = f.request(t, f.admin, "GET", path, nil)
	expectStatus(t, result, 200)
	if result.body["total"] != float64(1) || len(result.body["items"].([]any)) != 1 {
		t.Fatal("removed reference still excluded", result.body)
	}
	expectStatus(t, f.request(t, f.admin, "GET", "/api/v1/admin/engineering?exclude_project=invalid", nil), 400)
}
