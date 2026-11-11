package app

import (
	"testing"
	"time"
)

func TestPasswordHash(t *testing.T) {
	hash := passwordHash("test-password-123")
	if !passwordValid("test-password-123", hash) {
		t.Fatal("hash must verify")
	}
	if passwordValid("wrong", hash) {
		t.Fatal("wrong password accepted")
	}
	if passwordValid("x", "argon2id$garbage$garbage") {
		t.Fatal("malformed hash accepted")
	}
}
func TestProjectValidation(t *testing.T) {
	now := time.Now()
	in := projectInput{Name: "评审项目", StartAt: now, EndAt: now.Add(time.Hour)}
	if !validateProject(&in) {
		t.Fatal("valid project rejected")
	}
	in.EndAt = now.Add(-time.Second)
	if validateProject(&in) {
		t.Fatal("inverted dates accepted")
	}
	in.EndAt = now
	in.MinFiles = 10
	in.MaxFiles = 5
	if validateProject(&in) {
		t.Fatal("inverted file limits accepted")
	}
}
func TestIdentifiersAndTokens(t *testing.T) {
	if !validID(newID()) || validID("bad-id") {
		t.Fatal("invalid id validation")
	}
	a, b := randomToken(), randomToken()
	if a == b || len(a) != 64 || tokenHash(a) == tokenHash(b) {
		t.Fatal("tokens must be unique")
	}
}
