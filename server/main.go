package main

import (
	"archive/zip"
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type app struct {
	db          *sql.DB
	dataDir     string
	docsDir     string
	uploadDir   string
	versionsDir string
	jwtSecret   []byte
	pending     map[string]PendingLogin
	mu          sync.RWMutex
}

type User struct {
	ID                 int64  `json:"id"`
	Username           string `json:"username"`
	Nickname           string `json:"nickname"`
	Role               string `json:"role"`
	MustChangePassword bool   `json:"must_change_password"`
	MFAEnabled         bool   `json:"mfa_enabled"`
	MFABound           bool   `json:"mfa_bound"`
	TokenVersion       int    `json:"-"`
}

type UserRecord struct {
	User
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type PendingLogin struct {
	User         User
	Secret       string
	BindRequired bool
	ExpiresAt    time.Time
	FailedAt     []time.Time
}

type JWTClaims struct {
	Subject      int64  `json:"sub"`
	Username     string `json:"username"`
	TokenVersion int    `json:"token_version"`
	IssuedAt     int64  `json:"iat"`
	ExpiresAt    int64  `json:"exp"`
}

type Folder struct {
	ID        int64  `json:"id"`
	ParentID  int64  `json:"parent_id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

type Document struct {
	ID        int64  `json:"id"`
	FolderID  int64  `json:"folder_id"`
	Title     string `json:"title"`
	FilePath  string `json:"file_path"`
	SortOrder int    `json:"sort_order"`
	UpdatedAt string `json:"updated_at"`
}

type TrashItem struct {
	ID            int64  `json:"id"`
	Type          string `json:"type"`
	Title         string `json:"title"`
	ParentID      int64  `json:"parent_id"`
	DeletedAt     string `json:"deleted_at"`
	FolderCount   int    `json:"folder_count"`
	DocumentCount int    `json:"document_count"`
}

type TreeNode struct {
	Key       string     `json:"key"`
	ID        int64      `json:"id"`
	Type      string     `json:"type"`
	ParentID  int64      `json:"parent_id"`
	Title     string     `json:"title"`
	SortOrder int        `json:"sort_order"`
	Children  []TreeNode `json:"children,omitempty"`
}

type TreeSortNode struct {
	ID       int64          `json:"id"`
	Type     string         `json:"type"`
	Children []TreeSortNode `json:"children"`
}

func main() {
	dataDir := getenv("DOC_DATA_DIR", "../data")
	addr := getenv("DOC_ADDR", ":8080")

	a, err := newApp(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer a.db.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", a.handleLogin)
	mux.HandleFunc("/api/login/mfa", a.handleLoginMFA)
	mux.HandleFunc("/api/app-config", a.handleAppConfig)
	mux.HandleFunc("/api/setup/status", a.handleSetupStatus)
	mux.HandleFunc("/api/setup", a.handleSetup)
	mux.HandleFunc("/api/logout", a.withAuth(a.handleLogout))
	mux.HandleFunc("/api/me", a.withAuth(a.handleMe))
	mux.HandleFunc("/api/me/password", a.withAuth(a.handleMePassword))
	mux.HandleFunc("/api/settings", a.withAuth(a.handleSettings))
	mux.HandleFunc("/api/users", a.withAuth(a.handleUsers))
	mux.HandleFunc("/api/users/", a.withAuth(a.handleUserByID))
	mux.HandleFunc("/api/tree", a.withAuth(a.handleTree))
	mux.HandleFunc("/api/tree/sort", a.withAuth(a.handleTreeSort))
	mux.HandleFunc("/api/folders", a.withAuth(a.handleFolders))
	mux.HandleFunc("/api/folders/", a.withAuth(a.handleFolderByID))
	mux.HandleFunc("/api/documents", a.withAuth(a.handleDocuments))
	mux.HandleFunc("/api/documents/import", a.withAuth(a.handleDocumentImport))
	mux.HandleFunc("/api/documents/", a.withAuth(a.handleDocumentByID))
	mux.HandleFunc("/api/trash", a.withAuth(a.handleTrash))
	mux.HandleFunc("/api/trash/", a.withAuth(a.handleTrashByID))
	mux.HandleFunc("/api/uploads", a.withAuth(a.handleUploads))
	mux.HandleFunc("/api/search", a.withAuth(a.handleSearch))
	mux.HandleFunc("/api/operation-logs", a.withAuth(a.handleOperationLogs))
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(a.uploadDir))))
	mux.HandleFunc("/", serveStatic)

	log.Printf("doc-system listening on %s, data dir: %s", addr, dataDir)
	if err := http.ListenAndServe(addr, withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}

func newApp(dataDir string) (*app, error) {
	docsDir := filepath.Join(dataDir, "docs")
	uploadDir := filepath.Join(dataDir, "uploads")
	versionsDir := filepath.Join(dataDir, "versions")
	for _, dir := range []string{dataDir, docsDir, uploadDir, versionsDir, filepath.Join(dataDir, "backups")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "app.db"))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;"); err != nil {
		return nil, err
	}

	a := &app{
		db:          db,
		dataDir:     dataDir,
		docsDir:     docsDir,
		uploadDir:   uploadDir,
		versionsDir: versionsDir,
		pending:     map[string]PendingLogin{},
	}
	if err := a.migrate(); err != nil {
		return nil, err
	}
	jwtSecret, err := a.stringSetting("jwt_secret", "")
	if err != nil {
		return nil, err
	}
	a.jwtSecret = []byte(jwtSecret)
	if err := a.ensureInitialSetup(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *app) migrate() error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			nickname TEXT,
			role TEXT NOT NULL DEFAULT 'admin',
			token_version INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS folders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			parent_id INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			deleted_at DATETIME
		);`,
		`CREATE TABLE IF NOT EXISTS documents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			folder_id INTEGER NOT NULL DEFAULT 0,
			title TEXT NOT NULL,
			file_path TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_by INTEGER,
			updated_by INTEGER,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			deleted_at DATETIME
		);`,
		`CREATE TABLE IF NOT EXISTS attachments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			document_id INTEGER,
			original_name TEXT NOT NULL,
			file_path TEXT NOT NULL,
			mime_type TEXT,
			file_size INTEGER DEFAULT 0,
			created_by INTEGER,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS operation_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			action TEXT NOT NULL,
			target_type TEXT,
			target_id INTEGER,
			detail TEXT,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS document_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			document_id INTEGER NOT NULL,
			version_no INTEGER NOT NULL,
			title TEXT NOT NULL,
			file_path TEXT NOT NULL,
			content_bytes INTEGER NOT NULL DEFAULT 0,
			created_by INTEGER,
			created_at DATETIME NOT NULL,
			UNIQUE(document_id, version_no)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_document_versions_document_id
			ON document_versions(document_id, version_no DESC);`,
	}
	for _, stmt := range schema {
		if _, err := a.db.Exec(stmt); err != nil {
			return err
		}
	}
	columns := map[string]string{
		"must_change_password": "INTEGER NOT NULL DEFAULT 0",
		"mfa_enabled":          "INTEGER NOT NULL DEFAULT 0",
		"mfa_secret":           "TEXT",
		"mfa_bound_at":         "DATETIME",
		"token_version":        "INTEGER NOT NULL DEFAULT 1",
	}
	for name, definition := range columns {
		exists, err := a.columnExists("users", name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := a.db.Exec(fmt.Sprintf("ALTER TABLE users ADD COLUMN %s %s", name, definition)); err != nil {
				return err
			}
		}
	}
	if _, err := a.db.Exec(
		`INSERT OR IGNORE INTO app_settings (key, value, updated_at)
		 VALUES ('app_name', 'Doc System', datetime('now'))`,
	); err != nil {
		return err
	}
	if _, err := a.db.Exec(
		`INSERT OR IGNORE INTO app_settings (key, value, updated_at)
		 VALUES ('force_password_change_new_users', '0', datetime('now'))`,
	); err != nil {
		return err
	}
	if _, err := a.db.Exec(
		`INSERT OR IGNORE INTO app_settings (key, value, updated_at)
		 VALUES ('mfa_failed_window_seconds', '120', datetime('now'))`,
	); err != nil {
		return err
	}
	if _, err := a.db.Exec(
		`INSERT OR IGNORE INTO app_settings (key, value, updated_at)
		 VALUES ('mfa_failed_max_attempts', '5', datetime('now'))`,
	); err != nil {
		return err
	}
	if _, err := a.db.Exec(
		`INSERT OR IGNORE INTO app_settings (key, value, updated_at)
		 VALUES ('jwt_expire_days', '1', datetime('now'))`,
	); err != nil {
		return err
	}
	var jwtSecretCount int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM app_settings WHERE key = 'jwt_secret'`).Scan(&jwtSecretCount); err != nil {
		return err
	}
	if jwtSecretCount == 0 {
		secret, err := randomToken()
		if err != nil {
			return err
		}
		if _, err := a.db.Exec(
			`INSERT INTO app_settings (key, value, updated_at)
			 VALUES ('jwt_secret', ?, datetime('now'))`,
			secret,
		); err != nil {
			return err
		}
	}
	if _, err := a.db.Exec(
		`UPDATE users SET nickname = '超级管理员', updated_at = datetime('now')
		 WHERE username = 'admin' AND (nickname IS NULL OR nickname = '' OR nickname = '管理员')`,
	); err != nil {
		return err
	}
	return a.ensureOperationLogsIndex()
}

func (a *app) columnExists(table, column string) (bool, error) {
	rows, err := a.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	needed, err := a.setupNeeded()
	if err != nil {
		serverError(w, err)
		return
	}
	if needed {
		http.Error(w, "系统尚未完成首次安装，请先完成初始化向导", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	user, hash, err := a.findUser(req.Username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		a.logAuthLoginFailed(req.Username)
		http.Error(w, "用户名或密码错误", http.StatusUnauthorized)
		return
	}
	if user.MFAEnabled {
		a.startMFAChallenge(w, user)
		return
	}
	if err := a.issueJWT(w, user); err != nil {
		serverError(w, err)
		return
	}
	a.logAuthLogin(user)
	writeJSON(w, http.StatusOK, user)
}

func (a *app) handleLoginMFA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Token string `json:"token"`
		Code  string `json:"code"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	a.mu.RLock()
	pending, ok := a.pending[req.Token]
	a.mu.RUnlock()
	if !ok || time.Now().After(pending.ExpiresAt) {
		http.Error(w, "MFA 验证已过期，请重新登录", http.StatusUnauthorized)
		return
	}
	secret := pending.Secret
	if !verifyTOTP(secret, req.Code, time.Now()) {
		windowSeconds, err := a.intSetting("mfa_failed_window_seconds", 120)
		if err != nil {
			serverError(w, err)
			return
		}
		maxAttempts, err := a.intSetting("mfa_failed_max_attempts", 5)
		if err != nil {
			serverError(w, err)
			return
		}
		now := time.Now()
		cutoff := now.Add(-time.Duration(windowSeconds) * time.Second)
		a.mu.Lock()
		pending, ok = a.pending[req.Token]
		if !ok || now.After(pending.ExpiresAt) {
			a.mu.Unlock()
			http.Error(w, "MFA 验证已过期，请重新登录", http.StatusUnauthorized)
			return
		}
		recentFailures := make([]time.Time, 0, len(pending.FailedAt)+1)
		for _, failedAt := range pending.FailedAt {
			if failedAt.After(cutoff) {
				recentFailures = append(recentFailures, failedAt)
			}
		}
		recentFailures = append(recentFailures, now)
		pending.FailedAt = recentFailures
		if len(recentFailures) >= maxAttempts {
			delete(a.pending, req.Token)
			a.mu.Unlock()
			http.Error(w, "MFA 验证失败次数过多，请重新输入账号密码", http.StatusTooManyRequests)
			return
		}
		a.pending[req.Token] = pending
		a.mu.Unlock()
		http.Error(w, fmt.Sprintf("MFA 验证码不正确，还可尝试 %d 次", maxAttempts-len(recentFailures)), http.StatusUnauthorized)
		return
	}
	if pending.BindRequired {
		if _, err := a.db.Exec(
			`UPDATE users SET mfa_secret = ?, mfa_bound_at = datetime('now'), updated_at = datetime('now') WHERE id = ?`,
			secret, pending.User.ID,
		); err != nil {
			serverError(w, err)
			return
		}
	}
	latestUser, err := a.getUserByID(pending.User.ID)
	if err != nil || latestUser.TokenVersion != pending.User.TokenVersion {
		a.mu.Lock()
		delete(a.pending, req.Token)
		a.mu.Unlock()
		http.Error(w, "登录状态已变化，请重新输入账号密码", http.StatusUnauthorized)
		return
	}
	a.mu.Lock()
	delete(a.pending, req.Token)
	a.mu.Unlock()
	if err := a.issueJWT(w, latestUser); err != nil {
		serverError(w, err)
		return
	}
	a.logAuthLogin(latestUser)
	writeJSON(w, http.StatusOK, latestUser)
}

func (a *app) startMFAChallenge(w http.ResponseWriter, user User) {
	secret, err := a.mfaSecret(user.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	bindRequired := secret == ""
	if bindRequired {
		secret, err = randomTOTPSecret()
		if err != nil {
			serverError(w, err)
			return
		}
	}
	token, err := randomToken()
	if err != nil {
		serverError(w, err)
		return
	}
	pending := PendingLogin{
		User:         user,
		Secret:       secret,
		BindRequired: bindRequired,
		ExpiresAt:    time.Now().Add(5 * time.Minute),
	}
	a.mu.Lock()
	for key, item := range a.pending {
		if time.Now().After(item.ExpiresAt) {
			delete(a.pending, key)
		}
	}
	a.pending[token] = pending
	a.mu.Unlock()

	payload := map[string]any{
		"mfa_required":  true,
		"mfa_bound":     !bindRequired,
		"mfa_token":     token,
		"username":      user.Username,
		"expires_in":    300,
		"must_bind_mfa": bindRequired,
	}
	if bindRequired {
		otpauth := totpURL(user.Username, secret)
		qr, err := qrcode.Encode(otpauth, qrcode.Medium, 260)
		if err != nil {
			serverError(w, err)
			return
		}
		payload["manual_key"] = secret
		payload["qr_data_url"] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(qr)
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *app) jwtExpireDays() (int, error) {
	days, err := a.intSetting("jwt_expire_days", 1)
	if err != nil {
		return 0, err
	}
	if days < 1 {
		return 1, nil
	}
	if days > 90 {
		return 90, nil
	}
	return days, nil
}

func (a *app) signJWT(user User) (string, error) {
	now := time.Now()
	expireDays, err := a.jwtExpireDays()
	if err != nil {
		return "", err
	}
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := JWTClaims{
		Subject:      user.ID,
		Username:     user.Username,
		TokenVersion: user.TokenVersion,
		IssuedAt:     now.Unix(),
		ExpiresAt:    now.Add(time.Duration(expireDays) * 24 * time.Hour).Unix(),
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(headerJSON)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	unsigned := encodedHeader + "." + encodedClaims
	signature := signJWTPart(unsigned, a.jwtSecret)
	return unsigned + "." + signature, nil
}

func (a *app) parseJWT(token string) (JWTClaims, error) {
	var claims JWTClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, errors.New("invalid token")
	}
	unsigned := parts[0] + "." + parts[1]
	expected := signJWTPart(unsigned, a.jwtSecret)
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return claims, errors.New("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, err
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, err
	}
	if claims.Subject <= 0 || claims.ExpiresAt <= time.Now().Unix() {
		return claims, errors.New("token expired")
	}
	return claims, nil
}

func signJWTPart(unsigned string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *app) issueJWT(w http.ResponseWriter, user User) error {
	token, err := a.signJWT(user)
	if err != nil {
		return err
	}
	expireDays, err := a.jwtExpireDays()
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "doc_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   expireDays * 86400,
	})
	return nil
}

func clearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "doc_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "doc_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	a.addOperationLog(user.ID, "auth.logout", "user", user.ID, fmt.Sprintf("用户 %s 退出登录", user.Username))
	clearAuthCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleMe(w http.ResponseWriter, r *http.Request, user User) {
	writeJSON(w, http.StatusOK, user)
}

func (a *app) handleAppConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	appName, err := a.stringSetting("app_name", "Doc System")
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"app_name": appName})
}

