package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// encodeBase64 encodes bytes to a base64 string.
func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// ReadFileTool reads file contents.
type ReadFileTool struct{}

func (t *ReadFileTool) Name() string        { return "read_file" }
func (t *ReadFileTool) IsReadOnly() bool     { return true }
func (t *ReadFileTool) PermissionLevel() PermissionLevel { return PermissionRead }

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file at the given path. " +
		"Use offset and limit to read specific line ranges for large files."
}

func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"file_path": map[string]any{
			"type":        "string",
			"description": "Absolute path to the file to read",
		},
		"offset": map[string]any{
			"type":        "integer",
			"description": "Line number to start reading from (0-based, optional)",
		},
		"limit": map[string]any{
			"type":        "integer",
			"description": "Maximum number of lines to read (default 2000)",
		},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("invalid params: %w", err)
	}
	// Accept both "file_path" (primary) and "path" (compat).
	if p.FilePath == "" && p.Path != "" {
		p.FilePath = p.Path
	}
	if p.FilePath == "" {
		return ToolResult{}, fmt.Errorf("file_path is required")
	}
	if p.Limit <= 0 {
		p.Limit = 2000
	}

	// Check for PDF files.
	if isPDFFile(p.FilePath) {
		return readPDF(ctx, p.FilePath, p.Offset, p.Limit)
	}

	// Check for image files.
	if mediaType, ok := detectImageFile(p.FilePath); ok {
		return readImage(p.FilePath, mediaType)
	}

	data, err := os.ReadFile(p.FilePath)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	// Apply offset
	if p.Offset > 0 {
		if p.Offset >= totalLines {
			return ToolResult{Content: fmt.Sprintf("[File has %d lines, offset %d is beyond end]", totalLines, p.Offset)}, nil
		}
		lines = lines[p.Offset:]
	}

	// Apply limit with truncation notice
	truncated := false
	if len(lines) > p.Limit {
		lines = lines[:p.Limit]
		truncated = true
	}

	// Format with line numbers
	var sb strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&sb, "%6d\t%s\n", p.Offset+i+1, line)
	}

	if truncated {
		fmt.Fprintf(&sb, "[Truncated: %d total lines. Use offset/limit to read more.]", totalLines)
	}

	return ToolResult{Content: sb.String(), Truncated: truncated}, nil
}

// ── PDF reading ──────────────────────────────────────────────────────────────

// isPDFFile returns true if the file has a .pdf extension.
func isPDFFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

// readPDF extracts text from a PDF file using pdftotext (poppler-utils).
// Falls back to a basic extraction hint if pdftotext is not available.
func readPDF(ctx context.Context, path string, offset, limit int) (ToolResult, error) {
	// Verify the file exists and check size.
	info, err := os.Stat(path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to stat file: %w", err)
	}
	const maxPDFSize = 32 * 1024 * 1024 // 32MB
	if info.Size() > maxPDFSize {
		return ToolResult{
			Content: fmt.Sprintf("PDF file too large: %d bytes (max %d bytes). Consider extracting specific pages.", info.Size(), maxPDFSize),
			IsError: true,
		}, nil
	}

	// Try pdftotext (poppler-utils).
	if pdftotextBin, err := exec.LookPath("pdftotext"); err == nil {
		cmd := exec.CommandContext(ctx, pdftotextBin, "-layout", path, "-")
		out, err := cmd.Output()
		if err != nil {
			return ToolResult{
				Content: fmt.Sprintf("pdftotext error: %v", err),
				IsError: true,
			}, nil
		}

		text := string(out)
		if strings.TrimSpace(text) == "" {
			return ToolResult{Content: "[PDF contains no extractable text (possibly scanned/image-only)]"}, nil
		}

		// Apply offset/limit.
		lines := strings.Split(text, "\n")
		totalLines := len(lines)
		if offset > 0 {
			if offset >= totalLines {
				return ToolResult{Content: fmt.Sprintf("[PDF text has %d lines, offset %d is beyond end]", totalLines, offset)}, nil
			}
			lines = lines[offset:]
		}
		truncated := false
		if len(lines) > limit {
			lines = lines[:limit]
			truncated = true
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("[PDF: %s, %d lines extracted]\n\n", filepath.Base(path), totalLines))
		for i, line := range lines {
			fmt.Fprintf(&sb, "%6d\t%s\n", offset+i+1, line)
		}
		if truncated {
			fmt.Fprintf(&sb, "[Truncated: %d total lines. Use offset/limit to read more.]", totalLines)
		}

		return ToolResult{Content: sb.String(), Truncated: truncated}, nil
	}

	// Fallback: try reading raw text from PDF (basic extraction).
	data, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to read PDF: %w", err)
	}

	// Very basic text extraction: find text between stream/endstream markers.
	// This is a minimal fallback — real extraction requires pdftotext.
	text := extractBasicPDFText(data)
	if text != "" {
		lines := strings.Split(text, "\n")
		totalLines := len(lines)
		if offset > 0 && offset < totalLines {
			lines = lines[offset:]
		}
		if len(lines) > limit {
			lines = lines[:limit]
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("[PDF: %s, basic text extraction (install poppler for better results)]\n\n", filepath.Base(path)))
		sb.WriteString(strings.Join(lines, "\n"))
		return ToolResult{Content: sb.String()}, nil
	}

	return ToolResult{
		Content: fmt.Sprintf("[PDF file: %s (%d bytes)]\n\nCannot extract text. Install poppler for PDF support:\n  macOS: brew install poppler\n  Ubuntu: apt install poppler-utils\n  Fedora: dnf install poppler-utils",
			filepath.Base(path), info.Size()),
		IsError: true,
	}, nil
}

