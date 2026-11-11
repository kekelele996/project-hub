package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type githubSecurityTransport func(*http.Request) (*http.Response, error)

func (f githubSecurityTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func githubSecurityClient(t *testing.T, handler http.HandlerFunc) *GitHubClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &GitHubClient{BaseURL: server.URL, HTTPClient: server.Client(), Token: "ghp_mock_secret_123456"}
}

func githubSecurityJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func TestGitHubSecurityTokenEncryption(t *testing.T) {
	key := bytes.Repeat([]byte{0x51}, 32)
	id := newID()
	token := "github_pat_sensitive_token"
	sealed, err := encryptBackupToken(key, id, token)
	if err != nil {
		t.Fatal(err)
	}
	second, err := encryptBackupToken(key, id, token)
	if err != nil || sealed == second || strings.Contains(sealed, token) {
		t.Fatalf("nonce or ciphertext invalid: %v", err)
	}
	plain, err := decryptBackupToken(key, id, sealed)
	if err != nil || plain != token {
		t.Fatalf("round trip: %v", err)
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(sealed, "v1:"))
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	for _, scenario := range []struct {
		name, id, sealed string
		key              []byte
	}{
		{"tamper", id, "v1:" + base64.RawStdEncoding.EncodeToString(raw), key},
		{"different key", id, sealed, bytes.Repeat([]byte{0x52}, 32)},
		{"different account", newID(), sealed, key},
		{"invalid encoding", id, "v1:!!", key},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if result, err := decryptBackupToken(scenario.key, scenario.id, scenario.sealed); err == nil || result != "" {
				t.Fatal("unauthenticated ciphertext decrypted")
			}
		})
	}
	parsed, err := ParseBackupKey(base64.StdEncoding.EncodeToString(key))
	if err != nil || !bytes.Equal(parsed, key) {
		t.Fatalf("master key rejected: %v", err)
	}
	if parsed, err = ParseBackupKey(""); err != nil || parsed != nil {
		t.Fatal("empty key should leave backup unconfigured")
	}
	if _, err = ParseBackupKey(base64.StdEncoding.EncodeToString(key[:31])); err == nil {
		t.Fatal("wrong key length accepted")
	}
	if suffix := maskBackupToken(token); suffix != "oken" || strings.Contains(suffix, "*") {
		t.Fatalf("token suffix must contain only the last four characters: %q", suffix)
	}
	if suffix := maskBackupToken("abcd"); suffix != "****" {
		t.Fatal("short token should not be exposed")
	}
}

func TestGitHubSecurityRepositoryVerification(t *testing.T) {
	for _, scenario := range []struct {
		name, branch, sha string
		private, push, ok bool
	}{
		{"private writable", "main", strings.Repeat("a", 40), true, true, true},
		{"public", "main", strings.Repeat("a", 40), false, true, false},
		{"read only", "main", strings.Repeat("a", 40), true, false, false},
		{"uninitialized", "", "", true, true, false},
		{"bad commit", "main", "not-a-commit", true, true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			client := githubSecurityClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer ghp_mock_secret_123456" {
					t.Error("authenticated request missing token")
				}
				switch r.URL.Path {
				case "/user":
					githubSecurityJSON(w, 200, map[string]any{"id": 12, "login": "backup-owner"})
				case "/repos/backup-owner/backups":
					githubSecurityJSON(w, 200, map[string]any{"id": 34, "full_name": "backup-owner/backups", "private": scenario.private, "default_branch": scenario.branch, "permissions": map[string]any{"push": scenario.push}})
				case "/repos/backup-owner/backups/commits/main":
					githubSecurityJSON(w, 200, map[string]string{"sha": scenario.sha})
				default:
					t.Error("unexpected request:", r.URL.Path)
					w.WriteHeader(404)
				}
			})
			user, err := client.User(context.Background())
			if err != nil || user.Login != "backup-owner" {
				t.Fatalf("user verification failed: %v", err)
			}
			_, sha, err := client.VerifyRepository(context.Background(), "backup-owner", "backups")
			if (err == nil) != scenario.ok || (scenario.ok && sha != scenario.sha) {
				t.Fatalf("repository verification ok=%v sha=%s err=%v", scenario.ok, sha, err)
			}
		})
	}
}

