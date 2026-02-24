package tui

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ImageAttachment holds a base64-encoded image ready to send to the LLM.
type ImageAttachment struct {
	Data      string // base64
	MediaType string // e.g. "image/png"
	Label     string // e.g. "screenshot.png"
}

// imageExtensionsTUI maps file extensions to MIME types (mirrors tools/readfile.go).
var imageExtensionsTUI = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// imageExts is the set of image extensions we recognize (lowercase, with dot).
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

// detectImagePath extracts image file paths and URLs from user input text.
// Handles: quoted paths, backslash-escaped spaces, absolute/~ paths, and http(s) URLs.
// Returns the list of detected paths/URLs and the text with those removed.
func detectImagePath(text string) (paths []string, clean string) {
	type span struct {
		path       string
		start, end int
	}
	var found []span
	seen := make(map[string]bool)
	i := 0
	runes := []rune(text)
	n := len(runes)

	for i < n {
		var raw string
		var start, end int
		isURL := false

		// Case 1: quoted path — 'path' or "path"
		if runes[i] == '\'' || runes[i] == '"' {
			q := runes[i]
			j := i + 1
			for j < n && runes[j] != q {
				j++
			}
			if j < n {
				raw = string(runes[i+1 : j])
				start = i
				end = j + 1 // include closing quote
				if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
					isURL = true
				}
			} else {
				i++
				continue
			}
		} else if matchesAt(runes, i, "https://") || matchesAt(runes, i, "http://") || matchesAt(runes, i, "file://") {
			// Case 2: URL — consume until whitespace.
			j := i
			for j < n && runes[j] != ' ' && runes[j] != '\t' && runes[j] != '\n' {
				j++
			}
			raw = string(runes[i:j])
			start = i
			end = j
			isURL = true
		} else if runes[i] == '/' || (runes[i] == '~' && (i == 0 || runes[i-1] == ' ')) {
			// Case 3: absolute path or ~/path
			// Consume until unescaped whitespace or end of string.
			j := i
			for j < n {
				if runes[j] == '\\' && j+1 < n {
					j += 2 // skip escaped char (e.g. "\ ")
					continue
				}
				if runes[j] == ' ' || runes[j] == '\t' || runes[j] == '\n' {
					break
				}
				j++
			}
			raw = string(runes[i:j])
			start = i
			end = j
		} else {
			i++
			continue
		}

		var p string
		if isURL {
			// file:// URLs from drag-and-drop should be treated as local files.
			if strings.HasPrefix(raw, "file://") {
				localPath, ok := fileURLToPath(raw)
				if !ok {
					i = end
					continue
				}
				p = localPath
			} else {
				// Keep HTTP(S) URLs as candidates and let the image loader decide.
				// This avoids false negatives for signed/object URLs without extensions.
				p = raw
			}
		} else {
			// Unescape all backslash-escaped characters.
			p = unescapeBackslashes(raw)

			// Expand ~.
			if strings.HasPrefix(p, "~") {
				if home, err := os.UserHomeDir(); err == nil {
					p = filepath.Join(home, p[1:])
				}
			}

			// Check extension.
			ext := strings.ToLower(filepath.Ext(p))
			if !imageExts[ext] {
				i = end
				continue
			}

			// Check file exists.
			if _, err := os.Stat(p); err != nil {
				i = end
				continue
			}
		}

		if !seen[p] {
			seen[p] = true
			found = append(found, span{path: p, start: start, end: end})
		}
		i = end
	}

	if len(found) == 0 {
		return nil, text
	}

	// Remove matched spans (reverse order to preserve indices).
	cleanRunes := make([]rune, len(runes))
	copy(cleanRunes, runes)
	for j := len(found) - 1; j >= 0; j-- {
		s := found[j]
		paths = append([]string{s.path}, paths...)
		cleanRunes = append(cleanRunes[:s.start], cleanRunes[s.end:]...)
	}

	clean = strings.TrimSpace(string(cleanRunes))
	return paths, clean
}

// matchesAt checks if runes[i:] starts with the given prefix string.
func matchesAt(runes []rune, i int, prefix string) bool {
	pr := []rune(prefix)
	if i+len(pr) > len(runes) {
		return false
	}
	for k, r := range pr {
		if runes[i+k] != r {
			return false
		}
	}
	return true
}

// hasImageExtURL checks if a URL points to an image by examining
// the path component (ignoring query params and fragments).
func hasImageExtURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	return imageExts[ext]
}

// fileURLToPath converts file:// URLs to local paths.
// Returns false if URL is malformed or not a local file URL.
func fileURLToPath(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" {
		return "", false
	}
	p, err := url.PathUnescape(u.Path)
	if err != nil || p == "" {
		return "", false
	}
	return p, true
}

// looksLikeImageURL checks whether an HTTP(S) URL likely points to an image.
// It accepts:
// 1) URL path with known image extension
// 2) common image query hints (e.g. OSS image processing params)
// 3) response Content-Type starting with image/ (HEAD/GET probe)
func looksLikeImageURL(rawURL string) bool {
	if hasImageExtURL(rawURL) {
		return true
	}

	lower := strings.ToLower(rawURL)
	for _, hint := range []string{
		"x-oss-process=image", "image/", "format=png", "format=jpg", "format=jpeg", "format=webp", "format=gif",
		"image/png", "image/jpeg", "image/webp", "image/gif",
	} {
		if strings.Contains(lower, hint) {
			return true
		}
	}

	client := &http.Client{Timeout: 4 * time.Second}

	// Try HEAD first.
	if req, err := http.NewRequest("HEAD", rawURL, nil); err == nil {
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
			if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "image/") {
				return true
			}
		}
	}

	// Fallback to lightweight GET with range header for servers not supporting HEAD.
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "image/")
}

