// Package attachments is images attached in the composer, held as base64
// until the message goes out (the Daemon takes image blocks inline, not by
// path). Reading and shrinking a picked image is up to each Client (a port of
// attachments.ts and the pure part of local-image.ts).
package attachments

import (
	"math"
	"net/url"
	"slices"
	"strings"
)

var ImageMediaTypes = []string{"image/jpeg", "image/png", "image/gif", "image/webp"}

// Image is an attached picture.
type Image struct {
	ID        string
	Name      string
	MediaType string
	// Data is base64 without the data-URL prefix, as the Daemon wants it.
	Data string
}

func IsImageMediaType(mediaType string) bool { return slices.Contains(ImageMediaTypes, mediaType) }

// MaxImageEdge is the longest edge the model can use; Anthropic downsamples
// anything larger anyway.
const MaxImageEdge = 1568

// MaxImageBytes: above this the image is re-encoded. The Daemon estimates
// context as characters / 4 and counts base64 image data at full length, so a
// 2 MB screenshot reads as ~700k tokens and trips compaction; kept small it
// does not.
const MaxImageBytes = 300_000

// FitWithin scales (width, height) down to fit max on the longest edge, never up.
func FitWithin(width, height, max int) (int, int) {
	longest := math.Max(float64(width), float64(height))
	if longest <= float64(max) {
		return width, height
	}
	scale := float64(max) / longest
	return maxInt(1, int(math.Round(float64(width)*scale))), maxInt(1, int(math.Round(float64(height)*scale)))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (i Image) URL() string { return "data:" + i.MediaType + ";base64," + i.Data }

// LocalImagePath is the absolute path on the computer an image `src` names,
// or "" when a browser could load it itself (http, data, blob, ...). Images
// the agent writes into its markdown as paths, such as a screenshot it took,
// are not loadable that way: the Local Client's origin is not the filesystem
// and a Remote Client is on another device, so the Daemon reads them instead
// (`daemon.get_workspace_file_content`). Markdown sanitising has already
// percent-encoded the path by the time it reaches a component.
func LocalImagePath(src string) string {
	trimmed := strings.TrimSpace(src)
	if !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") {
		return ""
	}
	if decoded, err := url.PathUnescape(trimmed); err == nil {
		return decoded
	}
	return trimmed
}