func (a *app) handleMePassword(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if errMsg := validateSelfPassword(req.NewPassword); errMsg != "" {
		badRequest(w, errMsg)
		return
	}
	_, hash, err := a.findUser(user.Username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.CurrentPassword)) != nil {
		http.Error(w, "当前密码不正确", http.StatusUnauthorized)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.NewPassword)) == nil {
		badRequest(w, "新密码不能和当前密码一致")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		serverError(w, err)
		return
	}
	if _, err := a.db.Exec(
		`UPDATE users SET password_hash = ?, must_change_password = 0, updated_at = datetime('now') WHERE id = ?`,
		string(newHash), user.ID,
	); err != nil {
		serverError(w, err)
		return
	}
	a.deleteUserSessions(user.ID)
	clearAuthCookie(w)
	a.addOperationLog(user.ID, "auth.password_change", "user", user.ID, fmt.Sprintf("用户 %s 修改了密码", user.Username))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleSettings(w http.ResponseWriter, r *http.Request, user User) {
	if !isSuperAdmin(user) {
		http.Error(w, "无权访问系统设置", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		appName, err := a.stringSetting("app_name", "Doc System")
		if err != nil {
			serverError(w, err)
			return
		}
		force, err := a.boolSetting("force_password_change_new_users")
		if err != nil {
			serverError(w, err)
			return
		}
		window, err := a.intSetting("mfa_failed_window_seconds", 120)
		if err != nil {
			serverError(w, err)
			return
		}
		maxAttempts, err := a.intSetting("mfa_failed_max_attempts", 5)
		if err != nil {
			serverError(w, err)
			return
		}
		jwtExpireDays, err := a.jwtExpireDays()
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"app_name":                        appName,
			"force_password_change_new_users": force,
			"mfa_failed_window_seconds":       window,
			"mfa_failed_max_attempts":         maxAttempts,
			"jwt_expire_days":                 jwtExpireDays,
		})
	case http.MethodPut:
		var req struct {
			AppName                     *string `json:"app_name"`
			ForcePasswordChangeNewUsers *bool   `json:"force_password_change_new_users"`
			MFAFailedWindowSeconds      *int    `json:"mfa_failed_window_seconds"`
			MFAFailedMaxAttempts        *int    `json:"mfa_failed_max_attempts"`
			JWTExpireDays               *int    `json:"jwt_expire_days"`
		}
		if err := readJSON(r, &req); err != nil {
			badRequest(w, err.Error())
			return
		}
		if req.AppName != nil {
			appName := strings.TrimSpace(*req.AppName)
			if appName == "" {
				badRequest(w, "项目名称不能为空")
				return
			}
			if utf8.RuneCountInString(appName) > 40 {
				badRequest(w, "项目名称不能超过 40 个字符")
				return
			}
			if err := a.setStringSetting("app_name", appName); err != nil {
				serverError(w, err)
				return
			}
		}
		if req.ForcePasswordChangeNewUsers != nil {
			if err := a.setBoolSetting("force_password_change_new_users", *req.ForcePasswordChangeNewUsers); err != nil {
				serverError(w, err)
				return
			}
		}
		if req.MFAFailedWindowSeconds != nil {
			if *req.MFAFailedWindowSeconds < 30 || *req.MFAFailedWindowSeconds > 3600 {
				badRequest(w, "MFA 失败统计窗口需在 30 到 3600 秒之间")
				return
			}
			if err := a.setIntSetting("mfa_failed_window_seconds", *req.MFAFailedWindowSeconds); err != nil {
				serverError(w, err)
				return
			}
		}
		if req.MFAFailedMaxAttempts != nil {
			if *req.MFAFailedMaxAttempts < 1 || *req.MFAFailedMaxAttempts > 20 {
				badRequest(w, "MFA 失败次数需在 1 到 20 次之间")
				return
			}
			if err := a.setIntSetting("mfa_failed_max_attempts", *req.MFAFailedMaxAttempts); err != nil {
				serverError(w, err)
				return
			}
		}
		if req.JWTExpireDays != nil {
			if *req.JWTExpireDays < 1 || *req.JWTExpireDays > 90 {
				badRequest(w, "JWT 有效期需在 1 到 90 天之间")
				return
			}
			if err := a.setIntSetting("jwt_expire_days", *req.JWTExpireDays); err != nil {
				serverError(w, err)
				return
			}
		}
		a.addOperationLog(user.ID, "settings.update", "settings", 0, "更新了项目配置")
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (a *app) handleUsers(w http.ResponseWriter, r *http.Request, user User) {
	if !isAdmin(user) {
		http.Error(w, "无权访问用户管理", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("page") != "" || r.URL.Query().Get("page_size") != "" {
			page, pageSize, err := parsePagination(r)
			if err != nil {
				badRequest(w, err.Error())
				return
			}
			username := strings.TrimSpace(r.URL.Query().Get("username"))
			total, err := a.countUsers(username)
			if err != nil {
				serverError(w, err)
				return
			}
			users, err := a.listUsersPage(username, pageSize, (page-1)*pageSize)
			if err != nil {
				serverError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"items":     users,
				"total":     total,
				"page":      page,
				"page_size": pageSize,
			})
			return
		}
		users, err := a.listUsers()
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, users)
	case http.MethodPost:
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Nickname string `json:"nickname"`
			Role     string `json:"role"`
		}
		if err := readJSON(r, &req); err != nil {
			badRequest(w, err.Error())
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		req.Nickname = strings.TrimSpace(req.Nickname)
		req.Role = strings.TrimSpace(req.Role)
		if req.Username == "" {
			badRequest(w, "用户名不能为空")
			return
		}
		if len(req.Password) < 6 {
			badRequest(w, "密码至少 6 位")
			return
		}
		if !validRole(req.Role) {
			badRequest(w, "角色无效")
			return
		}
		mustChange, err := a.boolSetting("force_password_change_new_users")
		if err != nil {
			serverError(w, err)
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			serverError(w, err)
			return
		}
		res, err := a.db.Exec(
			`INSERT INTO users (username, password_hash, nickname, role, must_change_password, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
			req.Username, string(hash), req.Nickname, req.Role, boolInt(mustChange),
		)
		if err != nil {
			badRequest(w, "用户名已存在")
			return
		}
		id, _ := res.LastInsertId()
		a.addOperationLog(user.ID, "user.create", "user", id, fmt.Sprintf("创建用户 %s（角色 %s）", req.Username, req.Role))
		writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
	default:
		methodNotAllowed(w)
	}
}

func (a *app) handleUserByID(w http.ResponseWriter, r *http.Request, user User) {
	if !isAdmin(user) {
		http.Error(w, "无权访问用户管理", http.StatusForbidden)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/users/")
	path = strings.Trim(path, "/")
	if path == "" {
		notFound(w)
		return
	}
	parts := strings.Split(path, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		notFound(w)
		return
	}
	if len(parts) == 2 && parts[1] == "password" {
		a.handleUserPassword(w, r, user, id)
		return
	}
	if len(parts) == 3 && parts[1] == "mfa" && parts[2] == "reset" {
		a.handleUserMFAReset(w, r, user, id)
		return
	}
	if len(parts) != 1 {
		notFound(w)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req struct {
			Nickname   string `json:"nickname"`
			Role       string `json:"role"`
			MFAEnabled *bool  `json:"mfa_enabled"`
		}
		if err := readJSON(r, &req); err != nil {
			badRequest(w, err.Error())
			return
		}
		req.Nickname = strings.TrimSpace(req.Nickname)
		req.Role = strings.TrimSpace(req.Role)
		if !validRole(req.Role) {
			badRequest(w, "角色无效")
			return
		}
		target, err := a.getUserByID(id)
		if err != nil {
			notFound(w)
			return
		}
		if target.Username == "admin" && req.Role != "admin" {
			badRequest(w, "admin 用户不允许修改角色")
			return
		}
		if id == user.ID && req.Role != "admin" {
			badRequest(w, "不能移除自己的管理员角色")
			return
		}
		if req.Role != "admin" {
			ok, err := a.hasAnotherAdmin(id)
			if err != nil {
				serverError(w, err)
				return
			}
			if !ok {
				badRequest(w, "至少保留一个管理员")
				return
			}
		}
		res, err := a.db.Exec(
			`UPDATE users SET nickname = ?, role = ?, updated_at = datetime('now') WHERE id = ?`,
			req.Nickname, req.Role, id,
		)
		if err != nil {
			serverError(w, err)
			return
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			notFound(w)
			return
		}
		if req.MFAEnabled != nil {
			if *req.MFAEnabled {
				if _, err := a.db.Exec(
					`UPDATE users SET mfa_enabled = 1, updated_at = datetime('now') WHERE id = ?`,
					id,
				); err != nil {
					serverError(w, err)
					return
				}
			} else {
				if _, err := a.db.Exec(
					`UPDATE users SET mfa_enabled = 0, mfa_secret = NULL, mfa_bound_at = NULL, updated_at = datetime('now') WHERE id = ?`,
					id,
				); err != nil {
					serverError(w, err)
					return
				}
			}
		}
		a.refreshUserSessions(id)
		a.addOperationLog(user.ID, "user.update", "user", id, fmt.Sprintf("更新用户 %s（角色 %s）", target.Username, req.Role))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodDelete:
		if id == user.ID {
			badRequest(w, "不能删除当前登录用户")
			return
		}
		ok, err := a.hasAnotherAdmin(id)
		if err != nil {
			serverError(w, err)
			return
		}
		if !ok {
			badRequest(w, "至少保留一个管理员")
			return
		}
		target, err := a.getUserByID(id)
		if err != nil {
			notFound(w)
			return
		}
		res, err := a.db.Exec(`DELETE FROM users WHERE id = ?`, id)
		if err != nil {
			serverError(w, err)
			return
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			notFound(w)
			return
		}
		a.deleteUserSessions(id)
		a.addOperationLog(user.ID, "user.delete", "user", id, fmt.Sprintf("删除用户 %s", target.Username))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (a *app) handleUserMFAReset(w http.ResponseWriter, r *http.Request, actor User, id int64) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	target, err := a.getUserByID(id)
	if err != nil {
		notFound(w)
		return
	}
	res, err := a.db.Exec(
		`UPDATE users SET mfa_secret = NULL, mfa_bound_at = NULL, updated_at = datetime('now') WHERE id = ?`,
		id,
	)
	if err != nil {
		serverError(w, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		notFound(w)
		return
	}
	a.deleteUserSessions(id)
	a.addOperationLog(actor.ID, "user.reset_mfa", "user", id, fmt.Sprintf("重置用户 %s 的 MFA", target.Username))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleUserPassword(w http.ResponseWriter, r *http.Request, actor User, id int64) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	target, err := a.getUserByID(id)
	if err != nil {
		notFound(w)
		return
	}
	if target.Username == "admin" {
		badRequest(w, "admin 用户不允许重置密码")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if len(req.Password) < 6 {
		badRequest(w, "密码至少 6 位")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		serverError(w, err)
		return
	}
	res, err := a.db.Exec(
		`UPDATE users SET password_hash = ?, must_change_password = 1, updated_at = datetime('now') WHERE id = ?`,
		string(hash), id,
	)
	if err != nil {
		serverError(w, err)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		notFound(w)
		return
	}
	a.deleteUserSessions(id)
	a.addOperationLog(actor.ID, "user.reset_password", "user", id, fmt.Sprintf("重置用户 %s 的密码", target.Username))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleTree(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	folders, err := a.listFolders()
	if err != nil {
		serverError(w, err)
		return
	}
	documents, err := a.listDocuments()
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, buildTree(folders, documents))
}

func (a *app) handleTreeSort(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	if !canWrite(user) {
		http.Error(w, "只读用户不能调整目录", http.StatusForbidden)
		return
	}
	var req struct {
		Tree []TreeSortNode `json:"tree"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if len(req.Tree) != 1 || req.Tree[0].Type != "root" {
		badRequest(w, "目录树数据无效")
		return
	}
	updates, err := flattenTreeSort(req.Tree[0].Children, 0, map[int64]bool{})
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		serverError(w, err)
		return
	}
	for _, update := range updates {
		switch update.Type {
		case "folder":
			_, err = tx.Exec(
				`UPDATE folders SET parent_id = ?, sort_order = ?, updated_at = datetime('now') WHERE id = ? AND deleted_at IS NULL`,
				update.ParentID, update.SortOrder, update.ID,
			)
		case "document":
			_, err = tx.Exec(
				`UPDATE documents SET folder_id = ?, sort_order = ?, updated_at = datetime('now') WHERE id = ? AND deleted_at IS NULL`,
				update.ParentID, update.SortOrder, update.ID,
			)
		default:
			err = fmt.Errorf("不支持的节点类型: %s", update.Type)
		}
		if err != nil {
			_ = tx.Rollback()
			serverError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) handleFolders(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !canWrite(user) {
		http.Error(w, "只读用户不能创建文件夹", http.StatusForbidden)
		return
	}
	var req struct {
		ParentID int64  `json:"parent_id"`
		Name     string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		badRequest(w, "文件夹名称不能为空")
		return
	}
	res, err := a.db.Exec(
		`INSERT INTO folders (parent_id, name, sort_order, created_at, updated_at)
		 VALUES (?, ?, 0, datetime('now'), datetime('now'))`,
		req.ParentID, req.Name,
	)
	if err != nil {
		serverError(w, err)
		return
	}
	id, _ := res.LastInsertId()
	a.addOperationLog(user.ID, "folder.create", "folder", id, fmt.Sprintf("创建文件夹“%s”", req.Name))
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (a *app) handleFolderByID(w http.ResponseWriter, r *http.Request, user User) {
	id, err := parseID(r.URL.Path, "/api/folders/")
	if err != nil {
		notFound(w)
		return
	}
	switch r.Method {
	case http.MethodPut:
		if !canWrite(user) {
			http.Error(w, "只读用户不能修改文件夹", http.StatusForbidden)
			return
		}
		var req struct {
			ParentID *int64  `json:"parent_id"`
			Name     *string `json:"name"`
		}
		if err := readJSON(r, &req); err != nil {
			badRequest(w, err.Error())
			return
		}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				badRequest(w, "文件夹名称不能为空")
				return
			}
			if _, err := a.db.Exec(`UPDATE folders SET name = ?, updated_at = datetime('now') WHERE id = ?`, name, id); err != nil {
				serverError(w, err)
				return
			}
			a.addOperationLog(user.ID, "folder.update", "folder", id, fmt.Sprintf("重命名文件夹为“%s”", name))
		}
		if req.ParentID != nil {
			if _, err := a.db.Exec(`UPDATE folders SET parent_id = ?, updated_at = datetime('now') WHERE id = ?`, *req.ParentID, id); err != nil {
				serverError(w, err)
				return
			}
			a.addOperationLog(user.ID, "folder.update", "folder", id, fmt.Sprintf("移动文件夹到父级 %d", *req.ParentID))
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodDelete:
		if !canWrite(user) {
			http.Error(w, "只读用户不能删除文件夹", http.StatusForbidden)
			return
		}
		folder, err := a.getFolder(id, false)
		if err != nil {
			notFound(w)
			return
		}
		if err := a.softDeleteFolderTree(id); err != nil {
			serverError(w, err)
			return
		}
		a.addOperationLog(user.ID, "folder.delete", "folder", id, fmt.Sprintf("文件夹“%s”已移入回收站", folder.Name))
		writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("文件夹“%s”已移入回收站", folder.Name)})
	default:
		methodNotAllowed(w)
	}
}

func (a *app) handleDocuments(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !canWrite(user) {
		http.Error(w, "只读用户不能创建文档", http.StatusForbidden)
		return
	}
	var req struct {
		FolderID int64  `json:"folder_id"`
		Title    string `json:"title"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		badRequest(w, "文档标题不能为空")
		return
	}
	res, err := a.db.Exec(
		`INSERT INTO documents (folder_id, title, file_path, sort_order, created_by, updated_by, created_at, updated_at)
		 VALUES (?, ?, '', 0, ?, ?, datetime('now'), datetime('now'))`,
		req.FolderID, req.Title, user.ID, user.ID,
	)
	if err != nil {
		serverError(w, err)
		return
	}
	id, _ := res.LastInsertId()
	filePath := fmt.Sprintf("doc_%d.md", id)
	if err := os.WriteFile(filepath.Join(a.docsDir, filePath), []byte("# "+req.Title+"\n"), 0644); err != nil {
		serverError(w, err)
		return
	}
	if _, err := a.db.Exec(`UPDATE documents SET file_path = ? WHERE id = ?`, filePath, id); err != nil {
		serverError(w, err)
		return
	}
	a.addOperationLog(user.ID, "document.create", "document", id, fmt.Sprintf("创建文档“%s”", req.Title))
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (a *app) handleDocumentImport(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !canWrite(user) {
		http.Error(w, "只读用户不能导入文档", http.StatusForbidden)
		return
	}
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		badRequest(w, "导入文件不能超过 20MB")
		return
	}
	folderID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("folder_id")), 10, 64)
	if err != nil || folderID < 0 {
		badRequest(w, "目标文件夹无效")
		return
	}
	if folderID != 0 {
		if _, err := a.getFolder(folderID, false); err != nil {
			badRequest(w, "目标文件夹不存在或已被删除")
			return
		}
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		badRequest(w, "请选择要导入的文件")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 20<<20+1))
	if err != nil {
		serverError(w, err)
		return
	}
	if len(data) > 20<<20 {
		badRequest(w, "导入文件不能超过 20MB")
		return
	}
	title := documentTitleFromFilename(header.Filename)
	content, err := a.convertImportToMarkdown(header.Filename, data, user.ID)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	id, err := a.createDocumentWithContent(folderID, title, content, user.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":      id,
		"title":   title,
		"message": fmt.Sprintf("文件“%s”已导入为 Markdown 文档", header.Filename),
	})
	a.addOperationLog(user.ID, "document.import", "document", id, fmt.Sprintf("导入文件“%s”为文档“%s”", header.Filename, title))
}

func (a *app) handleDocumentByID(w http.ResponseWriter, r *http.Request, user User) {
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/documents/"), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] == "" {
		notFound(w)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		notFound(w)
		return
	}
	if len(parts) >= 2 && parts[1] == "versions" {
		a.handleDocumentVersions(w, r, user, id, parts)
		return
	}
	if len(parts) != 1 {
		notFound(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		doc, err := a.getDocument(id)
		if err != nil {
			notFound(w)
			return
		}
		content, err := os.ReadFile(filepath.Join(a.docsDir, doc.FilePath))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			serverError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         doc.ID,
			"folder_id":  doc.FolderID,
			"title":      doc.Title,
			"content":    string(content),
			"updated_at": doc.UpdatedAt,
		})
	case http.MethodPut:
		if !canWrite(user) {
			http.Error(w, "只读用户不能保存文档", http.StatusForbidden)
			return
		}
		var req struct {
			Title    string `json:"title"`
			Content  string `json:"content"`
			FolderID *int64 `json:"folder_id"`
		}
		if err := readJSON(r, &req); err != nil {
			badRequest(w, err.Error())
			return
		}
		doc, err := a.getDocument(id)
		if err != nil {
			notFound(w)
			return
		}
		title := strings.TrimSpace(req.Title)
		if title == "" {
			badRequest(w, "文档标题不能为空")
			return
		}
		oldBytes, err := os.ReadFile(filepath.Join(a.docsDir, doc.FilePath))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			serverError(w, err)
			return
		}
		oldContent := string(oldBytes)
		if shouldSnapshotDocument(doc.Title, title, oldContent, req.Content) {
			if err := a.createDocumentVersion(doc, oldContent, user.ID); err != nil {
				serverError(w, err)
				return
			}
		}
		if err := os.WriteFile(filepath.Join(a.docsDir, doc.FilePath), []byte(req.Content), 0644); err != nil {
			serverError(w, err)
			return
		}
		if req.FolderID != nil {
			_, err = a.db.Exec(
				`UPDATE documents SET title = ?, folder_id = ?, updated_by = ?, updated_at = datetime('now') WHERE id = ?`,
				title, *req.FolderID, user.ID, id,
			)
		} else {
			_, err = a.db.Exec(
				`UPDATE documents SET title = ?, updated_by = ?, updated_at = datetime('now') WHERE id = ?`,
				title, user.ID, id,
			)
		}
		if err != nil {
			serverError(w, err)
			return
		}
		a.addOperationLog(user.ID, "document.update", "document", id, fmt.Sprintf("保存文档“%s”", title))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodDelete:
		if !canWrite(user) {
			http.Error(w, "只读用户不能删除文档", http.StatusForbidden)
			return
		}
		doc, err := a.getDocument(id)
		if err != nil {
			notFound(w)
			return
		}
		if _, err := a.db.Exec(`UPDATE documents SET deleted_at = datetime('now'), updated_at = datetime('now') WHERE id = ? AND deleted_at IS NULL`, id); err != nil {
			serverError(w, err)
			return
		}
		a.addOperationLog(user.ID, "document.delete", "document", id, fmt.Sprintf("文档“%s”已移入回收站", doc.Title))
		writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("文档“%s”已移入回收站", doc.Title)})
	default:
		methodNotAllowed(w)
	}
}

func (a *app) handleTrash(w http.ResponseWriter, r *http.Request, user User) {
	if !canWrite(user) {
		http.Error(w, "只读用户不能访问回收站", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	page, pageSize, err := parsePagination(r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	items, err := a.listTrashItems()
	if err != nil {
		serverError(w, err)
		return
	}
	total := len(items)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items[start:end],
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (a *app) handleTrashByID(w http.ResponseWriter, r *http.Request, user User) {
	if !canWrite(user) {
		http.Error(w, "只读用户不能操作回收站", http.StatusForbidden)
		return
	}
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/trash/"), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 || len(parts) > 3 {
		notFound(w)
		return
	}
	itemType := parts[0]
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		notFound(w)
		return
	}
	if len(parts) == 3 {
		if parts[2] != "restore" || r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		message, err := a.restoreTrashItem(itemType, id)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		action := "document.restore"
		if itemType == "folder" {
			action = "folder.restore"
		}
		a.addOperationLog(user.ID, action, itemType, id, message)
		writeJSON(w, http.StatusOK, map[string]string{"message": message})
		return
	}
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	message, err := a.purgeTrashItem(itemType, id)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	action := "document.purge"
	if itemType == "folder" {
		action = "folder.purge"
	}
	a.addOperationLog(user.ID, action, itemType, id, message)
	writeJSON(w, http.StatusOK, map[string]string{"message": message})
}

func (a *app) handleUploads(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !canWrite(user) {
		http.Error(w, "只读用户不能上传附件", http.StatusForbidden)
		return
	}
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		badRequest(w, "上传文件不能超过 20MB")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		badRequest(w, "缺少上传文件")
		return
	}
	defer file.Close()

	now := time.Now()
	dir := filepath.Join(a.uploadDir, now.Format("2006"), now.Format("01"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		serverError(w, err)
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		if extensions, err := mime.ExtensionsByType(header.Header.Get("Content-Type")); err == nil && len(extensions) > 0 {
			ext = extensions[0]
		}
	}
	name := now.Format("20060102150405") + "_" + mustToken(4) + ext
	target := filepath.Join(dir, name)
	out, err := os.Create(target)
	if err != nil {
		serverError(w, err)
		return
	}
	size, err := io.Copy(out, file)
	closeErr := out.Close()
	if err != nil {
		serverError(w, err)
		return
	}
	if closeErr != nil {
		serverError(w, closeErr)
		return
	}
	rel := filepath.ToSlash(filepath.Join(now.Format("2006"), now.Format("01"), name))
	_, _ = a.db.Exec(
		`INSERT INTO attachments (original_name, file_path, mime_type, file_size, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, datetime('now'))`,
		header.Filename, rel, header.Header.Get("Content-Type"), size, user.ID,
	)
	a.addOperationLog(user.ID, "upload.create", "attachment", 0, fmt.Sprintf("上传附件“%s”", header.Filename))
	writeJSON(w, http.StatusCreated, map[string]string{
		"url":  "/uploads/" + rel,
		"name": header.Filename,
	})
}

func (a *app) handleSearch(w http.ResponseWriter, r *http.Request, user User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []Document{})
		return
	}
	rows, err := a.db.Query(
		`SELECT id, folder_id, title, file_path, sort_order, updated_at
		 FROM documents
		 WHERE deleted_at IS NULL AND title LIKE ?
		 ORDER BY updated_at DESC LIMIT 30`,
		"%"+q+"%",
	)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	docs, err := scanDocuments(rows)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

func (a *app) findUser(username string) (User, string, error) {
	var user User
	var hash string
	var mustChange, mfaEnabled, mfaBound, tokenVersion int
	err := a.db.QueryRow(
		`SELECT id, username, password_hash, COALESCE(nickname, ''), role,
		        must_change_password, mfa_enabled, CASE WHEN mfa_secret IS NULL OR mfa_secret = '' THEN 0 ELSE 1 END,
		        token_version
		 FROM users WHERE username = ?`,
		username,
	).Scan(&user.ID, &user.Username, &hash, &user.Nickname, &user.Role, &mustChange, &mfaEnabled, &mfaBound, &tokenVersion)
	user.MustChangePassword = mustChange == 1
	user.MFAEnabled = mfaEnabled == 1
	user.MFABound = mfaBound == 1
	user.TokenVersion = tokenVersion
	return user, hash, err
}

func (a *app) getUserByID(id int64) (User, error) {
	var user User
	var mustChange, mfaEnabled, mfaBound, tokenVersion int
	err := a.db.QueryRow(
		`SELECT id, username, COALESCE(nickname, ''), role,
		        must_change_password, mfa_enabled, CASE WHEN mfa_secret IS NULL OR mfa_secret = '' THEN 0 ELSE 1 END,
		        token_version
		 FROM users WHERE id = ?`,
		id,
	).Scan(&user.ID, &user.Username, &user.Nickname, &user.Role, &mustChange, &mfaEnabled, &mfaBound, &tokenVersion)
	user.MustChangePassword = mustChange == 1
	user.MFAEnabled = mfaEnabled == 1
	user.MFABound = mfaBound == 1
	user.TokenVersion = tokenVersion
	return user, err
}

func (a *app) listUsers() ([]UserRecord, error) {
	rows, err := a.db.Query(
		`SELECT id, username, COALESCE(nickname, ''), role,
		        must_change_password, mfa_enabled, CASE WHEN mfa_secret IS NULL OR mfa_secret = '' THEN 0 ELSE 1 END,
		        created_at, updated_at
		 FROM users ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]UserRecord, 0)
	for rows.Next() {
		var user UserRecord
		var mustChange, mfaEnabled, mfaBound int
		if err := rows.Scan(
			&user.ID, &user.Username, &user.Nickname, &user.Role,
			&mustChange, &mfaEnabled, &mfaBound, &user.CreatedAt, &user.UpdatedAt,
		); err != nil {
			return nil, err
		}
		user.MustChangePassword = mustChange == 1
		user.MFAEnabled = mfaEnabled == 1
		user.MFABound = mfaBound == 1
		users = append(users, user)
	}
	return users, rows.Err()
}