func TestGitHubSecurityReleaseOwnership(t *testing.T) {
	engineeringID, versionID := newID(), newID()
	tag := "ph-" + engineeringID + "-v1-" + versionID
	for _, scenario := range []string{"create", "reuse", "foreign"} {
		t.Run(scenario, func(t *testing.T) {
			posts := 0
			client := githubSecurityClient(t, func(w http.ResponseWriter, r *http.Request) {
				body := backupReleaseBody(engineeringID, versionID)
				if scenario == "foreign" {
					body = "someone else's release"
				}
				if r.Method == http.MethodGet && scenario == "create" {
					w.WriteHeader(404)
					return
				}
				status := 200
				if r.Method == http.MethodPost {
					posts++
					status = 201
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["body"] != body || payload["tag_name"] != tag || payload["target_commitish"] != strings.Repeat("a", 40) {
						t.Error("unexpected release creation payload")
					}
				}
				githubSecurityJSON(w, status, GitHubRelease{ID: 77, TagName: tag, Body: body})
			})
			_, err := client.EnsureRelease(context.Background(), "owner", "repo", tag, "version 1", strings.Repeat("a", 40), engineeringID, versionID)
			if scenario == "foreign" {
				if !errors.Is(err, ErrGitHubReleaseConflict) || posts != 0 {
					t.Fatalf("foreign release accepted or changed: %v", err)
				}
			} else if err != nil || (scenario == "create" && posts != 1) || (scenario == "reuse" && posts != 0) {
				t.Fatalf("release idempotence posts=%d err=%v", posts, err)
			}
		})
	}
}

func TestGitHubSecurityAssetIdempotence(t *testing.T) {
	content := []byte("verified backup bytes")
	sum := sha256.Sum256(content)
	expected := hex.EncodeToString(sum[:])
	for _, scenario := range []string{"upload", "reuse", "corrupt"} {
		t.Run(scenario, func(t *testing.T) {
			release := GitHubRelease{ID: 77, TagName: "backup-tag", Body: backupReleaseBody(newID(), newID())}
			asset := GitHubAsset{ID: 88, Name: "source.zip", Size: int64(len(content))}
			uploaded, deleted := 0, 0
			client := githubSecurityClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/releases/77":
					githubSecurityJSON(w, 200, release)
				case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/releases/77/assets":
					items := []GitHubAsset{asset}
					if scenario == "upload" && uploaded == 0 {
						items = nil
					}
					githubSecurityJSON(w, 200, items)
				case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/releases/77/assets":
					uploaded++
					raw, err := io.ReadAll(r.Body)
					if err != nil || !bytes.Equal(raw, content) || r.URL.Query().Get("name") != asset.Name || r.Header.Get("Content-Type") != "application/zip" {
						t.Error("uploaded asset content or metadata mismatch")
					}
					githubSecurityJSON(w, 201, asset)
				case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/releases/assets/88":
					if scenario == "corrupt" {
						_, _ = w.Write([]byte("corrupted"))
					} else {
						_, _ = w.Write(content)
					}
				case r.Method == http.MethodDelete:
					deleted++
					w.WriteHeader(204)
				default:
					t.Error("unexpected asset request:", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			})
			release.UploadURL = client.BaseURL + "/repos/owner/repo/releases/77/assets{?name,label}"
			result, err := client.EnsureAsset(context.Background(), "owner", "repo", release, asset.Name, "application/zip", bytes.NewReader(content), int64(len(content)), expected)
			if scenario == "corrupt" {
				if !errors.Is(err, ErrGitHubAssetConflict) || uploaded != 0 || deleted != 0 {
					t.Fatalf("conflicting asset overwritten: uploads=%d deletes=%d err=%v", uploaded, deleted, err)
				}
			} else if err != nil || result.ID != asset.ID || (scenario == "upload" && uploaded != 1) || (scenario == "reuse" && uploaded != 0) {
				t.Fatalf("asset idempotence uploads=%d result=%+v err=%v", uploaded, result, err)
			}
		})
	}
}