// unescapeBackslashes removes backslash escapes from a path string.
// Handles all common terminal escapes: "\ " → " ", "\(" → "(", "\)" → ")", etc.
func unescapeBackslashes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			i++ // skip backslash, emit the next character literally
			b.WriteRune(runes[i])
		} else {
			b.WriteRune(runes[i])
		}
	}
	return b.String()
}

// readImageBase64 reads an image file and returns an ImageAttachment with base64 data.
func readImageBase64(path string) (ImageAttachment, error) {
	ext := strings.ToLower(filepath.Ext(path))
	mediaType, ok := imageExtensionsTUI[ext]
	if !ok {
		return ImageAttachment{}, fmt.Errorf("unsupported image format: %s", ext)
	}

	info, err := os.Stat(path)
	if err != nil {
		return ImageAttachment{}, fmt.Errorf("cannot stat image: %w", err)
	}
	const maxSize = 20 * 1024 * 1024 // 20MB
	if info.Size() > maxSize {
		return ImageAttachment{}, fmt.Errorf("image too large: %d bytes (max %d)", info.Size(), maxSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ImageAttachment{}, fmt.Errorf("cannot read image: %w", err)
	}

	return ImageAttachment{
		Data:      base64.StdEncoding.EncodeToString(data),
		MediaType: mediaType,
		Label:     filepath.Base(path),
	}, nil
}

// downloadImageURL downloads an image from a URL and returns an ImageAttachment.
func downloadImageURL(imageURL string) (ImageAttachment, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(imageURL)
	if err != nil {
		return ImageAttachment{}, fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ImageAttachment{}, fmt.Errorf("image download failed: HTTP %d", resp.StatusCode)
	}

	const maxSize = 20 * 1024 * 1024 // 20MB
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return ImageAttachment{}, fmt.Errorf("failed to read image data: %w", err)
	}
	if len(data) > maxSize {
		return ImageAttachment{}, fmt.Errorf("image too large (max 20MB)")
	}

	// Determine media type: prefer Content-Type header, fallback to extension.
	mediaType := ""
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "image/") {
		// Normalize: "image/jpeg; charset=utf-8" → "image/jpeg"
		mediaType = strings.SplitN(ct, ";", 2)[0]
		mediaType = strings.TrimSpace(mediaType)
	}
	if mediaType == "" {
		// Fallback to URL extension.
		u, _ := url.Parse(imageURL)
		if u != nil {
			ext := strings.ToLower(filepath.Ext(u.Path))
			mediaType = imageExtensionsTUI[ext]
		}
	}
	if mediaType == "" {
		return ImageAttachment{}, fmt.Errorf("cannot determine image type for URL")
	}

	// Extract filename from URL path.
	label := "image"
	if u, err := url.Parse(imageURL); err == nil {
		label = filepath.Base(u.Path)
	}

	return ImageAttachment{
		Data:      base64.StdEncoding.EncodeToString(data),
		MediaType: mediaType,
		Label:     label,
	}, nil
}

// readClipboardImage reads an image from the system clipboard.
// macOS: uses osascript to write clipboard to a temp PNG, then reads it.
// Linux: uses xclip to read PNG data from clipboard.
func readClipboardImage() (ImageAttachment, error) {
	switch runtime.GOOS {
	case "darwin":
		return readClipboardImageMac()
	case "linux":
		return readClipboardImageLinux()
	default:
		return ImageAttachment{}, fmt.Errorf("clipboard image not supported on %s", runtime.GOOS)
	}
}

func readClipboardImageMac() (ImageAttachment, error) {
	// Use osascript to check if clipboard has an image and write it to a temp file.
	tmpFile, err := os.CreateTemp("", "apexion-clip-*.png")
	if err != nil {
		return ImageAttachment{}, fmt.Errorf("cannot create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	script := fmt.Sprintf(`
		set imgData to the clipboard as «class PNGf»
		set fp to open for access POSIX file %q with write permission
		write imgData to fp
		close access fp
	`, tmpPath)

	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmpPath)
		return ImageAttachment{}, fmt.Errorf("no image in clipboard: %s", strings.TrimSpace(string(out)))
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) == 0 {
		return ImageAttachment{}, fmt.Errorf("clipboard image is empty")
	}

	return ImageAttachment{
		Data:      base64.StdEncoding.EncodeToString(data),
		MediaType: "image/png",
		Label:     "clipboard.png",
	}, nil
}

func readClipboardImageLinux() (ImageAttachment, error) {
	// Try xclip first, then xsel.
	cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	data, err := cmd.Output()
	if err != nil {
		return ImageAttachment{}, fmt.Errorf("no image in clipboard (install xclip): %w", err)
	}
	if len(data) == 0 {
		return ImageAttachment{}, fmt.Errorf("clipboard image is empty")
	}

	return ImageAttachment{
		Data:      base64.StdEncoding.EncodeToString(data),
		MediaType: "image/png",
		Label:     "clipboard.png",
	}, nil
}
