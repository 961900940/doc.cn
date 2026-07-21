package main

import "testing"

func TestShouldSnapshotDocument(t *testing.T) {
	tests := []struct {
		name       string
		oldTitle   string
		newTitle   string
		oldContent string
		newContent string
		want       bool
	}{
		{name: "unchanged", oldTitle: "A", newTitle: "A", oldContent: "hello", newContent: "hello", want: false},
		{name: "trailing newline only", oldTitle: "A", newTitle: "A", oldContent: "hello\n", newContent: "hello", want: false},
		{name: "content changed", oldTitle: "A", newTitle: "A", oldContent: "hello", newContent: "world", want: true},
		{name: "title changed", oldTitle: "A", newTitle: "B", oldContent: "hello", newContent: "hello", want: true},
		{name: "title trim", oldTitle: " A ", newTitle: "A", oldContent: "x", newContent: "x", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSnapshotDocument(tt.oldTitle, tt.newTitle, tt.oldContent, tt.newContent)
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
