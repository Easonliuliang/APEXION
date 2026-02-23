package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
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

// imagePathRe matches absolute or home-relative paths ending in an image extension.
// It handles paths that may be wrapped in quotes or contain escaped spaces.
var imagePathRe = regexp.MustCompile(`(?:['"]([^'"]+\.(?:png|jpe?g|gif|webp))['"]|(/[^\s]+\.(?:png|jpe?g|gif|webp))|(~[^\s]+\.(?:png|jpe?g|gif|webp)))`)

// detectImagePath extracts image file paths from user input text.
// Returns the list of detected paths and the text with those paths removed.
func detectImagePath(text string) (paths []string, clean string) {
	clean = text
	seen := make(map[string]bool)

	matches := imagePathRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil, text
	}

	// Collect paths and their positions (reverse order for safe removal).
	type match struct {
		path  string
		start int
		end   int
	}
	var found []match

	for _, loc := range matches {
		var p string
		// loc[2:4] = quoted group, loc[4:6] = absolute path, loc[6:8] = home path
		if loc[2] >= 0 {
			p = text[loc[2]:loc[3]]
		} else if loc[4] >= 0 {
			p = text[loc[4]:loc[5]]
		} else if loc[6] >= 0 {
			p = text[loc[6]:loc[7]]
		}
		if p == "" {
			continue
		}

		// Expand ~ to home directory.
		if strings.HasPrefix(p, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, p[1:])
			}
		}

		// Unescape backslash-spaces (common in drag-and-drop).
		p = strings.ReplaceAll(p, "\\ ", " ")

		if seen[p] {
			continue
		}

		// Verify the file exists and has a recognized image extension.
		ext := strings.ToLower(filepath.Ext(p))
		if _, ok := imageExtensionsTUI[ext]; !ok {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}

		seen[p] = true
		found = append(found, match{path: p, start: loc[0], end: loc[1]})
	}

	if len(found) == 0 {
		return nil, text
	}

	// Remove matched spans from text (process in reverse to preserve indices).
	cleanBytes := []byte(text)
	for i := len(found) - 1; i >= 0; i-- {
		m := found[i]
		paths = append([]string{m.path}, paths...) // prepend to maintain order
		cleanBytes = append(cleanBytes[:m.start], cleanBytes[m.end:]...)
	}

	clean = strings.TrimSpace(string(cleanBytes))
	return paths, clean
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
