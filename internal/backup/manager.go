// Package backup creates validated, downloadable database backups.
package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
)

const manifestSuffix = ".manifest.json"

type Entry struct {
	ID                string `json:"id"`
	Filename          string `json:"filename"`
	Dialect           string `json:"dialect"`
	CreatedAt         int64  `json:"created_at"`
	SizeBytes         int64  `json:"size_bytes"`
	SHA256            string `json:"sha256"`
	MappingRows       int    `json:"mapping_rows"`
	DataKeyConfigured bool   `json:"data_key_configured"`
	Verified          bool   `json:"verified"`
}

type Manager struct {
	DB          *sql.DB
	Dialect     db.Dialect
	DatabaseURL string
	Dir         string
	mu          sync.Mutex
	now         func() time.Time
}

func New(database *sql.DB, dialect db.Dialect, databaseURL, dir string) *Manager {
	if strings.TrimSpace(dir) == "" {
		dir = filepath.Join("data", "backups")
	}
	return &Manager{DB: database, Dialect: dialect, DatabaseURL: databaseURL, Dir: dir, now: time.Now}
}

func (m *Manager) List() ([]Entry, error) {
	entries, err := os.ReadDir(m.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]Entry, 0, len(entries))
	for _, item := range entries {
		if item.IsDir() || !strings.HasSuffix(item.Name(), manifestSuffix) {
			continue
		}
		var entry Entry
		raw, readErr := os.ReadFile(filepath.Join(m.Dir, item.Name()))
		if readErr == nil && json.Unmarshal(raw, &entry) == nil && validID(entry.ID) {
			if _, statErr := os.Stat(filepath.Join(m.Dir, entry.Filename)); statErr == nil {
				result = append(result, entry)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result, nil
}

func (m *Manager) Create(ctx context.Context, retentionCount int) (Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if retentionCount < 1 {
		retentionCount = 14
	}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return Entry{}, fmt.Errorf("创建备份目录: %w", err)
	}
	now := m.now()
	id := now.Format("20060102-150405.000000000")
	ext := ".dump"
	if m.Dialect == db.DialectSQLite {
		ext = ".db"
	}
	filename := "xianyu-" + string(m.Dialect) + "-" + id + ext
	finalPath := filepath.Join(m.Dir, filename)
	tmpPath := finalPath + ".partial"
	if err := m.createDump(ctx, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return Entry{}, err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return Entry{}, fmt.Errorf("完成备份文件: %w", err)
	}
	checksum, size, err := fileDigest(finalPath)
	if err != nil {
		return Entry{}, err
	}
	mappingRows, err := m.exportMappings(ctx, filepath.Join(m.Dir, "xianyu-sku-mappings-"+id+".csv"))
	if err != nil {
		return Entry{}, fmt.Errorf("导出 SKU 映射: %w", err)
	}
	entry := Entry{ID: id, Filename: filename, Dialect: string(m.Dialect), CreatedAt: now.Unix(), SizeBytes: size, SHA256: checksum, MappingRows: mappingRows, DataKeyConfigured: strings.TrimSpace(os.Getenv("XIANYU_DATA_KEY")) != "", Verified: true}
	raw, _ := json.MarshalIndent(entry, "", "  ")
	if err := os.WriteFile(filepath.Join(m.Dir, id+manifestSuffix), append(raw, '\n'), 0o600); err != nil {
		return Entry{}, fmt.Errorf("写入备份清单: %w", err)
	}
	if err := m.prune(retentionCount); err != nil {
		return entry, fmt.Errorf("备份成功，但清理旧备份失败: %w", err)
	}
	return entry, nil
}

func (m *Manager) Path(id string) (string, string, error) {
	if !validID(id) {
		return "", "", os.ErrNotExist
	}
	entries, err := m.List()
	if err != nil {
		return "", "", err
	}
	for _, entry := range entries {
		if entry.ID == id {
			return filepath.Join(m.Dir, entry.Filename), entry.Filename, nil
		}
	}
	return "", "", os.ErrNotExist
}

func (m *Manager) createDump(ctx context.Context, target string) error {
	switch m.Dialect {
	case db.DialectPostgres:
		if strings.TrimSpace(m.DatabaseURL) == "" {
			return errors.New("DATABASE_URL 未配置，无法执行 PostgreSQL 备份")
		}
		cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--compress=6", "--no-owner", "--no-privileges", "--file", target, m.DatabaseURL)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("pg_dump 失败: %w: %s", err, m.sanitizeOutput(string(output)))
		}
		verify := exec.CommandContext(ctx, "pg_restore", "--list", target)
		if output, err := verify.CombinedOutput(); err != nil {
			return fmt.Errorf("pg_restore 校验失败: %w: %s", err, m.sanitizeOutput(string(output)))
		}
		return nil
	case db.DialectSQLite:
		quoted := strings.ReplaceAll(target, "'", "''")
		if _, err := m.DB.ExecContext(ctx, "VACUUM INTO '"+quoted+"'"); err != nil {
			return fmt.Errorf("SQLite 在线备份失败: %w", err)
		}
		check, err := sql.Open("sqlite", "file:"+target+"?mode=ro")
		if err != nil {
			return err
		}
		defer check.Close()
		var integrity string
		if err := check.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
			return fmt.Errorf("SQLite 完整性校验失败: %s: %w", integrity, err)
		}
		return nil
	default:
		return fmt.Errorf("当前版本暂不支持 %s 在线备份", m.Dialect)
	}
}