func (a *app) countUsers(username string) (int, error) {
	var total int
	username = strings.TrimSpace(username)
	if username == "" {
		err := a.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total)
		return total, err
	}
	err := a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username LIKE ?`, "%"+username+"%").Scan(&total)
	return total, err
}

func (a *app) listUsersPage(username string, limit int, offset int) ([]UserRecord, error) {
	username = strings.TrimSpace(username)
	query := `SELECT id, username, COALESCE(nickname, ''), role,
	         must_change_password, mfa_enabled, CASE WHEN mfa_secret IS NULL OR mfa_secret = '' THEN 0 ELSE 1 END,
	         created_at, updated_at
	  FROM users`
	args := []any{}
	if username != "" {
		query += ` WHERE username LIKE ?`
		args = append(args, "%"+username+"%")
	}
	query += ` ORDER BY id ASC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]UserRecord, 0)
	for rows.Next() {
		var user UserRecord
		var mustChange, mfaEnabled, mfaBound int
		if err := rows.Scan(
			&user.ID, &user.Username, &user.Nickname, &user.Role,
			&mustChange, &mfaEnabled, &mfaBound, &user.CreatedAt, &user.UpdatedAt,
		); err != nil {
			return nil, err
		}
		user.MustChangePassword = mustChange == 1
		user.MFAEnabled = mfaEnabled == 1
		user.MFABound = mfaBound == 1
		users = append(users, user)
	}
	return users, rows.Err()
}

