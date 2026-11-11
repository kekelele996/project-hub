package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

func waitForDBCondition(t *testing.T, f *testFixture, condition string, args ...any) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var met bool
		if err := f.a.DB.QueryRow(context.Background(), condition, args...).Scan(&met); err != nil {
			t.Fatal(err)
		}
		if met {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("database condition did not occur: %s", condition)
}

func waitForUserLock(t *testing.T, f *testFixture, queryPrefix string) {
	t.Helper()
	waitForDBCondition(t, f, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND wait_event_type='Lock' AND query LIKE $1)`, queryPrefix+"%")
}

// The trigger pauses after password verification but before inserting a session.
func TestPostgresPasswordResetCannotBeBypassedByInflightLogin(t *testing.T) {
	f := integrationFixture(t)
	ctx := context.Background()
	oldPassword, newPassword := "old-password-2026", "new-password-2026"
	if _, err := f.a.DB.Exec(ctx, `UPDATE users SET password_hash=$1 WHERE id=$2`, passwordHash(oldPassword), f.memberA.user.ID); err != nil {
		t.Fatal(err)
	}
	gateKey := time.Now().UnixNano()
	sql := fmt.Sprintf(`CREATE FUNCTION pause_login_session() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.user_id::text='%s' THEN PERFORM pg_advisory_xact_lock(%d); END IF; RETURN NEW; END $$; CREATE TRIGGER pause_login BEFORE INSERT ON sessions FOR EACH ROW EXECUTE FUNCTION pause_login_session()`, f.memberA.user.ID, gateKey)
	if _, err := f.a.DB.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	gate, err := f.a.DB.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Release()
	if _, err = gate.Exec(ctx, `SELECT pg_advisory_lock($1)`, gateKey); err != nil {
		t.Fatal(err)
	}
	defer gate.Exec(ctx, `SELECT pg_advisory_unlock($1)`, gateKey)
	loginResult := make(chan testResult, 1)
	go func() {
		loginResult <- f.request(t, testIdentity{}, "POST", "/api/v1/auth/login", map[string]any{"username": f.memberA.user.Username, "password": oldPassword})
	}()
	waitForUserLock(t, f, "INSERT INTO sessions")
	resetResult := make(chan testResult, 1)
	go func() {
		resetResult <- f.request(t, f.admin, "PATCH", "/api/v1/admin/users/"+f.memberA.user.ID, map[string]any{"password": newPassword})
	}()
	waitForUserLock(t, f, "SELECT id,username,name,role,active,created_at FROM users WHERE id=")
	if _, err = gate.Exec(ctx, `SELECT pg_advisory_unlock($1)`, gateKey); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-loginResult:
		expectStatus(t, result, 200)
	case <-time.After(5 * time.Second):
		t.Fatal("login failed to finish")
	}
	select {
	case result := <-resetResult:
		expectStatus(t, result, 200)
	case <-time.After(5 * time.Second):
		t.Fatal("reset failed to finish")
	}
	var activeSessions int
	if err = f.a.DB.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, f.memberA.user.ID).Scan(&activeSessions); err != nil || activeSessions != 0 {
		t.Fatalf("stale login survived password reset: count=%d err=%v", activeSessions, err)
	}
	expectStatus(t, f.request(t, testIdentity{}, "POST", "/api/v1/auth/login", map[string]any{"username": f.memberA.user.Username, "password": oldPassword}), 401)
	// Remove the gate before proving the updated password remains usable.
	if _, err = f.a.DB.Exec(ctx, `DROP TRIGGER pause_login ON sessions`); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, testIdentity{}, "POST", "/api/v1/auth/login", map[string]any{"username": f.memberA.user.Username, "password": newPassword}), 200)
}

func uploadTestBody(t *testing.T) ([]byte, string) {
	t.Helper()
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	file, err := zw.Create("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write([]byte("package main\nfunc main() {}\n")); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	var screenshot bytes.Buffer
	if err = png.Encode(&screenshot, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range map[string]string{"name": "凭证撤销验收", "github_url": "https://github.com/example/verification", "github_ref": "v1.0.0", "tech_stack": "[\"Go\"]"} {
		if err = writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, part := range []struct {
		name, filename string
		data           []byte
	}{{"zip", "source.zip", archive.Bytes()}, {"startup_screenshot", "startup.png", screenshot.Bytes()}, {"feature_screenshots", "feature.png", screenshot.Bytes()}} {
		field, err := writer.CreateFormFile(part.name, part.filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = field.Write(part.data); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func TestPostgresRevokedTokenCannotCommitSlowUpload(t *testing.T) {
	f := integrationFixture(t)
	token := f.request(t, f.admin, "POST", "/api/v1/tokens", map[string]any{"name": "slow-upload"})
	expectStatus(t, token, 201)
	body, contentType := uploadTestBody(t)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", f.server.URL+"/api/v1/admin/engineering-versions", reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token.body["token"].(string))
	req.Header.Set("Content-Type", contentType)
	result := make(chan testResult, 1)
	errors := make(chan error, 1)
	go func() {
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			errors <- err
			return
		}
		defer response.Body.Close()
		var value map[string]any
		_ = json.NewDecoder(response.Body).Decode(&value)
		result <- testResult{status: response.StatusCode, body: value}
	}()
	if _, err = writer.Write(body[:len(body)/2]); err != nil {
		t.Fatal(err)
	}
	waitForDBCondition(t, f, `SELECT last_used_at IS NOT NULL FROM api_tokens WHERE id=$1`, token.body["id"])
	expectStatus(t, f.request(t, f.admin, "DELETE", "/api/v1/tokens/"+token.body["id"].(string), nil), 200)
	if _, err = writer.Write(body[len(body)/2:]); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-result:
		expectStatus(t, response, 403)
	case err := <-errors:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("slow upload timeout")
	}
	var versions, jobs int
	if err = f.a.DB.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM engineering_versions),(SELECT count(*) FROM analysis_jobs)`).Scan(&versions, &jobs); err != nil || versions != 1 || jobs != 0 {
		t.Fatalf("revoked upload committed: versions=%d jobs=%d err=%v", versions, jobs, err)
	}
}

