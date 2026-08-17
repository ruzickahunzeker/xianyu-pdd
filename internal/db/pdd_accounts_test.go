package db

import (
	"context"
	"strings"
	"testing"
)

func TestPDDAccountSingleConfigEncryptsCookieAndKeepsIdentity(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "pdd-account-test-key")
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if ok, err := store.Users.Create(ctx, "pdd-owner", "pdd@example.com", "pw"); err != nil || !ok {
		t.Fatal(err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "pdd-owner")
	a, err := store.PDDAccounts.SaveSingle(ctx, owner.ID, "主账号", "token=x; pdd_user_id=123456; other=y", "123456", "60984097534", "UA", true)
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := store.DB.QueryRow(`SELECT cookie_encrypted FROM pdd_accounts WHERE id=?`, a.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, encryptedValuePrefix) || strings.Contains(raw, "pdd_user_id") {
		t.Fatalf("cookie not encrypted: %q", raw)
	}
	updated, err := store.PDDAccounts.SaveSingle(ctx, owner.ID, "新名称", "", "123456", "60984097534", "UA2", true)
	if err != nil || updated.ID != a.ID || !strings.Contains(updated.Cookie, "pdd_user_id=123456") {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	byID, err := store.PDDAccounts.GetByID(ctx, a.ID)
	if err != nil || byID.ID != a.ID || byID.UserID != owner.ID || byID.UserAgent != "UA2" || !strings.Contains(byID.Cookie, "pdd_user_id=123456") {
		t.Fatalf("byID=%+v err=%v", byID, err)
	}
}
