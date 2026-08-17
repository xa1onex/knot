package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.runMigrations(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func NewID() string { return uuid.NewString() }

func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// --- Users ---

type User struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (*User, error) {
	u := &User{ID: NewID(), Email: email, PasswordHash: passwordHash, CreatedAt: time.Now().UTC()}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &created)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	var u User
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &created)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &u, nil
}

// --- Sessions ---

type Session struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

func (s *Store) CreateSession(ctx context.Context, userID, tokenHash string, expires time.Time) (*Session, error) {
	sess := &Session{ID: NewID(), UserID: userID, TokenHash: tokenHash, ExpiresAt: expires, CreatedAt: time.Now().UTC()}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.TokenHash, sess.ExpiresAt.Format(time.RFC3339Nano), sess.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, hash string) (*Session, error) {
	var sess Session
	var exp, created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, token_hash, expires_at, created_at FROM sessions WHERE token_hash = ?`, hash).
		Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &exp, &created)
	if err != nil {
		return nil, err
	}
	sess.ExpiresAt, _ = time.Parse(time.RFC3339Nano, exp)
	sess.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &sess, nil
}

func (s *Store) DeleteSessionByTokenHash(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hash)
	return err
}

// --- Credentials ---

const (
	CredentialKindAPI = "api"
	CredentialKindAI  = "temporary_ai"
)

type Credential struct {
	ID              string
	UserID          string
	Name            string
	TokenHash       string
	TokenPrefix     string
	Kind            string
	Scopes          []string
	ExpiresAt       *time.Time
	RevokedAt       *time.Time
	CreatedAt       time.Time
	MaxStorageBytes *int64
	MaxFileBytes    *int64
	MaxFiles        *int64
}

func (s *Store) CreateCredential(ctx context.Context, c *Credential) error {
	var exp, rev any
	if c.ExpiresAt != nil {
		exp = c.ExpiresAt.Format(time.RFC3339Nano)
	}
	if c.RevokedAt != nil {
		rev = c.RevokedAt.Format(time.RFC3339Nano)
	}
	if c.Kind == "" {
		c.Kind = CredentialKindAPI
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO credentials (id, user_id, name, token_hash, token_prefix, scopes, expires_at, revoked_at, created_at, kind)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.UserID, c.Name, c.TokenHash, c.TokenPrefix, strings.Join(c.Scopes, ","), exp, rev, c.CreatedAt.Format(time.RFC3339Nano), c.Kind)
	return err
}

const credentialSelect = `
SELECT id, user_id, name, token_hash, token_prefix, scopes, expires_at, revoked_at, created_at,
 COALESCE(max_storage_bytes, -1), COALESCE(max_file_bytes, -1), COALESCE(max_files, -1),
 COALESCE(kind, 'api')
FROM credentials`

func (s *Store) ListCredentials(ctx context.Context, userID string) ([]Credential, error) {
	rows, err := s.db.QueryContext(ctx, credentialSelect+` WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) ListCredentialsByKind(ctx context.Context, userID, kind string) ([]Credential, error) {
	rows, err := s.db.QueryContext(ctx, credentialSelect+` WHERE user_id = ? AND kind = ? ORDER BY created_at DESC`, userID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	if out == nil {
		out = []Credential{}
	}
	return out, rows.Err()
}

func (s *Store) GetCredentialByTokenHash(ctx context.Context, hash string) (*Credential, error) {
	row := s.db.QueryRowContext(ctx, credentialSelect+` WHERE token_hash = ?`, hash)
	return scanCredential(row)
}

func (s *Store) GetCredentialByID(ctx context.Context, userID, id string) (*Credential, error) {
	row := s.db.QueryRowContext(ctx, credentialSelect+` WHERE user_id = ? AND id = ?`, userID, id)
	return scanCredential(row)
}

func (s *Store) SetCredentialQuotas(ctx context.Context, userID, id string, maxStorage, maxFile, maxFiles *int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE credentials SET max_storage_bytes = ?, max_file_bytes = ?, max_files = ? WHERE user_id = ? AND id = ?`,
		nullInt64(maxStorage), nullInt64(maxFile), nullInt64(maxFiles), userID, id)
	return err
}

func nullInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func (s *Store) RevokeCredential(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE credentials SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		now(), id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCredential(row scanner) (*Credential, error) {
	var c Credential
	var scopes string
	var exp, rev sql.NullString
	var created string
	var maxStor, maxFile, maxFiles int64
	if err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.TokenHash, &c.TokenPrefix, &scopes, &exp, &rev, &created,
		&maxStor, &maxFile, &maxFiles, &c.Kind); err != nil {
		return nil, err
	}
	if scopes != "" {
		c.Scopes = strings.Split(scopes, ",")
	}
	if exp.Valid {
		t, _ := time.Parse(time.RFC3339Nano, exp.String)
		c.ExpiresAt = &t
	}
	if rev.Valid {
		t, _ := time.Parse(time.RFC3339Nano, rev.String)
		c.RevokedAt = &t
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if maxStor >= 0 {
		c.MaxStorageBytes = &maxStor
	}
	if maxFile >= 0 {
		c.MaxFileBytes = &maxFile
	}
	if maxFiles >= 0 {
		c.MaxFiles = &maxFiles
	}
	return &c, nil
}

// --- Devices ---

type Device struct {
	ID              string
	UserID          string
	Name            string
	PublicKey       string
	DeviceTokenHash string
	Hostname        string
	OS              string
	Arch            string
	CPUs            int
	RAMMB           uint64
	AgentVersion    string
	Online          bool
	LastSeenAt      *time.Time
	RevokedAt       *time.Time
	CreatedAt       time.Time
}

func (s *Store) CreateDevice(ctx context.Context, d *Device) error {
	var last any
	if d.LastSeenAt != nil {
		last = d.LastSeenAt.Format(time.RFC3339Nano)
	}
	online := 0
	if d.Online {
		online = 1
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO devices (id, user_id, name, public_key, device_token_hash, hostname, os, arch, cpus, ram_mb, online, last_seen_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.UserID, d.Name, d.PublicKey, d.DeviceTokenHash, d.Hostname, d.OS, d.Arch, d.CPUs, d.RAMMB, online, last, d.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListDevices(ctx context.Context, userID string) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, user_id, name, public_key, device_token_hash, hostname, os, arch, cpus, ram_mb, COALESCE(agent_version,''), online, last_seen_at, revoked_at, created_at
FROM devices WHERE user_id = ? ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (s *Store) GetDevice(ctx context.Context, userID, id string) (*Device, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, user_id, name, public_key, device_token_hash, hostname, os, arch, cpus, ram_mb, COALESCE(agent_version,''), online, last_seen_at, revoked_at, created_at
FROM devices WHERE id = ? AND user_id = ?`, id, userID)
	return scanDevice(row)
}

