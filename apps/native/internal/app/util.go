package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/attachments"
	"github.com/kkkk2323/droi/apps/native/internal/drafts"
	"github.com/kkkk2323/droi/apps/native/internal/l10n"
	"github.com/kkkk2323/droi/apps/native/internal/transcript"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func itoa(n int) string { return strconv.Itoa(n) }

// L is the window's words in the user's language: l10n.L.
func L(key string, args ...any) string { return l10n.L(key, args...) }

func draftOf(text string, images []attachments.Image) drafts.Draft {
	return drafts.Draft{Text: text, Images: images}
}

func attachmentImage(img attachments.Image) transcript.Image {
	return transcript.Image{MediaType: img.MediaType, Data: img.Data}
}

func trimOr(s, fallback string) string {
	if t := strings.TrimSpace(s); t != "" {
		return t
	}
	return fallback
}

// relativeTime is how long ago, as the sidebar rows show it: now, 5m, 3h,
// 2d, then a date.
func relativeTime(t, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	m := int(d / time.Minute)
	switch {
	case m < 1:
		return L("now")
	case m < 60:
		return L("%dm", m)
	case m < 24*60:
		return L("%dh", m/60)
	case m < 30*24*60:
		return L("%dd", m/(24*60))
	}
	return t.Format(L("Jan 2"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// workspaceName is a path's last segment.
func workspaceName(path string) string {
	if b := filepath.Base(strings.TrimRight(path, "/")); b != "." && b != "/" {
		return b
	}
	return path
}

// formatTokens is 999, 1.2k, 45k, 1.3M, as the context meter shows them.
func formatTokens(n float64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", n/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%dk", int(n/1_000+0.5))
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", n/1_000)
	}
	return fmt.Sprintf("%d", int(n))
}
