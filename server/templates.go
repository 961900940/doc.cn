package main

import (
	"embed"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed defaults/templates/*.md
var defaultTemplatesFS embed.FS

var defaultTemplateTitles = map[string]string{
	"api-doc.md":              "接口文档",
	"deploy-guide.md":         "部署文档",
	"incident-review.md":      "故障复盘",
	"project-architecture.md": "项目架构说明",
	"tech-solution.md":        "技术方案",
	"work-review.md":          "工作复盘",
}

type DocumentTemplate struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Content string `json:"content,omitempty"`
}

func (a *app) handleTemplates(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if r.URL.Path == "/api/templates" {
		items, err := a.listDocumentTemplates()
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/templates/") {
		notFound(w)
		return
	}
	name, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/templates/"))
	if err != nil {
		badRequest(w, "模板名称不正确")
		return
	}
	item, err := a.getDocumentTemplate(name)
	if err != nil {
		if os.IsNotExist(err) {
			notFound(w)
			return
		}
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *app) ensureDefaultTemplates() error {
	if strings.TrimSpace(a.templatesDir) == "" {
		return nil
	}
	if err := os.MkdirAll(a.templatesDir, 0755); err != nil {
		return err
	}
	empty, err := isTemplateDirEmpty(a.templatesDir)
	if err != nil {
		return err
	}
	if !empty {
		return nil
	}

	entries, err := fs.ReadDir(defaultTemplatesFS, "defaults/templates")
	if err != nil {
		return err
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		source := path.Join("defaults/templates", entry.Name())
		data, err := defaultTemplatesFS.ReadFile(source)
		if err != nil {
			return err
		}
		target := filepath.Join(a.templatesDir, entry.Name())
		if err := os.WriteFile(target, data, 0644); err != nil {
			return err
		}
		_ = os.Chtimes(target, now, now)
	}
	return nil
}

func (a *app) listDocumentTemplates() ([]DocumentTemplate, error) {
	entries, err := os.ReadDir(a.templatesDir)
	if err != nil {
		return nil, err
	}
	items := make([]DocumentTemplate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		item, err := a.getDocumentTemplate(entry.Name())
		if err != nil {
			return nil, err
		}
		item.Content = ""
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (a *app) getDocumentTemplate(name string) (DocumentTemplate, error) {
	cleanName, err := cleanTemplateName(name)
	if err != nil {
		return DocumentTemplate{}, err
	}
	data, err := os.ReadFile(filepath.Join(a.templatesDir, cleanName))
	if err != nil {
		return DocumentTemplate{}, err
	}
	content := string(data)
	return DocumentTemplate{
		Name:    cleanName,
		Title:   templateTitle(cleanName, content),
		Content: content,
	}, nil
}

func cleanTemplateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.HasPrefix(name, ".") {
		return "", os.ErrInvalid
	}
	if !strings.HasSuffix(name, ".md") {
		return "", os.ErrInvalid
	}
	return name, nil
}

func templateTitle(name string, content string) string {
	if title := defaultTemplateTitles[name]; title != "" {
		return title
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "# "))
			if title != "" && title != "{{title}}" {
				return title
			}
		}
	}
	title := strings.TrimSuffix(name, path.Ext(name))
	title = strings.ReplaceAll(title, "-", " ")
	return title
}

func isTemplateDirEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".DS_Store" || strings.HasPrefix(name, ".") {
			continue
		}
		return false, nil
	}
	return true, nil
}