func (a *app) mfaSecret(userID int64) (string, error) {
	var secret sql.NullString
	err := a.db.QueryRow(`SELECT mfa_secret FROM users WHERE id = ?`, userID).Scan(&secret)
	if err != nil || !secret.Valid {
		return "", err
	}
	return secret.String, nil
}

func (a *app) hasAnotherAdmin(excludeID int64) (bool, error) {
	var count int
	err := a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin' AND id != ?`, excludeID).Scan(&count)
	return count > 0, err
}

func (a *app) refreshUserSessions(userID int64) {
	_ = userID
}

func (a *app) deleteUserSessions(userID int64) {
	if userID <= 0 {
		return
	}
	if _, err := a.db.Exec(
		`UPDATE users SET token_version = token_version + 1, updated_at = datetime('now') WHERE id = ?`,
		userID,
	); err != nil {
		log.Printf("failed to invalidate jwt tokens for user %d: %v", userID, err)
	}
}

func (a *app) listFolders() ([]Folder, error) {
	rows, err := a.db.Query(
		`SELECT id, parent_id, name, sort_order FROM folders
		 WHERE deleted_at IS NULL
		 ORDER BY sort_order ASC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var folders []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.ParentID, &f.Name, &f.SortOrder); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

func (a *app) getFolder(id int64, includeDeleted bool) (Folder, error) {
	var folder Folder
	query := `SELECT id, parent_id, name, sort_order FROM folders WHERE id = ?`
	if !includeDeleted {
		query += ` AND deleted_at IS NULL`
	}
	err := a.db.QueryRow(query, id).Scan(&folder.ID, &folder.ParentID, &folder.Name, &folder.SortOrder)
	return folder, err
}

func (a *app) listDocuments() ([]Document, error) {
	rows, err := a.db.Query(
		`SELECT id, folder_id, title, file_path, sort_order, updated_at FROM documents
		 WHERE deleted_at IS NULL
		 ORDER BY sort_order ASC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDocuments(rows)
}

func (a *app) getDocument(id int64) (Document, error) {
	var doc Document
	err := a.db.QueryRow(
		`SELECT id, folder_id, title, file_path, sort_order, updated_at
		 FROM documents WHERE id = ? AND deleted_at IS NULL`,
		id,
	).Scan(&doc.ID, &doc.FolderID, &doc.Title, &doc.FilePath, &doc.SortOrder, &doc.UpdatedAt)
	return doc, err
}

func (a *app) getDeletedDocument(id int64) (Document, error) {
	var doc Document
	err := a.db.QueryRow(
		`SELECT id, folder_id, title, file_path, sort_order, updated_at
		 FROM documents WHERE id = ? AND deleted_at IS NOT NULL`,
		id,
	).Scan(&doc.ID, &doc.FolderID, &doc.Title, &doc.FilePath, &doc.SortOrder, &doc.UpdatedAt)
	return doc, err
}

func (a *app) createDocumentWithContent(folderID int64, title string, content string, userID int64) (int64, error) {
	res, err := a.db.Exec(
		`INSERT INTO documents (folder_id, title, file_path, sort_order, created_by, updated_by, created_at, updated_at)
		 VALUES (?, ?, '', 0, ?, ?, datetime('now'), datetime('now'))`,
		folderID, title, userID, userID,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	filePath := fmt.Sprintf("doc_%d.md", id)
	if err := os.WriteFile(filepath.Join(a.docsDir, filePath), []byte(content), 0644); err != nil {
		return 0, err
	}
	if _, err := a.db.Exec(`UPDATE documents SET file_path = ? WHERE id = ?`, filePath, id); err != nil {
		return 0, err
	}
	return id, nil
}

func documentTitleFromFilename(filename string) string {
	name := strings.TrimSpace(filepath.Base(filename))
	ext := filepath.Ext(name)
	if ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		return "导入文档"
	}
	return name
}

func (a *app) convertImportToMarkdown(filename string, data []byte, userID int64) (string, error) {
	title := documentTitleFromFilename(filename)
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".md", ".markdown":
		content := normalizeImportedText(string(data))
		if strings.TrimSpace(content) == "" {
			return "# " + title + "\n", nil
		}
		return content, nil
	case ".txt", ".log":
		content := strings.TrimSpace(normalizeImportedText(string(data)))
		if content == "" {
			return "# " + title + "\n", nil
		}
		return "# " + title + "\n\n" + content + "\n", nil
	case ".csv":
		content, err := csvToMarkdownTable(data)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(content) == "" {
			content = "_空 CSV 文件_"
		}
		return "# " + title + "\n\n" + content + "\n", nil
	case ".html", ".htm":
		content := strings.TrimSpace(htmlToMarkdownText(string(data)))
		if content == "" {
			content = "_HTML 文件没有可导入的文本内容_"
		}
		return "# " + title + "\n\n" + content + "\n", nil
	case ".docx":
		content, err := docxToMarkdown(data)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(content) == "" {
			content = "_DOCX 文件没有可导入的文本内容_"
		}
		return "# " + title + "\n\n" + content + "\n", nil
	case ".doc":
		content, err := docLegacyToMarkdown(data)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(content) == "" {
			content = "_DOC 文件没有可导入的文本内容_"
		}
		return "# " + title + "\n\n" + content + "\n", nil
	case ".pdf":
		return a.pdfImportToMarkdown(title, filename, data, userID)
	case ".xls", ".xlsx":
		content, err := excelToMarkdown(filename, data)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(content) == "" {
			content = "_Excel 文件没有可导入的表格内容_"
		}
		return "# " + title + "\n\n" + content + "\n", nil
	default:
		return "", errors.New("暂不支持该文件格式，当前支持 md、txt、log、csv、html、docx、doc、pdf、xls、xlsx")
	}
}

