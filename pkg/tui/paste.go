package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"pi/pkg/types"
)

// pasteMsg carries an image read from the system clipboard (or nil + error).
type pasteMsg struct {
	img *types.ImageAttachment
	err error
}

func readClipboardImageMsg() tea.Msg {
	img, err := readClipboardImage()
	if err != nil {
		return pasteMsg{err: err}
	}
	return pasteMsg{img: img}
}

func readClipboardImage() (*types.ImageAttachment, error) {
	// 1) Wayland: wl-paste --type image/*
	if data, mt, ok := wlPasteImage(); ok {
		return encodeImage(data, mt)
	}
	// 2) X11: xclip -selection clipboard -t TARGETS -o | grep IMAGE
	if data, mt, ok := xclipImage(); ok {
		return encodeImage(data, mt)
	}
	// 3) Fallback: clipboard contains a text path to an image file
	if txt := textClipboard(); txt != "" {
		p := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(txt), "file://"))
		if p != "" {
			if abs, err := filepath.Abs(p); err == nil {
				if st, err := os.Stat(abs); err == nil && !st.IsDir() {
					if mt := imageMIME(abs); mt != "" {
						data, err := os.ReadFile(abs)
						if err != nil {
							return nil, fmt.Errorf("read %s: %w", abs, err)
						}
						return encodeImage(data, mt)
					}
				}
			}
		}
	}
	return nil, fmt.Errorf("no image found in clipboard (copy an image, or a file path to one)")
}

func imageMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return ""
}

const maxPasteBytes = 8 << 20 // 8 MiB raw

func encodeImage(data []byte, mediaType string) (*types.ImageAttachment, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty image data")
	}
	if len(data) > maxPasteBytes {
		return nil, fmt.Errorf("image too large (%d MB, max %d MB)", len(data)>>20, maxPasteBytes>>20)
	}
	return &types.ImageAttachment{MediaType: mediaType, Data: base64.StdEncoding.EncodeToString(data)}, nil
}

func wlPasteImage() ([]byte, string, bool) {
	bin, err := exec.LookPath("wl-paste")
	if err != nil {
		return nil, "", false
	}
	out, err := exec.Command(bin, "--list-types").Output()
	if err != nil {
		return nil, "", false
	}
	for _, line := range strings.Fields(string(out)) {
		if strings.HasPrefix(line, "image/") {
			data, err := exec.Command(bin, "-t", line).Output()
			if err == nil && len(data) > 0 {
				return data, line, true
			}
		}
	}
	return nil, "", false
}

func xclipImage() ([]byte, string, bool) {
	bin, err := exec.LookPath("xclip")
	if err != nil {
		return nil, "", false
	}
	out, err := exec.Command(bin, "-selection", "clipboard", "-t", "TARGETS", "-o").Output()
	if err != nil {
		return nil, "", false
	}
	for _, line := range strings.Fields(string(out)) {
		if strings.HasPrefix(line, "image/") {
			data, err := exec.Command(bin, "-selection", "clipboard", "-t", line, "-o").Output()
			if err == nil && len(data) > 0 {
				return data, line, true
			}
		}
	}
	return nil, "", false
}

func textClipboard() string {
	if s, err := clipboard.ReadAll(); err == nil {
		return s
	}
	return ""
}