func TestGitHubSecurityDownloadRedirect(t *testing.T) {
	for _, redirect := range []string{"https://release-assets.githubusercontent.com/file.zip", "https://attacker.invalid/file.zip", "http://release-assets.githubusercontent.com/file.zip", "https://githubusercontent.com.attacker.invalid/file.zip"} {
		t.Run(redirect, func(t *testing.T) {
			client := githubSecurityClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "" {
					t.Error("API download missing authentication")
				}
				w.Header().Set("Location", redirect)
				w.WriteHeader(http.StatusFound)
			})
			transport := client.HTTPClient.Transport
			crossRequests := 0
			client.HTTPClient.Transport = githubSecurityTransport(func(r *http.Request) (*http.Response, error) {
				if strings.HasPrefix(r.URL.String(), client.BaseURL) {
					return transport.RoundTrip(r)
				}
				crossRequests++
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("cross-origin download leaked credentials")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("asset")), ContentLength: 5, Request: r}, nil
			})
			var output bytes.Buffer
			_, err := client.DownloadAsset(context.Background(), "owner", "repo", 88, &output)
			allowed := redirect == "https://release-assets.githubusercontent.com/file.zip"
			if allowed && (err != nil || crossRequests != 1 || output.String() != "asset") {
				t.Fatalf("allowed redirect failed: requests=%d err=%v", crossRequests, err)
			}
			if !allowed && (err == nil || crossRequests != 0) {
				t.Fatalf("untrusted redirect reached transport: requests=%d err=%v", crossRequests, err)
			}
		})
	}
}

func TestGitHubSecurityUploadURLValidation(t *testing.T) {
	client := githubSecurityClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid upload URL reached server")
	})
	for _, address := range []string{
		"https://attacker.invalid/repos/owner/repo/releases/77/assets",
		"https://uploads.github.com.attacker.invalid/repos/owner/repo/releases/77/assets",
		"https://secret@uploads.github.com/repos/owner/repo/releases/77/assets",
		"https://uploads.github.com/repos/owner/repo/releases/78/assets",
		"https://uploads.github.com/repos/owner/repo/releases/77/assets?name=unexpected",
		"https://uploads.github.com/repos/owner/repo/releases/77/assets{?unknown}",
	} {
		if _, err := client.uploadAddress("owner", "repo", GitHubRelease{ID: 77, UploadURL: address}, "source.zip"); err == nil {
			t.Error("untrusted upload address accepted:", address)
		}
	}
	address, err := client.uploadAddress("owner", "repo", GitHubRelease{ID: 77, UploadURL: "https://uploads.github.com/repos/owner/repo/releases/77/assets{?name,label}"}, "source.zip")
	parsed, parseErr := url.Parse(address)
	if err != nil || parseErr != nil || parsed.Query().Get("name") != "source.zip" {
		t.Fatalf("trusted upload address rejected: %v", err)
	}
}

func TestGitHubSecurityErrorsDoNotLeakSecrets(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 422, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := githubSecurityClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("secret upstream body ghp_mock_secret_123456"))
			})
			_, err := client.User(context.Background())
			if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), client.Token) {
				t.Fatalf("upstream error leaked details: %v", err)
			}
		})
	}
}

func TestGitHubSecurityTargetLockSingleConnection(t *testing.T) {
	f := integrationFixture(t)
	accountID, targetID := newID(), newID()
	ctx := context.Background()
	if _, err := f.a.DB.Exec(ctx, `INSERT INTO github_accounts(id,name,login,token_ciphertext,token_suffix) VALUES($1,'review-account','review-owner','unused','1234')`, accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.DB.Exec(ctx, `INSERT INTO backup_targets(id,engineering_id,account_id,owner,repo) VALUES($1,$2,$3,'review-owner','review-repo')`, targetID, f.engineering, accountID); err != nil {
		t.Fatal(err)
	}
	config := f.a.DB.Config()
	config.MaxConns = 1
	config.MinConns = 0
	single, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(single.Close)
	f.a.DB = single
	tx, err := single.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	limited, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	target, err := f.a.lockUsableTarget(limited, tx, targetID)
	if err != nil || target.ID != targetID {
		t.Fatalf("target lookup must use the transaction connection: target=%s error=%v", target.ID, err)
	}
}

func TestGitHubSecurityAdminListsRecheckDatabaseRole(t *testing.T) {
	f := integrationFixture(t)
	paths := []string{"/api/v1/admin/github-accounts", "/api/v1/admin/backup-targets", "/api/v1/admin/backup-jobs"}
	for _, path := range paths {
		expectStatus(t, f.request(t, f.admin, http.MethodGet, path, nil), 200)
		expectStatus(t, f.request(t, f.memberA, http.MethodGet, path, nil), 403)
	}
	if _, err := f.a.DB.Exec(context.Background(), `UPDATE users SET role='member' WHERE id=$1`, f.admin.user.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		expectStatus(t, f.request(t, f.admin, http.MethodGet, path, nil), 403)
	}
	if _, err := f.a.DB.Exec(context.Background(), `UPDATE users SET active=false WHERE id=$1`, f.admin.user.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		expectStatus(t, f.request(t, f.admin, http.MethodGet, path, nil), 401)
	}
}

func TestGitHubSecurityCorruptRestoreKeepsOriginal(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "code.zip")
	original := []byte("original local file")
	if err := os.WriteFile(destination, original, 0600); err != nil {
		t.Fatal(err)
	}
	expectedContent := []byte("verified backup bytes")
	sum := sha256.Sum256(expectedContent)
	expected := backupFile{Size: int64(len(expectedContent)), SHA256: hex.EncodeToString(sum[:])}
	client := githubSecurityClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte{'x'}, len(expectedContent)))
	})
	_, err := stageBackupDownload(context.Background(), client, BackupTarget{Owner: "owner", Repo: "repo"}, GitHubAsset{ID: 88}, expected, destination)
	if err == nil {
		t.Fatal("corrupted restore accepted")
	}
	actual, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("corrupted download changed the original local file")
	}
	temporary, err := filepath.Glob(filepath.Join(directory, ".restore-*"))
	if err != nil || len(temporary) != 0 {
		t.Fatal("failed download left temporary files")
	}
}