func normalizeImportedText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimPrefix(text, "\ufeff")
}

func csvToMarkdownTable(data []byte) (string, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return "", errors.New("CSV 文件解析失败，请检查文件格式")
	}
	if len(records) == 0 {
		return "", nil
	}
	width := 0
	for _, row := range records {
		if len(row) > width {
			width = len(row)
		}
	}
	if width == 0 {
		return "", nil
	}
	var b strings.Builder
	writeRow := func(row []string) {
		b.WriteString("|")
		for i := 0; i < width; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			b.WriteString(" ")
			b.WriteString(escapeMarkdownTableCell(cell))
			b.WriteString(" |")
		}
		b.WriteString("\n")
	}
	writeRow(records[0])
	b.WriteString("|")
	for i := 0; i < width; i++ {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, row := range records[1:] {
		writeRow(row)
	}
	return b.String(), nil
}

func escapeMarkdownTableCell(value string) string {
	value = strings.TrimSpace(normalizeImportedText(value))
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return value
}

func htmlToMarkdownText(raw string) string {
	text := normalizeImportedText(raw)
	replacements := []struct {
		pattern string
		value   string
	}{
		{`(?is)<\s*br\s*/?\s*>`, "\n"},
		{`(?is)</\s*p\s*>`, "\n\n"},
		{`(?is)</\s*div\s*>`, "\n"},
		{`(?is)</\s*h[1-6]\s*>`, "\n\n"},
		{`(?is)<\s*li[^>]*>`, "- "},
		{`(?is)</\s*li\s*>`, "\n"},
		{`(?is)<\s*script[^>]*>.*?</\s*script\s*>`, ""},
		{`(?is)<\s*style[^>]*>.*?</\s*style\s*>`, ""},
		{`(?is)<[^>]+>`, ""},
	}
	for _, item := range replacements {
		text = regexp.MustCompile(item.pattern).ReplaceAllString(text, item.value)
	}
	text = html.UnescapeString(text)
	return compactBlankLines(text)
}

