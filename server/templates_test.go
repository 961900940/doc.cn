package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewAppSeedsDefaultTemplatesWhenEmpty(t *testing.T) {
	dataDir := t.TempDir()
	a, err := newApp(dataDir)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer a.db.Close()

	templatesDir := filepath.Join(dataDir, "templates")
	names := readTemplateNames(t, templatesDir)
	expected := []string{
		"api-doc.md",
		"deploy-guide.md",
		"incident-review.md",
		"project-architecture.md",
		"tech-solution.md",
		"work-review.md",
	}
	for _, name := range expected {
		if !names[name] {
			t.Fatalf("missing default template %s, got %#v", name, names)
		}
	}
	data, err := os.ReadFile(filepath.Join(templatesDir, "project-architecture.md"))
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if !strings.Contains(string(data), "# {{title}}") {
		t.Fatalf("unexpected template content: %s", string(data))
	}
	items, err := a.listDocumentTemplates()
	if err != nil {
		t.Fatalf("list templates: %v", err)
	}
	if len(items) != len(expected) {
		t.Fatalf("unexpected template count: %d", len(items))
	}
	item, err := a.getDocumentTemplate("api-doc.md")
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	if item.Name != "api-doc.md" || item.Content == "" {
		t.Fatalf("unexpected template item: %+v", item)
	}
}

func TestNewAppDoesNotSeedWhenTemplatesExist(t *testing.T) {
	dataDir := t.TempDir()
	templatesDir := filepath.Join(dataDir, "templates")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("create templates dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "custom.md"), []byte("# 自定义模板\n"), 0644); err != nil {
		t.Fatalf("write custom template: %v", err)
	}

	a, err := newApp(dataDir)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer a.db.Close()

	names := readTemplateNames(t, templatesDir)
	if len(names) != 1 || !names["custom.md"] {
		t.Fatalf("expected only custom template, got %#v", names)
	}
}

func readTemplateNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read templates dir: %v", err)
	}
	names := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names[entry.Name()] = true
	}
	return names
}
