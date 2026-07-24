package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteKnowledgeBaseExport(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer a.db.Close()
	if _, err := a.completeSetup(setupRequest{AdminPassword: "Admin123!"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	res, err := a.db.Exec(
		`INSERT INTO folders (parent_id, name, sort_order, created_at, updated_at)
		 VALUES (0, '项目:A', 0, datetime('now'), datetime('now'))`,
	)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	folderID, _ := res.LastInsertId()
	if _, err := a.db.Exec(
		`INSERT INTO folders (parent_id, name, sort_order, created_at, updated_at)
		 VALUES (0, '空文件夹', 1, datetime('now'), datetime('now'))`,
	); err != nil {
		t.Fatalf("create empty folder: %v", err)
	}
	if _, err := a.createDocumentWithContent(folderID, "架构说明", "# 架构说明\n\nhello", 1); err != nil {
		t.Fatalf("create document: %v", err)
	}
	uploadDir := filepath.Join(a.uploadDir, "2026", "07")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		t.Fatalf("create upload dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "demo.txt"), []byte("upload"), 0644); err != nil {
		t.Fatalf("write upload: %v", err)
	}

	var buf bytes.Buffer
	stats, err := a.writeKnowledgeBaseExport(&buf)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if stats.FolderCount != 2 || stats.DocumentCount != 1 || stats.UploadCount != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	entries := map[string]string{}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", file.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", file.Name, err)
		}
		entries[file.Name] = string(data)
	}
	if !strings.Contains(entries["知识库/项目-A/架构说明.md"], "hello") {
		t.Fatalf("missing exported markdown document, entries: %#v", entries)
	}
	if _, ok := entries["知识库/空文件夹/"]; !ok {
		t.Fatalf("missing exported empty folder, entries: %#v", entries)
	}
	if entries["uploads/2026/07/demo.txt"] != "upload" {
		t.Fatalf("missing exported upload, entries: %#v", entries)
	}
	if !strings.Contains(entries["manifest.json"], `"document_count": 1`) {
		t.Fatalf("missing manifest stats: %s", entries["manifest.json"])
	}
}

func TestDocumentAndFolderExportFormats(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer a.db.Close()
	if _, err := a.completeSetup(setupRequest{AdminPassword: "Admin123!"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	res, err := a.db.Exec(
		`INSERT INTO folders (parent_id, name, sort_order, created_at, updated_at)
		 VALUES (0, '导出测试', 0, datetime('now'), datetime('now'))`,
	)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	folderID, _ := res.LastInsertId()
	if _, err := a.db.Exec(
		`INSERT INTO folders (parent_id, name, sort_order, created_at, updated_at)
		 VALUES (?, '空子目录', 1, datetime('now'), datetime('now'))`,
		folderID,
	); err != nil {
		t.Fatalf("create empty child folder: %v", err)
	}
	docID, err := a.createDocumentWithContent(folderID, "接口说明", "# 接口说明\n\n- 登录接口\n\n`GET /api/me`", 1)
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	doc, err := a.getDocument(docID)
	if err != nil {
		t.Fatalf("get document: %v", err)
	}
	content, err := a.readDocumentContent(doc)
	if err != nil {
		t.Fatalf("read document: %v", err)
	}

	htmlData, htmlName, htmlType, err := renderDocumentExport(doc, content, "html")
	if err != nil {
		t.Fatalf("render html: %v", err)
	}
	if htmlName != "接口说明.html" || !strings.Contains(htmlType, "text/html") {
		t.Fatalf("unexpected html meta: %s %s", htmlName, htmlType)
	}
	if !strings.Contains(string(htmlData), "<h1>接口说明</h1>") || !strings.Contains(string(htmlData), "<li>登录接口</li>") {
		t.Fatalf("unexpected html: %s", string(htmlData))
	}

	pdfData, pdfName, pdfType, err := renderDocumentExport(doc, content, "pdf")
	if err != nil {
		t.Fatalf("render pdf: %v", err)
	}
	if pdfName != "接口说明.pdf" || pdfType != "application/pdf" || !bytes.HasPrefix(pdfData, []byte("%PDF-")) {
		t.Fatalf("unexpected pdf export: %s %s %.8q", pdfName, pdfType, pdfData)
	}

	folder, err := a.getFolder(folderID, false)
	if err != nil {
		t.Fatalf("get folder: %v", err)
	}
	zipData, count, err := a.renderFolderExport(folder, "html")
	if err != nil {
		t.Fatalf("render folder: %v", err)
	}
	if count != 1 {
		t.Fatalf("unexpected folder export count: %d", count)
	}
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("open folder zip: %v", err)
	}
	found := false
	foundEmptyDir := false
	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, "接口说明.html") {
			found = true
		}
		if file.Name == "导出测试/空子目录/" {
			foundEmptyDir = true
		}
	}
	if !found {
		t.Fatalf("folder zip missing html document")
	}
	if !foundEmptyDir {
		t.Fatalf("folder zip missing empty child folder")
	}
}
