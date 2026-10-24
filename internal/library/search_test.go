package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchKnowledgeResourceMatchesTextCaseInsensitivelyAcrossChunks(t *testing.T) {
	root := t.TempDir()
	text := filepath.Join(root, "references", "guide.md")
	if err := os.MkdirAll(filepath.Dir(text), 0700); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("x", 64*1024-2) + "Café no documento"
	if err := os.WriteFile(text, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "preview.png"), []byte("CAFÉ no binário"), 0600); err != nil {
		t.Fatal(err)
	}
	found, err := SearchKnowledgeResource(root, "CAFÉ")
	if err != nil || !found {
		t.Fatalf("did not find text across chunks: found=%v err=%v", found, err)
	}
	found, err = SearchKnowledgeResource(root, "only-in-binary")
	if err != nil || found {
		t.Fatalf("searched a non-text asset: found=%v err=%v", found, err)
	}
}

func TestSearchKnowledgeResourceRejectsEmptyQuery(t *testing.T) {
	if _, err := SearchKnowledgeResource(t.TempDir(), " "); err == nil {
		t.Fatal("empty query accepted")
	}
}