func (s *Store) GetDeviceByID(ctx context.Context, id string) (*Device, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, user_id, name, public_key, device_token_hash, hostname, os, arch, cpus, ram_mb, COALESCE(agent_version,''), online, last_seen_at, revoked_at, created_at
FROM devices WHERE id = ?`, id)
	return scanDevice(row)
}

func (s *Store) GetDeviceByTokenHash(ctx context.Context, hash string) (*Device, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, user_id, name, public_key, device_token_hash, hostname, os, arch, cpus, ram_mb, COALESCE(agent_version,''), online, last_seen_at, revoked_at, created_at
FROM devices WHERE device_token_hash = ?`, hash)
	return scanDevice(row)
}

func (s *Store) UpdateDevicePresence(ctx context.Context, deviceID string, online bool, hostname, osName, arch string, cpus int, ramMB uint64, agentVersion string) error {
	on := 0
	if online {
		on = 1
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE devices SET online = ?, last_seen_at = ?, hostname = COALESCE(NULLIF(?, ''), hostname),
  os = COALESCE(NULLIF(?, ''), os), arch = COALESCE(NULLIF(?, ''), arch),
  cpus = CASE WHEN ? > 0 THEN ? ELSE cpus END,
  ram_mb = CASE WHEN ? > 0 THEN ? ELSE ram_mb END,
  agent_version = COALESCE(NULLIF(?, ''), agent_version)
WHERE id = ?`,
		on, now(), hostname, osName, arch, cpus, cpus, ramMB, ramMB, agentVersion, deviceID)
	return err
}

func (s *Store) MarkStaleDevicesOffline(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE devices SET online = 0
WHERE online = 1 AND (last_seen_at IS NULL OR last_seen_at < ?)`, olderThan.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanDevice(row scanner) (*Device, error) {
	var d Device
	var online int
	var last, revoked sql.NullString
	var created string
	if err := row.Scan(&d.ID, &d.UserID, &d.Name, &d.PublicKey, &d.DeviceTokenHash,
		&d.Hostname, &d.OS, &d.Arch, &d.CPUs, &d.RAMMB, &d.AgentVersion, &online, &last, &revoked, &created); err != nil {
		return nil, err
	}
	d.Online = online == 1
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if last.Valid {
		t, _ := time.Parse(time.RFC3339Nano, last.String)
		d.LastSeenAt = &t
	}
	if revoked.Valid {
		t, _ := time.Parse(time.RFC3339Nano, revoked.String)
		d.RevokedAt = &t
	}
	return &d, nil
}

func (s *Store) RevokeDevice(ctx context.Context, userID, deviceID string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE devices SET revoked_at = ?, online = 0, device_token_hash = ?
WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		now(), "revoked-"+NewID(), deviceID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	_, _ = s.db.ExecContext(ctx, `
UPDATE device_sessions SET revoked_at = ? WHERE device_id = ? AND revoked_at IS NULL`, now(), deviceID)
	return nil
}

// --- Registration tokens ---

type RegistrationToken struct {
	ID          string
	UserID      string
	TokenHash   string
	TokenPrefix string
	NameHint    string
	ExpiresAt   time.Time
	UsedAt      *time.Time
	CreatedAt   time.Time
}

func (s *Store) CreateRegistrationToken(ctx context.Context, t *RegistrationToken) error {
	var used any
	if t.UsedAt != nil {
		used = t.UsedAt.Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO registration_tokens (id, user_id, token_hash, token_prefix, name_hint, expires_at, used_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.TokenHash, t.TokenPrefix, t.NameHint,
		t.ExpiresAt.Format(time.RFC3339Nano), used, t.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetRegistrationTokenByHash(ctx context.Context, hash string) (*RegistrationToken, error) {
	var t RegistrationToken
	var used sql.NullString
	var exp, created string
	err := s.db.QueryRowContext(ctx, `
SELECT id, user_id, token_hash, token_prefix, name_hint, expires_at, used_at, created_at
FROM registration_tokens WHERE token_hash = ?`, hash).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &t.TokenPrefix, &t.NameHint, &exp, &used, &created)
	if err != nil {
		return nil, err
	}
	t.ExpiresAt, _ = time.Parse(time.RFC3339Nano, exp)
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if used.Valid {
		u, _ := time.Parse(time.RFC3339Nano, used.String)
		t.UsedAt = &u
	}
	return &t, nil
}

func (s *Store) MarkRegistrationTokenUsed(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE registration_tokens SET used_at = ? WHERE id = ? AND used_at IS NULL`, now(), id)
	return err
}

// --- Audit ---

const (
	ActorTypeUser      = "user"
	ActorTypeAISession = "ai_session"
	ActorTypeSystem    = "system"
)

type AuditEvent struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	ActorType   string    `json:"actor_type"`
	Actor       string    `json:"actor"`
	ActorID     string    `json:"actor_id"`
	ParentActor string    `json:"parent_actor,omitempty"`
	AISessionID string    `json:"ai_session_id,omitempty"`
	MCPClient   string    `json:"mcp_client,omitempty"`
	WorkflowID  string    `json:"workflow_id,omitempty"`
	PlanID      string    `json:"plan_id,omitempty"`
	TraceID     string    `json:"trace_id,omitempty"`
	Action      string    `json:"action"`
	Resource    string    `json:"resource"`
	Target      string    `json:"target,omitempty"`
	Detail      string    `json:"detail"`
	Result      string    `json:"result"`
	CreatedAt   time.Time `json:"created_at"`
}

func (e *AuditEvent) fillTarget() {
	if e != nil && e.Target == "" {
		e.Target = e.Resource
	}
}

func (s *Store) InsertAudit(ctx context.Context, e *AuditEvent) error {
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.ActorType == "" {
		e.ActorType = ActorTypeUser
	}
	e.fillTarget()
	var uid any
	if e.UserID != "" {
		uid = e.UserID
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO audit_events (id, user_id, actor, action, resource, detail, result, created_at,
  actor_type, actor_id, parent_actor, ai_session_id, mcp_client, workflow_id, plan_id, trace_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, uid, e.Actor, e.Action, e.Resource, e.Detail, e.Result, e.CreatedAt.Format(time.RFC3339Nano),
		e.ActorType, e.ActorID, e.ParentActor, e.AISessionID, e.MCPClient, e.WorkflowID, e.PlanID, e.TraceID)
	return err
}

func (s *Store) ListAudit(ctx context.Context, userID string, limit int) ([]AuditEvent, error) {
	return s.SearchAudit(ctx, AuditQuery{UserID: userID, Limit: limit})
}

func IsNotFound(err error) bool {
	return err == sql.ErrNoRows
}

func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}
