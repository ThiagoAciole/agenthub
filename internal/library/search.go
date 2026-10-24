package library

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxSearchQueryBytes = 4096

// SearchKnowledgeResource searches registered plain-text formats without returning document content.
func SearchKnowledgeResource(root, query string) (bool, error) {
	if strings.TrimSpace(query) == "" || len(query) > maxSearchQueryBytes {
		return false, fmt.Errorf("consulta vazia ou maior que %d bytes", maxSearchQueryBytes)
	}
	files, err := resourceFiles(root)
	if err != nil {
		return false, err
	}
	query = strings.ToLower(query)
	for _, relative := range files {
		if !searchableTextFile(relative) {
			continue
		}
		file, err := os.Open(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return false, err
		}
		found, searchErr := readerContains(file, query)
		closeErr := file.Close()
		if searchErr != nil {
			return false, searchErr
		}
		if closeErr != nil {
			return false, closeErr
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

func searchableTextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".adoc", ".csv", ".json", ".markdown", ".md", ".rst", ".text", ".toml", ".txt", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func readerContains(reader io.Reader, query string) (bool, error) {
	buffer := make([]byte, 64*1024)
	keep := len(query) + 4
	tail := make([]byte, 0, keep)
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			chunk := make([]byte, 0, len(tail)+n)
			chunk = append(chunk, tail...)
			chunk = append(chunk, buffer[:n]...)
			if strings.Contains(strings.ToLower(string(chunk)), query) {
				return true, nil
			}
			if len(chunk) > keep {
				tail = append(tail[:0], chunk[len(chunk)-keep:]...)
			} else {
				tail = append(tail[:0], chunk...)
			}
		}
		if readErr == io.EOF {
			return false, nil
		}
		if readErr != nil {
			return false, readErr
		}
	}
}