func docxToMarkdown(data []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", errors.New("DOCX 文件解析失败，请确认文件未损坏")
	}
	var documentFile *zip.File
	for _, file := range reader.File {
		if file.Name == "word/document.xml" {
			documentFile = file
			break
		}
	}
	if documentFile == nil {
		return "", errors.New("DOCX 文件缺少正文内容")
	}
	rc, err := documentFile.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	decoder := xml.NewDecoder(rc)
	var b strings.Builder
	needSpace := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", errors.New("DOCX 正文解析失败")
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "t":
				var value string
				if err := decoder.DecodeElement(&value, &item); err != nil {
					return "", errors.New("DOCX 文本解析失败")
				}
				if needSpace && strings.TrimSpace(value) != "" {
					b.WriteString(" ")
				}
				b.WriteString(value)
				needSpace = false
			case "tab":
				b.WriteString(" ")
			case "br":
				b.WriteString("\n")
			}
		case xml.EndElement:
			if item.Name.Local == "p" {
				b.WriteString("\n\n")
				needSpace = false
			} else if item.Name.Local == "r" {
				needSpace = true
			}
		}
	}
	return compactBlankLines(b.String()), nil
}

func compactBlankLines(text string) string {
	lines := strings.Split(normalizeImportedText(text), "\n")
	var b strings.Builder
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blank++
			if blank > 1 {
				continue
			}
			b.WriteString("\n")
			continue
		}
		blank = 0
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func (a *app) softDeleteFolderTree(id int64) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`WITH RECURSIVE subtree(id) AS (
			SELECT id FROM folders WHERE id = ?
			UNION ALL
			SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
		)
		UPDATE documents
		SET deleted_at = COALESCE(deleted_at, datetime('now')), updated_at = datetime('now')
		WHERE folder_id IN (SELECT id FROM subtree)`,
		id,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(
		`WITH RECURSIVE subtree(id) AS (
			SELECT id FROM folders WHERE id = ?
			UNION ALL
			SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
		)
		UPDATE folders
		SET deleted_at = COALESCE(deleted_at, datetime('now')), updated_at = datetime('now')
		WHERE id IN (SELECT id FROM subtree)`,
		id,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (a *app) listTrashItems() ([]TrashItem, error) {
	items := make([]TrashItem, 0)
	folderItems := make([]TrashItem, 0)
	folderRows, err := a.db.Query(
		`SELECT id, parent_id, name, deleted_at
		 FROM folders
		 WHERE deleted_at IS NOT NULL
		   AND (parent_id = 0 OR NOT EXISTS (
		   	SELECT 1 FROM folders parent WHERE parent.id = folders.parent_id AND parent.deleted_at IS NOT NULL
		   ))
		 ORDER BY deleted_at DESC, id DESC`,
	)
	if err != nil {
		return nil, err
	}
	for folderRows.Next() {
		var item TrashItem
		item.Type = "folder"
		if err := folderRows.Scan(&item.ID, &item.ParentID, &item.Title, &item.DeletedAt); err != nil {
			_ = folderRows.Close()
			return nil, err
		}
		folderItems = append(folderItems, item)
	}
	if err := folderRows.Err(); err != nil {
		_ = folderRows.Close()
		return nil, err
	}
	if err := folderRows.Close(); err != nil {
		return nil, err
	}
	for _, item := range folderItems {
		folderCount, documentCount, err := a.countFolderTree(item.ID)
		if err != nil {
			return nil, err
		}
		item.FolderCount = folderCount
		item.DocumentCount = documentCount
		items = append(items, item)
	}
	documentRows, err := a.db.Query(
		`SELECT id, folder_id, title, deleted_at
		 FROM documents
		 WHERE deleted_at IS NOT NULL
		   AND (folder_id = 0 OR NOT EXISTS (
		   	SELECT 1 FROM folders parent WHERE parent.id = documents.folder_id AND parent.deleted_at IS NOT NULL
		   ))
		 ORDER BY deleted_at DESC, id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer documentRows.Close()
	for documentRows.Next() {
		var item TrashItem
		item.Type = "document"
		item.DocumentCount = 1
		if err := documentRows.Scan(&item.ID, &item.ParentID, &item.Title, &item.DeletedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := documentRows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].DeletedAt != items[j].DeletedAt {
			return items[i].DeletedAt > items[j].DeletedAt
		}
		return items[i].ID > items[j].ID
	})
	return items, nil
}

func (a *app) countFolderTree(id int64) (int, int, error) {
	var folderCount int
	if err := a.db.QueryRow(
		`WITH RECURSIVE subtree(id, path) AS (
			SELECT id, printf(',%d,', id) FROM folders WHERE id = ?
			UNION ALL
			SELECT f.id, s.path || f.id || ','
			FROM folders f
			JOIN subtree s ON f.parent_id = s.id
			WHERE instr(s.path, printf(',%d,', f.id)) = 0
		)
		SELECT COUNT(*) FROM subtree`,
		id,
	).Scan(&folderCount); err != nil {
		return 0, 0, err
	}
	var documentCount int
	if err := a.db.QueryRow(
		`WITH RECURSIVE subtree(id, path) AS (
			SELECT id, printf(',%d,', id) FROM folders WHERE id = ?
			UNION ALL
			SELECT f.id, s.path || f.id || ','
			FROM folders f
			JOIN subtree s ON f.parent_id = s.id
			WHERE instr(s.path, printf(',%d,', f.id)) = 0
		)
		SELECT COUNT(*) FROM documents WHERE folder_id IN (SELECT id FROM subtree)`,
		id,
	).Scan(&documentCount); err != nil {
		return 0, 0, err
	}
	return folderCount, documentCount, nil
}

func (a *app) restoreTrashItem(itemType string, id int64) (string, error) {
	switch itemType {
	case "folder":
		folder, err := a.getFolder(id, true)
		if err != nil {
			return "", errors.New("回收站中未找到该文件夹")
		}
		var folderDeletedAt sql.NullString
		if err := a.db.QueryRow(`SELECT deleted_at FROM folders WHERE id = ?`, id).Scan(&folderDeletedAt); err != nil || !folderDeletedAt.Valid {
			return "", errors.New("回收站中未找到该文件夹")
		}
		if folder.ParentID != 0 {
			var deletedAt sql.NullString
			err := a.db.QueryRow(`SELECT deleted_at FROM folders WHERE id = ?`, folder.ParentID).Scan(&deletedAt)
			if err != nil {
				return "", errors.New("上级文件夹不存在，无法恢复")
			}
			if deletedAt.Valid {
				return "", errors.New("上级文件夹仍在回收站，请先恢复上级文件夹")
			}
		}
		if err := a.restoreFolderTree(id); err != nil {
			return "", err
		}
		return fmt.Sprintf("文件夹“%s”已恢复", folder.Name), nil
	case "document":
		doc, err := a.getDeletedDocument(id)
		if err != nil {
			return "", errors.New("回收站中未找到该文档")
		}
		if doc.FolderID != 0 {
			var deletedAt sql.NullString
			err := a.db.QueryRow(`SELECT deleted_at FROM folders WHERE id = ?`, doc.FolderID).Scan(&deletedAt)
			if err != nil {
				return "", errors.New("所在文件夹不存在，无法恢复")
			}
			if deletedAt.Valid {
				return "", errors.New("所在文件夹仍在回收站，请先恢复文件夹")
			}
		}
		if _, err := a.db.Exec(
			`UPDATE documents SET deleted_at = NULL, updated_at = datetime('now') WHERE id = ? AND deleted_at IS NOT NULL`,
			id,
		); err != nil {
			return "", err
		}
		return fmt.Sprintf("文档“%s”已恢复", doc.Title), nil
	default:
		return "", errors.New("回收站项目类型无效")
	}
}

func (a *app) restoreFolderTree(id int64) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`WITH RECURSIVE subtree(id) AS (
			SELECT id FROM folders WHERE id = ?
			UNION ALL
			SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
		)
		UPDATE folders
		SET deleted_at = NULL, updated_at = datetime('now')
		WHERE id IN (SELECT id FROM subtree)`,
		id,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(
		`WITH RECURSIVE subtree(id) AS (
			SELECT id FROM folders WHERE id = ?
			UNION ALL
			SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
		)
		UPDATE documents
		SET deleted_at = NULL, updated_at = datetime('now')
		WHERE folder_id IN (SELECT id FROM subtree)`,
		id,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (a *app) purgeTrashItem(itemType string, id int64) (string, error) {
	switch itemType {
	case "folder":
		folder, err := a.getFolder(id, true)
		if err != nil {
			return "", errors.New("回收站中未找到该文件夹")
		}
		var folderDeletedAt sql.NullString
		if err := a.db.QueryRow(`SELECT deleted_at FROM folders WHERE id = ?`, id).Scan(&folderDeletedAt); err != nil || !folderDeletedAt.Valid {
			return "", errors.New("回收站中未找到该文件夹")
		}
		if err := a.purgeFolderTree(id); err != nil {
			return "", err
		}
		return fmt.Sprintf("文件夹“%s”已永久删除", folder.Name), nil
	case "document":
		doc, err := a.getDeletedDocument(id)
		if err != nil {
			return "", errors.New("回收站中未找到该文档")
		}
		if _, err := a.db.Exec(`DELETE FROM documents WHERE id = ? AND deleted_at IS NOT NULL`, id); err != nil {
			return "", err
		}
		if doc.FilePath != "" {
			if err := os.Remove(filepath.Join(a.docsDir, doc.FilePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
		if err := a.deleteDocumentVersions(id); err != nil {
			return "", err
		}
		return fmt.Sprintf("文档“%s”已永久删除", doc.Title), nil
	default:
		return "", errors.New("回收站项目类型无效")
	}
}

func (a *app) purgeFolderTree(id int64) error {
	rows, err := a.db.Query(
		`WITH RECURSIVE subtree(id) AS (
			SELECT id FROM folders WHERE id = ?
			UNION ALL
			SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
		)
		SELECT id, file_path FROM documents WHERE folder_id IN (SELECT id FROM subtree)`,
		id,
	)
	if err != nil {
		return err
	}
	docIDs := make([]int64, 0)
	filePaths := make([]string, 0)
	for rows.Next() {
		var docID int64
		var filePath string
		if err := rows.Scan(&docID, &filePath); err != nil {
			_ = rows.Close()
			return err
		}
		docIDs = append(docIDs, docID)
		if filePath != "" {
			filePaths = append(filePaths, filePath)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`WITH RECURSIVE subtree(id) AS (
			SELECT id FROM folders WHERE id = ?
			UNION ALL
			SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
		)
		DELETE FROM document_versions WHERE document_id IN (
			SELECT id FROM documents WHERE folder_id IN (SELECT id FROM subtree)
		)`,
		id,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(
		`WITH RECURSIVE subtree(id) AS (
			SELECT id FROM folders WHERE id = ?
			UNION ALL
			SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
		)
		DELETE FROM documents WHERE folder_id IN (SELECT id FROM subtree)`,
		id,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(
		`WITH RECURSIVE subtree(id) AS (
			SELECT id FROM folders WHERE id = ?
			UNION ALL
			SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
		)
		DELETE FROM folders WHERE id IN (SELECT id FROM subtree)`,
		id,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, filePath := range filePaths {
		if err := os.Remove(filepath.Join(a.docsDir, filePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, docID := range docIDs {
		_ = os.RemoveAll(filepath.Join(a.versionsDir, fmt.Sprintf("doc_%d", docID)))
	}
	return nil
}

func scanDocuments(rows *sql.Rows) ([]Document, error) {
	docs := make([]Document, 0)
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.FolderID, &d.Title, &d.FilePath, &d.SortOrder, &d.UpdatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

func buildTree(folders []Folder, documents []Document) []TreeNode {
	children := map[int64][]TreeNode{}
	for _, folder := range folders {
		children[folder.ParentID] = append(children[folder.ParentID], TreeNode{
			Key:       fmt.Sprintf("folder-%d", folder.ID),
			ID:        folder.ID,
			Type:      "folder",
			ParentID:  folder.ParentID,
			Title:     folder.Name,
			SortOrder: folder.SortOrder,
		})
	}
	for _, doc := range documents {
		children[doc.FolderID] = append(children[doc.FolderID], TreeNode{
			Key:       fmt.Sprintf("document-%d", doc.ID),
			ID:        doc.ID,
			Type:      "document",
			ParentID:  doc.FolderID,
			Title:     doc.Title,
			SortOrder: doc.SortOrder,
		})
	}
	var attachChildren func([]TreeNode) []TreeNode
	attachChildren = func(nodes []TreeNode) []TreeNode {
		if nodes == nil {
			return []TreeNode{}
		}
		sort.SliceStable(nodes, func(i, j int) bool {
			if nodes[i].Type != nodes[j].Type {
				return nodes[i].Type == "folder"
			}
			if nodes[i].SortOrder != nodes[j].SortOrder {
				return nodes[i].SortOrder < nodes[j].SortOrder
			}
			return nodes[i].ID < nodes[j].ID
		})
		for i := range nodes {
			if nodes[i].Type == "folder" {
				nodes[i].Children = attachChildren(children[nodes[i].ID])
			}
		}
		return nodes
	}
	return []TreeNode{{
		Key:      "root-0",
		ID:       0,
		Type:     "root",
		ParentID: 0,
		Title:    "知识库",
		Children: attachChildren(children[0]),
	}}
}

type treeSortUpdate struct {
	ID        int64
	Type      string
	ParentID  int64
	SortOrder int
}

func flattenTreeSort(nodes []TreeSortNode, parentID int64, ancestors map[int64]bool) ([]treeSortUpdate, error) {
	var updates []treeSortUpdate
	for index, node := range nodes {
		switch node.Type {
		case "folder":
			if node.ID <= 0 {
				return nil, errors.New("文件夹节点无效")
			}
			if ancestors[node.ID] {
				return nil, errors.New("不能把文件夹移动到自己的子级中")
			}
			sortOrder := (index + 1) * 10
			updates = append(updates, treeSortUpdate{
				ID:        node.ID,
				Type:      node.Type,
				ParentID:  parentID,
				SortOrder: sortOrder,
			})
			nextAncestors := make(map[int64]bool, len(ancestors)+1)
			for id, ok := range ancestors {
				nextAncestors[id] = ok
			}
			nextAncestors[node.ID] = true
			childUpdates, err := flattenTreeSort(node.Children, node.ID, nextAncestors)
			if err != nil {
				return nil, err
			}
			updates = append(updates, childUpdates...)
		case "document":
			if node.ID <= 0 {
				return nil, errors.New("文档节点无效")
			}
			if len(node.Children) > 0 {
				return nil, errors.New("文档不能包含子节点")
			}
			updates = append(updates, treeSortUpdate{
				ID:        node.ID,
				Type:      node.Type,
				ParentID:  parentID,
				SortOrder: (index + 1) * 10,
			})
		default:
			return nil, fmt.Errorf("不支持的节点类型: %s", node.Type)
		}
	}
	return updates, nil
}

func (a *app) withAuth(next func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("doc_token")
		if err != nil {
			http.Error(w, "未登录", http.StatusUnauthorized)
			return
		}
		claims, err := a.parseJWT(cookie.Value)
		if err != nil {
			http.Error(w, "登录已失效", http.StatusUnauthorized)
			return
		}
		user, err := a.getUserByID(claims.Subject)
		if err != nil || user.TokenVersion != claims.TokenVersion {
			http.Error(w, "登录已失效", http.StatusUnauthorized)
			return
		}
		if user.MustChangePassword && !allowedBeforePasswordChange(r.URL.Path) {
			http.Error(w, "请先修改密码", http.StatusForbidden)
			return
		}
		next(w, r, user)
	}
}

func allowedBeforePasswordChange(path string) bool {
	return path == "/api/me" || path == "/api/me/password" || path == "/api/logout"
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	public := "public"
	path := filepath.Join(public, filepath.Clean(r.URL.Path))
	if r.URL.Path == "/" {
		path = filepath.Join(public, "index.html")
	}
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(public, "index.html")
	}
	http.ServeFile(w, r, path)
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func parseID(path, prefix string) (int64, error) {
	raw := strings.TrimPrefix(path, prefix)
	raw = strings.Trim(raw, "/")
	if raw == "" || strings.Contains(raw, "/") {
		return 0, errors.New("invalid id")
	}
	return strconv.ParseInt(raw, 10, 64)
}

func parsePagination(r *http.Request) (int, int, error) {
	page := 1
	pageSize := 10
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return 0, 0, errors.New("页码必须大于 0")
		}
		page = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return 0, 0, errors.New("每页数量必须大于 0")
		}
		if parsed > 100 {
			return 0, 0, errors.New("每页数量不能超过 100")
		}
		pageSize = parsed
	}
	return page, pageSize, nil
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func mustToken(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func isAdmin(user User) bool {
	return user.Role == "admin"
}

func isSuperAdmin(user User) bool {
	return user.Username == "admin" && user.Role == "admin"
}

func canWrite(user User) bool {
	return user.Role == "admin" || user.Role == "editor"
}

func validRole(role string) bool {
	return role == "admin" || role == "editor" || role == "viewer"
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (a *app) boolSetting(key string) (bool, error) {
	var value string
	if err := a.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value); err != nil {
		return false, err
	}
	return value == "1" || strings.EqualFold(value, "true"), nil
}

func (a *app) setBoolSetting(key string, value bool) error {
	_, err := a.db.Exec(
		`INSERT INTO app_settings (key, value, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, strconv.Itoa(boolInt(value)),
	)
	return err
}

func (a *app) stringSetting(key string, fallback string) (string, error) {
	var value string
	if err := a.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fallback, nil
		}
		return fallback, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	return value, nil
}

func (a *app) setStringSetting(key string, value string) error {
	_, err := a.db.Exec(
		`INSERT INTO app_settings (key, value, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, value,
	)
	return err
}

func (a *app) intSetting(key string, fallback int) (int, error) {
	var value string
	if err := a.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fallback, nil
		}
		return fallback, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback, nil
	}
	return parsed, nil
}

func (a *app) setIntSetting(key string, value int) error {
	_, err := a.db.Exec(
		`INSERT INTO app_settings (key, value, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, strconv.Itoa(value),
	)
	return err
}

func randomTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), "="), nil
}

