package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

type setupRequest struct {
	AppName                     string `json:"app_name"`
	AdminNickname               string `json:"admin_nickname"`
	AdminPassword               string `json:"admin_password"`
	ForcePasswordChangeNewUsers *bool  `json:"force_password_change_new_users"`
	JWTExpireDays               *int   `json:"jwt_expire_days"`
}

func (a *app) setupNeeded() (bool, error) {
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

func (a *app) ensureInitialSetup() error {
	needed, err := a.setupNeeded()
	if err != nil {
		return err
	}
	if !needed {
		return nil
	}
	password, ok := os.LookupEnv("DOC_ADMIN_PASSWORD")
	if !ok {
		return nil
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return errors.New("DOC_ADMIN_PASSWORD 不能为空")
	}
	_, err = a.completeSetup(setupRequest{
		AppName:       "Doc System",
		AdminNickname: "超级管理员",
		AdminPassword: password,
	})
	return err
}

func (a *app) completeSetup(req setupRequest) (User, error) {
	needed, err := a.setupNeeded()
	if err != nil {
		return User{}, err
	}
	if !needed {
		return User{}, errors.New("系统已完成初始化，不能重复安装")
	}

	appName := strings.TrimSpace(req.AppName)
	if appName == "" {
		appName = "Doc System"
	}
	if utf8.RuneCountInString(appName) > 40 {
		return User{}, errors.New("项目名称不能超过 40 个字符")
	}

	nickname := strings.TrimSpace(req.AdminNickname)
	if nickname == "" {
		nickname = "超级管理员"
	}
	if utf8.RuneCountInString(nickname) > 40 {
		return User{}, errors.New("管理员昵称不能超过 40 个字符")
	}

	if errMsg := validateSelfPassword(req.AdminPassword); errMsg != "" {
		return User{}, errors.New(strings.Replace(errMsg, "新密码", "管理员密码", 1))
	}

	jwtDays := 1
	if req.JWTExpireDays != nil {
		jwtDays = *req.JWTExpireDays
	}
	if jwtDays < 1 || jwtDays > 90 {
		return User{}, errors.New("JWT 有效期需在 1 到 90 天之间")
	}

	forceChange := false
	if req.ForcePasswordChangeNewUsers != nil {
		forceChange = *req.ForcePasswordChangeNewUsers
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}

	tx, err := a.db.Begin()
	if err != nil {
		return User{}, err
	}
	res, err := tx.Exec(
		`INSERT INTO users (username, password_hash, nickname, role, must_change_password, created_at, updated_at)
		 VALUES (?, ?, ?, 'admin', 0, datetime('now'), datetime('now'))`,
		"admin", string(hash), nickname,
	)
	if err != nil {
		_ = tx.Rollback()
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return User{}, err
	}
	settings := []struct {
		key   string
		value string
	}{
		{"app_name", appName},
		{"jwt_expire_days", fmt.Sprintf("%d", jwtDays)},
		{"force_password_change_new_users", fmt.Sprintf("%d", boolInt(forceChange))},
		{"setup_completed", "1"},
	}
	for _, item := range settings {
		if _, err := tx.Exec(
			`INSERT INTO app_settings (key, value, updated_at)
			 VALUES (?, ?, datetime('now'))
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
			item.key, item.value,
		); err != nil {
			_ = tx.Rollback()
			return User{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}

	user, err := a.getUserByID(id)
	if err != nil {
		return User{}, err
	}
	a.addOperationLog(user.ID, "settings.update", "settings", 0, "完成首次安装初始化")
	return user, nil
}

func (a *app) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	needed, err := a.setupNeeded()
	if err != nil {
		serverError(w, err)
		return
	}
	appName, err := a.stringSetting("app_name", "Doc System")
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"needed":   needed,
		"app_name": appName,
	})
}

func (a *app) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req setupRequest
	if err := readJSON(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	user, err := a.completeSetup(req)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := a.issueJWT(w, user); err != nil {
		serverError(w, err)
		return
	}
	a.logAuthLogin(user)
	writeJSON(w, http.StatusCreated, user)
}
