package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type PDDAccount struct {
	ID, Name, Cookie, PDDUID, DefaultAddressID, UserAgent, CredentialStatus, LastError string
	UserID, LastVerifiedAt, CreatedAt, UpdatedAt                                       int64
	Enabled, IsDefault                                                                 bool
}

type PDDAccountStore struct {
	DB      *sql.DB
	Dialect Dialect
	codec   *secretCodec
}

// GetByID returns and decrypts the exact account attached to a purchase task.
// Workers must not silently fall back to the current default account because
// doing so could run a task with credentials from another PDD account.
func (s *PDDAccountStore) GetByID(ctx context.Context, id string) (*PDDAccount, error) {
	var a PDDAccount
	var cookie string
	var enabled, isDefault int
	err := s.DB.QueryRowContext(ctx, `SELECT id,user_id,name,cookie_encrypted,pdd_uid,default_address_id,user_agent,enabled,is_default,credential_status,last_verified_at,last_error,created_at,updated_at FROM pdd_accounts WHERE id=? LIMIT 1`, strings.TrimSpace(id)).Scan(&a.ID, &a.UserID, &a.Name, &cookie, &a.PDDUID, &a.DefaultAddressID, &a.UserAgent, &enabled, &isDefault, &a.CredentialStatus, &a.LastVerifiedAt, &a.LastError, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	plain, err := s.codec.decrypt("pdd-cookie", a.ID, cookie)
	if err != nil {
		return nil, err
	}
	a.Cookie, a.Enabled, a.IsDefault = plain, enabled != 0, isDefault != 0
	return &a, nil
}

func (s *PDDAccountStore) Default(ctx context.Context, userID int64) (*PDDAccount, error) {
	var a PDDAccount
	var cookie string
	var enabled, isDefault int
	err := s.DB.QueryRowContext(ctx, `SELECT id,user_id,name,cookie_encrypted,pdd_uid,default_address_id,user_agent,enabled,is_default,credential_status,last_verified_at,last_error,created_at,updated_at FROM pdd_accounts WHERE user_id=? AND is_default=1 LIMIT 1`, userID).Scan(&a.ID, &a.UserID, &a.Name, &cookie, &a.PDDUID, &a.DefaultAddressID, &a.UserAgent, &enabled, &isDefault, &a.CredentialStatus, &a.LastVerifiedAt, &a.LastError, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	plain, err := s.codec.decrypt("pdd-cookie", a.ID, cookie)
	if err != nil {
		return nil, err
	}
	a.Cookie, a.Enabled, a.IsDefault = plain, enabled != 0, isDefault != 0
	return &a, nil
}

func (s *PDDAccountStore) SaveSingle(ctx context.Context, userID int64, name, cookie, pddUID, addressID, userAgent string, enabled bool) (*PDDAccount, error) {
	if s.codec == nil || s.codec.aead == nil {
		return nil, errors.New("XIANYU_DATA_KEY 未配置，禁止保存拼多多 Cookie")
	}
	existing, err := s.Default(ctx, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	now := time.Now().Unix()
	if existing == nil {
		id := uuid.NewString()
		encrypted, err := s.codec.encrypt("pdd-cookie", id, strings.TrimSpace(cookie))
		if err != nil {
			return nil, err
		}
		_, err = s.DB.ExecContext(ctx, `INSERT INTO pdd_accounts(id,user_id,name,cookie_encrypted,pdd_uid,default_address_id,user_agent,enabled,is_default,credential_status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,1,'unchecked',?,?)`, id, userID, name, encrypted, pddUID, addressID, userAgent, boolIntDB(enabled), now, now)
		if err != nil {
			return nil, err
		}
		return s.Default(ctx, userID)
	}
	if strings.TrimSpace(cookie) == "" {
		cookie = existing.Cookie
	}
	encrypted, err := s.codec.encrypt("pdd-cookie", existing.ID, strings.TrimSpace(cookie))
	if err != nil {
		return nil, err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE pdd_accounts SET name=?,cookie_encrypted=?,pdd_uid=?,default_address_id=?,user_agent=?,enabled=?,credential_status='unchecked',last_verified_at=0,last_error='',updated_at=? WHERE id=? AND user_id=?`, name, encrypted, pddUID, addressID, userAgent, boolIntDB(enabled), now, existing.ID, userID)
	if err != nil {
		return nil, err
	}
	return s.Default(ctx, userID)
}

func (s *PDDAccountStore) DeleteDefault(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM pdd_accounts WHERE user_id=? AND is_default=1`, userID)
	return err
}
func (s *PDDAccountStore) MarkVerified(ctx context.Context, id string, userID int64, status, lastError string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE pdd_accounts SET credential_status=?,last_verified_at=?,last_error=?,updated_at=? WHERE id=? AND user_id=?`, status, time.Now().Unix(), lastError, time.Now().Unix(), id, userID)
	return err
}
func boolIntDB(v bool) int {
	if v {
		return 1
	}
	return 0
}