func TestGitHubSecurityCompletionRechecksAvailability(t *testing.T) {
	for _, scenario := range []string{"ready", "account disabled", "account expired", "account revision changed", "target disabled", "target revision changed", "lost lease"} {
		t.Run(scenario, func(t *testing.T) {
			f := integrationFixture(t)
			ctx := context.Background()
			accountID, targetID, jobID := newID(), newID(), newID()
			if _, err := f.a.DB.Exec(ctx, `INSERT INTO github_accounts(id,name,login,token_ciphertext,token_suffix) VALUES($1,'review-account','review-owner','unused','1234')`, accountID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.a.DB.Exec(ctx, `INSERT INTO backup_targets(id,engineering_id,account_id,owner,repo) VALUES($1,$2,$3,'review-owner','review-repo')`, targetID, f.engineering, accountID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.a.DB.Exec(ctx, `INSERT INTO backup_jobs(id,target_id,version_id,kind) VALUES($1,$2,$3,'restore')`, jobID, targetID, f.version); err != nil {
				t.Fatal(err)
			}
			work, err := f.a.reserveBackupJob(ctx)
			if err != nil {
				t.Fatal(err)
			}
			work.Target, err = f.a.readTarget(ctx, targetID)
			if err != nil {
				t.Fatal(err)
			}
			work.Account, err = f.a.readAccount(ctx, accountID)
			if err != nil {
				t.Fatal(err)
			}
			var query string
			var id string
			switch scenario {
			case "account disabled":
				query, id = `UPDATE github_accounts SET active=false WHERE id=$1`, accountID
			case "account expired":
				query, id = `UPDATE github_accounts SET expires_at=now()-interval '1 day' WHERE id=$1`, accountID
			case "account revision changed":
				query, id = `UPDATE github_accounts SET revision=revision+1 WHERE id=$1`, accountID
			case "target disabled":
				query, id = `UPDATE backup_targets SET enabled=false WHERE id=$1`, targetID
			case "target revision changed":
				query, id = `UPDATE backup_targets SET revision=revision+1 WHERE id=$1`, targetID
			case "lost lease":
				query, id = `UPDATE backup_jobs SET lease_owner='another-worker' WHERE id=$1`, jobID
			}
			if query != "" {
				if _, err = f.a.DB.Exec(ctx, query, id); err != nil {
					t.Fatal(err)
				}
			}
			destination := filepath.Join(f.a.UploadDir, "original.zip")
			temporary := filepath.Join(f.a.UploadDir, "staged.zip")
			if err = os.WriteFile(destination, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(temporary, []byte("verified"), 0600); err != nil {
				t.Fatal(err)
			}
			result := backupResult{Manifest: []byte(`{}`), Remote: []byte(`{}`), Staged: []stagedBackupFile{{temporary: temporary, destination: destination}}}
			if err = f.a.finishBackupWork(ctx, work, result, nil); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			var status string
			if err = f.a.DB.QueryRow(ctx, `SELECT status FROM backup_jobs WHERE id=$1`, jobID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if scenario == "ready" {
				if string(content) != "verified" || status != "ready" {
					t.Fatalf("valid restore not committed: content=%q status=%s", content, status)
				}
			} else if string(content) != "original" || (scenario != "lost lease" && status != "failed") || (scenario == "lost lease" && status != "running") {
				t.Fatalf("invalidated restore changed state: content=%q status=%s", content, status)
			}
		})
	}
}
