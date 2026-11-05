package assistant

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Chunk struct {
	Document  string    `json:"document"`
	Section   string    `json:"section"`
	Index     int       `json:"chunk_index"`
	Content   string    `json:"content"`
	Hash      string    `json:"-"`
	Embedding []float32 `json:"-"`
}

// Only regular top-level Markdown files are imported; symlinks are rejected.
func LoadDocuments(dir string) ([]Chunk, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read policy directory")
	}
	var chunks []Chunk
	documents := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil, fmt.Errorf("policies must be regular Markdown files")
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("policies must be regular Markdown files")
		}
		documents++
		if documents > 10 || len(entry.Name()) > 120 {
			return nil, fmt.Errorf("at most 10 policies with names up to 120 bytes are allowed")
		}
		file, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("open policy file")
		}
		data, readErr := io.ReadAll(io.LimitReader(file, MaxDocumentBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return nil, fmt.Errorf("read policy file")
		}
		if len(data) > MaxDocumentBytes || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return nil, fmt.Errorf("policy must be valid UTF-8, without NUL, and at most %d bytes", MaxDocumentBytes)
		}
		parts, err := SplitDocument(entry.Name(), string(data))
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, parts...)
		if len(chunks) > MaxChunks {
			return nil, fmt.Errorf("policy corpus exceeds %d chunks", MaxChunks)
		}
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("no policy Markdown content found")
	}
	return chunks, nil
}

// Section IDs include heading line numbers, so repeated headings remain distinct.
func SplitDocument(name, text string) ([]Chunk, error) {
	if len(text) > MaxDocumentBytes || !utf8.ValidString(text) {
		return nil, fmt.Errorf("invalid policy document")
	}
	var chunks []Chunk
	section := "intro@L1"
	var lines []string
	flush := func() {
		body := strings.TrimSpace(strings.Join(lines, "\n"))
		lines = nil
		index := 0
		for len(body) > 0 {
			end := len(body)
			if end > MaxChunkBytes {
				end = MaxChunkBytes
				for !utf8.RuneStart(body[end]) {
					end--
				}
			}
			content := body[:end]
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
			chunks = append(chunks, Chunk{Document: name, Section: section, Index: index, Content: content, Hash: hash})
			index++
			body = strings.TrimSpace(body[end:])
		}
	}
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "#") {
			heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
			if heading != "" {
				flush()
				if len(heading) > 160 {
					return nil, fmt.Errorf("policy heading exceeds 160 bytes")
				}
				section = fmt.Sprintf("%s@L%d", heading, i+1)
			}
		}
		lines = append(lines, line)
	}
	flush()
	return chunks, nil
}

func EmbeddingText(c Chunk) string { return c.Document + "\n" + c.Section + "\n" + c.Content }
