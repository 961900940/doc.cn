package main

import (
	"strings"
	"testing"
)

func TestOperationActionLabel(t *testing.T) {
	if got := operationActionLabel("document.create"); got != "创建文档" {
		t.Fatalf("got %q", got)
	}
	if got := operationActionLabel("unknown.action"); got != "unknown.action" {
		t.Fatalf("got %q", got)
	}
}

func TestOperationLogFilters(t *testing.T) {
	where, args := operationLogFilters("auth.login", "admin", 0)
	if where == "" || len(args) != 4 {
		t.Fatalf("where=%q args=%v", where, args)
	}
	where, args = operationLogFilters("", "", 0)
	if where != "" || len(args) != 0 {
		t.Fatalf("empty filters should be empty, where=%q args=%v", where, args)
	}
	where, args = operationLogFilters("", "", 7)
	if !strings.Contains(where, "l.user_id = ?") || len(args) != 1 || args[0] != int64(7) {
		t.Fatalf("user scope where=%q args=%v", where, args)
	}
}