func (m *Manager) exportMappings(ctx context.Context, target string) (int, error) {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"mapping_type", "user_id", "cookie_id", "item_id", "xianyu_sku_id", "source_goods_id", "source_sku_id", "properties_json", "mapping_source", "material_id", "material_sku_id", "publish_record_id", "status", "created_at", "updated_at"})
	count := 0
	queries := []string{
		`SELECT 'item',user_id,cookie_id,item_id,xianyu_sku_id,source_goods_id,source_sku_id,xianyu_properties_json,mapping_source,'','','','',created_at,updated_at FROM item_pdd_sku_mappings ORDER BY user_id,cookie_id,item_id,xianyu_sku_id`,
		`SELECT 'material_publish',r.user_id,r.cookie_id,r.published_item_id,m.xianyu_sku_id,r.source_id,m.source_sku_id,m.published_properties_json,'publish',r.material_id,m.material_sku_id,m.publish_record_id,m.mapping_status,r.created_at,r.finished_at FROM material_publish_sku_mappings m JOIN material_publish_records r ON r.id=m.publish_record_id ORDER BY r.user_id,r.id,m.id`,
	}
	for _, query := range queries {
		rows, queryErr := m.DB.QueryContext(ctx, query)
		if queryErr != nil {
			_ = f.Close()
			return count, queryErr
		}
		for rows.Next() {
			var values [15]string
			args := make([]any, len(values))
			for i := range values {
				args[i] = &values[i]
			}
			if err := rows.Scan(args...); err != nil {
				_ = rows.Close()
				_ = f.Close()
				return count, err
			}
			_ = w.Write(values[:])
			count++
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			_ = f.Close()
			return count, err
		}
		_ = rows.Close()
	}
	w.Flush()
	closeErr := f.Close()
	if err := w.Error(); err != nil {
		return count, err
	}
	return count, closeErr
}

func (m *Manager) prune(keep int) error {
	entries, err := m.List()
	if err != nil || len(entries) <= keep {
		return err
	}
	for _, entry := range entries[keep:] {
		for _, name := range []string{entry.Filename, "xianyu-sku-mappings-" + entry.ID + ".csv", entry.ID + manifestSuffix} {
			path := filepath.Join(m.Dir, name)
			if filepath.Dir(path) != filepath.Clean(m.Dir) {
				return errors.New("拒绝清理备份目录之外的文件")
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

func fileDigest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), size, err
}

func validID(id string) bool {
	if len(id) != len("20060102-150405.000000000") {
		return false
	}
	_, err := time.Parse("20060102-150405.000000000", id)
	return err == nil && !strings.ContainsAny(id, `/\\`)
}

func (m *Manager) sanitizeOutput(value string) string {
	if m.DatabaseURL != "" {
		value = strings.ReplaceAll(value, m.DatabaseURL, "[DATABASE_URL REDACTED]")
	}
	value = strings.TrimSpace(value)
	if len(value) > 600 {
		value = value[:600]
	}
	return value
}
