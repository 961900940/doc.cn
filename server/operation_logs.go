package main

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

type OperationLog struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	Nickname    string `json:"nickname"`
	Action      string `json:"action"`
	ActionLabel string `json:"action_label"`
	TargetType  string `json:"target_type"`
	TargetID    int64  `json:"target_id"`
	Detail      string `json:"detail"`
	CreatedAt   string `json:"created_at"`
}

type OperationActionOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

var operationActionLabels = map[string]string{
	"auth.login":               "登录成功",
	"auth.login_failed":        "登录失败",
	"auth.logout":              "退出登录",
	"auth.password_change":     "修改密码",
	"user.create":              "创建用户",
	"user.update":              "更新用户",
	"user.delete":              "删除用户",
	"user.reset_password":      "重置用户密码",
	"user.reset_mfa":           "重置用户 MFA",
	"settings.update":          "更新项目配置",
	"folder.create":            "创建文件夹",
	"folder.update":            "更新文件夹",
	"folder.delete":            "删除文件夹",
	"folder.restore":           "恢复文件夹",
	"folder.purge":             "永久删除文件夹",
	"document.create":          "创建文档",
	"document.update":          "保存文档",
	"document.delete":          "删除文档",
	"document.import":          "导入文档",
	"document.restore":         "恢复文档",
	"document.purge":           "永久删除文档",
	"document.restore_version": "恢复历史版本",
	"upload.create":            "上传附件",
}

func operationActionLabel(action string) string {
	if label, ok := operationActionLabels[action]; ok {
		return label
	}
	return action
}

func listOperationActionOptions() []OperationActionOption {
	order := []string{
		"auth.login",
		"auth.login_failed",
		"auth.logout",
		"auth.password_change",
		"user.create",
		"user.update",
		"user.delete",
		"user.reset_password",
		"user.reset_mfa",
		"settings.update",
		"folder.create",
		"folder.update",
		"folder.delete",
		"folder.restore",
		"folder.purge",
		"document.create",
		"document.update",
		"document.delete",
		"document.import",
		"document.restore",
		"document.purge",
		"document.restore_version",
		"upload.create",
	}
	items := make([]OperationActionOption, 0, len(order))
	for _, value := range order {
		items = append(items, OperationActionOption{Value: value, Label: operationActionLabel(value)})
	}
	return items
}

func (a *app) addOperationLog(userID int64, action, targetType string, targetID int64, detail string) {
	detail = strings.TrimSpace(detail)
	if utf8.RuneCountInString(detail) > 500 {
		runes := []rune(detail)
		detail = string(runes[:500])
	}
	_, _ = a.db.Exec(
		`INSERT INTO operation_logs (user_id, action, target_type, target_id, detail, created_at)
		 VALUES (?, ?, ?, ?, ?, datetime('now'))`,
		nullInt64(userID), action, nullString(targetType), nullInt64(targetID), detail,
	)
}

func nullInt64(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}

func nullString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func (a *app) countOperationLogs(action, keyword string, onlyUserID int64) (int, error) {
	where, args := operationLogFilters(action, keyword, onlyUserID)
	var total int
	err := a.db.QueryRow(`SELECT COUNT(*) FROM operation_logs l LEFT JOIN users u ON u.id = l.user_id`+where, args...).Scan(&total)
	return total, err
}

func (a *app) listOperationLogs(action, keyword string, onlyUserID int64, limit, offset int) ([]OperationLog, error) {
	where, args := operationLogFilters(action, keyword, onlyUserID)
	args = append(args, limit, offset)
	rows, err := a.db.Query(
		`SELECT l.id, COALESCE(l.user_id, 0), COALESCE(u.username, ''), COALESCE(u.nickname, ''),
		        l.action, COALESCE(l.target_type, ''), COALESCE(l.target_id, 0), COALESCE(l.detail, ''), l.created_at
		 FROM operation_logs l
		 LEFT JOIN users u ON u.id = l.user_id`+where+`
		 ORDER BY l.id DESC
		 LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []OperationLog{}
	for rows.Next() {
		var item OperationLog
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.Username, &item.Nickname,
			&item.Action, &item.TargetType, &item.TargetID, &item.Detail, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.ActionLabel = operationActionLabel(item.Action)
		items = append(items, item)
	}
	return items, rows.Err()
}

func operationLogFilters(action, keyword string, onlyUserID int64) (string, []any) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 5)
	action = strings.TrimSpace(action)
	keyword = strings.TrimSpace(keyword)
	if onlyUserID > 0 {
		clauses = append(clauses, "l.user_id = ?")
		args = append(args, onlyUserID)
	}
	if action != "" {
		clauses = append(clauses, "l.action = ?")
		args = append(args, action)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		clauses = append(clauses, `(COALESCE(u.username, '') LIKE ? OR COALESCE(u.nickname, '') LIKE ? OR COALESCE(l.detail, '') LIKE ?)`)
		args = append(args, like, like, like)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (a *app) handleOperationLogs(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	page, pageSize, err := parsePagination(r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	onlyUserID := int64(0)
	if !isAdmin(user) {
		onlyUserID = user.ID
	}
	total, err := a.countOperationLogs(action, keyword, onlyUserID)
	if err != nil {
		serverError(w, err)
		return
	}
	items, err := a.listOperationLogs(action, keyword, onlyUserID, pageSize, (page-1)*pageSize)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"scope":     map[string]any{"all": isAdmin(user)},
		"actions":   listOperationActionOptions(),
	})
}

func (a *app) logAuthLogin(user User) {
	a.addOperationLog(user.ID, "auth.login", "user", user.ID, fmt.Sprintf("用户 %s 登录成功", user.Username))
}

func (a *app) logAuthLoginFailed(username string) {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "(空)"
	}
	a.addOperationLog(0, "auth.login_failed", "user", 0, fmt.Sprintf("用户名 %s 登录失败", username))
}

func (a *app) ensureOperationLogsIndex() error {
	if _, err := a.db.Exec(`CREATE INDEX IF NOT EXISTS idx_operation_logs_created_at ON operation_logs(created_at DESC)`); err != nil {
		return err
	}
	_, err := a.db.Exec(`CREATE INDEX IF NOT EXISTS idx_operation_logs_action ON operation_logs(action)`)
	return err
}