func TestPostgresWriteAndCredentialRevocationUseOneLockOrder(t *testing.T) {
	f := integrationFixture(t)
	token := f.request(t, f.admin, "POST", "/api/v1/tokens", map[string]any{"name": "write-lock"})
	expectStatus(t, token, 201)
	state := authState{User: f.admin.user, Bearer: true, TokenID: token.body["id"].(string)}
	ctx := context.WithValue(context.Background(), authKey{}, state)
	tx, err := f.a.beginAuthorized(ctx, f.admin.user, true)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	revoked := make(chan testResult, 1)
	go func() { revoked <- f.request(t, f.admin, "DELETE", "/api/v1/tokens/"+state.TokenID, nil) }()
	waitForUserLock(t, f, "SELECT id,username,name,role,active,created_at FROM users WHERE id=")
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-revoked:
		expectStatus(t, result, 200)
	case <-time.After(5 * time.Second):
		t.Fatal("revocation deadlocked")
	}
	if next, err := f.a.beginAuthorized(ctx, f.admin.user, true); err != errPermission {
		if next != nil {
			_ = next.Rollback(ctx)
		}
		t.Fatalf("revoked bearer snapshot reused: %v", err)
	}
	var sessionID string
	if err = f.a.DB.QueryRow(context.Background(), `SELECT id FROM sessions WHERE token_hash=$1`, tokenHash(f.memberA.cookie)).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	cookieCtx := context.WithValue(context.Background(), authKey{}, authState{User: f.memberA.user, SessionID: sessionID})
	expectStatus(t, f.request(t, f.memberA, "POST", "/api/v1/auth/logout", nil), 204)
	if next, err := f.a.beginAuthorized(cookieCtx, f.memberA.user, false); err != errPermission {
		if next != nil {
			_ = next.Rollback(cookieCtx)
		}
		t.Fatalf("logged out cookie snapshot reused: %v", err)
	}
	// Revoking one credential does not change role or active status.
	var active bool
	var role string
	if err = f.a.DB.QueryRow(context.Background(), `SELECT active,role FROM users WHERE id=$1`, f.admin.user.ID).Scan(&active, &role); err != nil || !active || strings.TrimSpace(role) != "admin" {
		t.Fatal("credential test unexpectedly changed identity")
	}
}
