package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxDocumentVersions = 50

type DocumentVersion struct {
	ID            int64  `json:"id"`
	DocumentID    int64  `json:"document_id"`
	VersionNo     int    `json:"version_no"`
	Title         string `json:"title"`
	ContentBytes  int    `json:"content_bytes"`
	CreatedBy     int64  `json:"created_by"`
	CreatedByName string `json:"created_by_name"`
	CreatedAt     string `json:"created_at"`
}

func (a *app) createDocumentVersion(doc Document, content string, userID int64) error {
	var nextNo int
	if err := a.db.QueryRow(
		`SELECT COALESCE(MAX(version_no), 0) + 1 FROM document_versions WHERE document_id = ?`,
		doc.ID,
	).Scan(&nextNo); err != nil {
		return err
	}

	dir := filepath.Join(a.versionsDir, fmt.Sprintf("doc_%d", doc.ID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	relPath := filepath.ToSlash(filepath.Join(fmt.Sprintf("doc_%d", doc.ID), fmt.Sprintf("v%d.md", nextNo)))
	absPath := filepath.Join(a.versionsDir, filepath.FromSlash(relPath))
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return err
	}

	_, err := a.db.Exec(
		`INSERT INTO document_versions (document_id, version_no, title, file_path, content_bytes, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now'))`,
		doc.ID, nextNo, doc.Title, relPath, len([]byte(content)), userID,
	)
	if err != nil {
		_ = os.Remove(absPath)
		return err
	}
	return a.pruneDocumentVersions(doc.ID)
}

func (a *app) pruneDocumentVersions(documentID int64) error {
	rows, err := a.db.Query(
		`SELECT id, file_path FROM document_versions
		 WHERE document_id = ?
		 ORDER BY version_no DESC
		 LIMIT -1 OFFSET ?`,
		documentID, maxDocumentVersions,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []int64
	var paths []string
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return err
		}
		ids = append(ids, id)
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := a.db.Exec(`DELETE FROM document_versions WHERE id = ?`, id); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(a.versionsDir, filepath.FromSlash(paths[i])))
	}
	return nil
}

func (a *app) listDocumentVersions(documentID int64) ([]DocumentVersion, error) {
	rows, err := a.db.Query(
		`SELECT v.id, v.document_id, v.version_no, v.title, v.content_bytes, COALESCE(v.created_by, 0),
		        COALESCE(NULLIF(u.nickname, ''), u.username, '系统'), v.created_at
		 FROM document_versions v
		 LEFT JOIN users u ON u.id = v.created_by
		 WHERE v.document_id = ?
		 ORDER BY v.version_no DESC`,
		documentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []DocumentVersion{}
	for rows.Next() {
		var item DocumentVersion
		if err := rows.Scan(
			&item.ID, &item.DocumentID, &item.VersionNo, &item.Title, &item.ContentBytes,
			&item.CreatedBy, &item.CreatedByName, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *app) getDocumentVersion(documentID, versionID int64) (DocumentVersion, string, error) {
	var item DocumentVersion
	var filePath string
	err := a.db.QueryRow(
		`SELECT v.id, v.document_id, v.version_no, v.title, v.file_path, v.content_bytes, COALESCE(v.created_by, 0),
		        COALESCE(NULLIF(u.nickname, ''), u.username, '系统'), v.created_at
		 FROM document_versions v
		 LEFT JOIN users u ON u.id = v.created_by
		 WHERE v.id = ? AND v.document_id = ?`,
		versionID, documentID,
	).Scan(
		&item.ID, &item.DocumentID, &item.VersionNo, &item.Title, &filePath, &item.ContentBytes,
		&item.CreatedBy, &item.CreatedByName, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DocumentVersion{}, "", errors.New("历史版本不存在")
	}
	if err != nil {
		return DocumentVersion{}, "", err
	}
	content, err := os.ReadFile(filepath.Join(a.versionsDir, filepath.FromSlash(filePath)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DocumentVersion{}, "", errors.New("历史版本文件缺失")
		}
		return DocumentVersion{}, "", err
	}
	return item, string(content), nil
}

func (a *app) restoreDocumentVersion(documentID, versionID int64, userID int64) error {
	doc, err := a.getDocument(documentID)
	if err != nil {
		return errors.New("文档不存在或已删除")
	}
	version, content, err := a.getDocumentVersion(documentID, versionID)
	if err != nil {
		return err
	}

	currentBytes, err := os.ReadFile(filepath.Join(a.docsDir, doc.FilePath))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	currentContent := string(currentBytes)
	if currentContent != content || doc.Title != version.Title {
		if err := a.createDocumentVersion(doc, currentContent, userID); err != nil {
			return err
		}
	}

	if err := os.WriteFile(filepath.Join(a.docsDir, doc.FilePath), []byte(content), 0644); err != nil {
		return err
	}
	_, err = a.db.Exec(
		`UPDATE documents SET title = ?, updated_by = ?, updated_at = datetime('now') WHERE id = ? AND deleted_at IS NULL`,
		version.Title, userID, documentID,
	)
	return err
}

func (a *app) deleteDocumentVersions(documentID int64) error {
	rows, err := a.db.Query(`SELECT file_path FROM document_versions WHERE document_id = ?`, documentID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return err
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := a.db.Exec(`DELETE FROM document_versions WHERE document_id = ?`, documentID); err != nil {
		return err
	}
	for _, path := range paths {
		_ = os.Remove(filepath.Join(a.versionsDir, filepath.FromSlash(path)))
	}
	_ = os.Remove(filepath.Join(a.versionsDir, fmt.Sprintf("doc_%d", documentID)))
	return nil
}

func (a *app) handleDocumentVersions(w http.ResponseWriter, r *http.Request, user User, documentID int64, parts []string) {
	// parts: ["123","versions"] or ["123","versions","9"] or ["123","versions","9","restore"]
	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if _, err := a.getDocument(documentID); err != nil {
			notFound(w)
			return
		}
		items, err := a.listDocumentVersions(documentID)
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}

	if len(parts) < 3 {
		notFound(w)
		return
	}
	versionID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || versionID <= 0 {
		notFound(w)
		return
	}

	if len(parts) == 3 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		item, content, err := a.getDocumentVersion(documentID, versionID)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":              item.ID,
			"document_id":     item.DocumentID,
			"version_no":      item.VersionNo,
			"title":           item.Title,
			"content":         content,
			"content_bytes":   item.ContentBytes,
			"created_by":      item.CreatedBy,
			"created_by_name": item.CreatedByName,
			"created_at":      item.CreatedAt,
		})
		return
	}

	if len(parts) == 4 && parts[3] == "restore" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if !canWrite(user) {
			http.Error(w, "只读用户不能恢复历史版本", http.StatusForbidden)
			return
		}
		if err := a.restoreDocumentVersion(documentID, versionID, user.ID); err != nil {
			badRequest(w, err.Error())
			return
		}
		a.addOperationLog(user.ID, "document.restore_version", "document", documentID, fmt.Sprintf("文档恢复到历史版本 #%d", versionID))
		writeJSON(w, http.StatusOK, map[string]string{"message": "已恢复到所选历史版本"})
		return
	}

	notFound(w)
}

func shouldSnapshotDocument(oldTitle, newTitle, oldContent, newContent string) bool {
	return strings.TrimRight(oldContent, "\n") != strings.TrimRight(newContent, "\n") ||
		strings.TrimSpace(oldTitle) != strings.TrimSpace(newTitle)
}