// extractBasicPDFText attempts a minimal text extraction from raw PDF bytes.
// Looks for text between BT/ET markers (PDF text objects). Very limited.
func extractBasicPDFText(data []byte) string {
	content := string(data)
	var result strings.Builder
	seen := make(map[string]bool)

	// Find text in parentheses within BT..ET blocks (PDF text objects).
	idx := 0
	for idx < len(content) {
		btIdx := strings.Index(content[idx:], "BT")
		if btIdx < 0 {
			break
		}
		btIdx += idx
		etIdx := strings.Index(content[btIdx:], "ET")
		if etIdx < 0 {
			break
		}
		etIdx += btIdx

		block := content[btIdx:etIdx]
		// Extract text from Tj and TJ operators (text in parentheses).
		for i := 0; i < len(block); i++ {
			if block[i] == '(' {
				depth := 1
				start := i + 1
				for j := start; j < len(block); j++ {
					if block[j] == '\\' {
						j++ // skip escaped char
						continue
					}
					if block[j] == '(' {
						depth++
					} else if block[j] == ')' {
						depth--
						if depth == 0 {
							text := block[start:j]
							if !seen[text] && len(strings.TrimSpace(text)) > 0 {
								seen[text] = true
								result.WriteString(text)
							}
							i = j
							break
						}
					}
				}
			}
		}

		idx = etIdx + 2
	}

	return strings.TrimSpace(result.String())
}

// ── Image reading ────────────────────────────────────────────────────────────

// imageExtensions maps file extensions to MIME types.
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// imageSignatures maps magic bytes to MIME types for validation.
var imageSignatures = []struct {
	prefix    string
	mediaType string
}{
	{"\x89PNG", "image/png"},
	{"\xff\xd8\xff", "image/jpeg"},
	{"GIF87a", "image/gif"},
	{"GIF89a", "image/gif"},
	{"RIFF", "image/webp"}, // Need to also check for "WEBP" at offset 8.
}

// detectImageFile checks if a file is an image by extension + magic bytes.
// Returns (mediaType, true) if it's a valid image, ("", false) otherwise.
func detectImageFile(path string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	expectedType, isImageExt := imageExtensions[ext]
	if !isImageExt {
		return "", false
	}

	// Validate with magic bytes.
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	header := make([]byte, 12)
	n, err := f.Read(header)
	if err != nil || n < 4 {
		return "", false
	}

	for _, sig := range imageSignatures {
		if n >= len(sig.prefix) && string(header[:len(sig.prefix)]) == sig.prefix {
			// Special check for WebP: need "WEBP" at offset 8.
			if sig.mediaType == "image/webp" {
				if n >= 12 && string(header[8:12]) == "WEBP" {
					return sig.mediaType, true
				}
				continue
			}
			return sig.mediaType, true
		}
	}

	// Extension matched but magic bytes didn't — still return expected type
	// to avoid false negatives for unusual encodings.
	return expectedType, true
}

// readImage reads an image file and returns it as base64-encoded data.
func readImage(path string, mediaType string) (ToolResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to stat image: %w", err)
	}

	const maxImageSize = 20 * 1024 * 1024 // 20MB
	if info.Size() > maxImageSize {
		return ToolResult{
			Content: fmt.Sprintf("Image too large: %d bytes (max %d bytes)", info.Size(), maxImageSize),
			IsError: true,
		}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to read image: %w", err)
	}

	encoded := encodeBase64(data)

	return ToolResult{
		Content:        fmt.Sprintf("[Image: %s, %s, %d bytes]", filepath.Base(path), mediaType, len(data)),
		ImageData:      encoded,
		ImageMediaType: mediaType,
	}, nil
}