func totpURL(username, secret string) string {
	label := url.PathEscape("Doc System:" + username)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", "Doc System")
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + label + "?" + q.Encode()
}

func verifyTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	counter := now.Unix() / 30
	for offset := int64(-1); offset <= 1; offset++ {
		if generateTOTP(secret, counter+offset) == code {
			return true
		}
	}
	return false
}

func generateTOTP(secret string, counter int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := (int(sum[offset])&0x7f)<<24 |
		(int(sum[offset+1])&0xff)<<16 |
		(int(sum[offset+2])&0xff)<<8 |
		(int(sum[offset+3]) & 0xff)
	return fmt.Sprintf("%06d", bin%1000000)
}

func validateSelfPassword(password string) string {
	if utf8.RuneCountInString(password) < 8 {
		return "新密码至少 8 位"
	}
	categories := 0
	hasLetter := false
	hasDigit := false
	hasSpecial := false
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}
	if hasLetter {
		categories++
	}
	if hasDigit {
		categories++
	}
	if hasSpecial {
		categories++
	}
	if categories < 2 {
		return "新密码需包含字母、数字、特殊符号中的至少 2 种"
	}
	return ""
}

func badRequest(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusBadRequest)
}

func notFound(w http.ResponseWriter) {
	http.Error(w, "未找到", http.StatusNotFound)
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

func serverError(w http.ResponseWriter, err error) {
	log.Println(err)
	http.Error(w, "服务器内部错误", http.StatusInternalServerError)
}
