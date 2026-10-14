package chatbridge

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Files in a reply are uploaded to WeCom as temporary media first, in
// chunks: over HTTPS for a Channel reply, which names them by placeholder,
// and over the long connection for a direct message, where each is a
// message of its own. reply sends only files in the Workspace: its calls
// raise no Prompt, so it must not carry a file from elsewhere out of the
// computer.

const (
	chunkBytes = 512 << 10
	// maxPerReply is WeCom's most files, images and mentions in a Channel
	// reply, each; direct messages keep to it too.
	maxPerReply   = 8
	maxFileBytes  = 20 << 20
	maxImageBytes = 10 << 20
)

// outgoing is a file of the Workspace a reply sends.
type outgoing struct {
	path, name string
	// kind is an image or a file.
	kind mediaKind
}

var imageExtensions = []string{".png", ".jpg", ".jpeg", ".gif"}

// outgoingFiles checks the files a reply names: in the Workspace, after
// symbolic links, and within WeCom's limits. A relative path is the
// Workspace's.
func (b *Bridge) outgoingFiles(paths []string) ([]outgoing, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	ws := b.o.Settings.Get().ChatBridge.Workspace
	if ws == "" {
		return nil, errors.New("the Chat Bridge has no Workspace")
	}
	root, err := filepath.EvalSymlinks(ws)
	if err != nil {
		return nil, fmt.Errorf("cannot read the Workspace: %w", err)
	}
	var out []outgoing
	count := map[mediaKind]int{}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(ws, p)
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", p, err)
		}
		if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("%s is outside the Workspace %s: reply sends only files in the Workspace", p, ws)
		}
		fi, err := os.Stat(real)
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", p, err)
		}
		switch size := fi.Size(); {
		case !fi.Mode().IsRegular():
			return nil, fmt.Errorf("%s is not a file", p)
		case size == 0:
			return nil, fmt.Errorf("%s is empty", p)
		case size > maxFileBytes:
			return nil, fmt.Errorf("%s is %d bytes; WeCom takes files of at most 20 MB", p, size)
		}
		f := outgoing{path: real, name: filepath.Base(p), kind: mediaFile}
		if slices.Contains(imageExtensions, strings.ToLower(filepath.Ext(f.name))) && fi.Size() <= maxImageBytes {
			f.kind = mediaImage
		}
		if count[f.kind]++; count[f.kind] > maxPerReply {
			return nil, fmt.Errorf("a reply takes at most %d files and %d images", maxPerReply, maxPerReply)
		}
		out = append(out, f)
	}
	return out, nil
}

// uploadStep is one of WeCom's upload calls (upload_media_init, _chunk,
// _finish) on one route.
type uploadStep func(ctx context.Context, step string, body, out any) error

func (a *wecomAPI) uploadStep(ctx context.Context, step string, body, out any) error {
	return a.call(ctx, "/"+step, body, out)
}

func (l *longConnection) uploadStep(ctx context.Context, step string, body, out any) error {
	res, err := l.call(ctx, "aibot_"+step, "", body)
	if err != nil || out == nil {
		return err
	}
	if len(res) == 0 {
		return errors.New("WeCom answered " + step + " with nothing")
	}
	return json.Unmarshal(res, out)
}

// upload sends f as temporary media and returns its media_id.
func upload(ctx context.Context, call uploadStep, f outgoing) (string, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return "", err
	}
	chunks := (len(data) + chunkBytes - 1) / chunkBytes
	sum := md5.Sum(data)
	var init struct {
		UploadID string `json:"upload_id"`
	}
	if err := call(ctx, "upload_media_init", map[string]any{
		"type": f.kind, "filename": f.name, "total_size": len(data), "total_chunks": chunks, "md5": hex.EncodeToString(sum[:]),
	}, &init); err != nil {
		return "", err
	}
	if init.UploadID == "" {
		return "", errors.New("WeCom gave no upload_id")
	}
	for i := range chunks {
		part := data[i*chunkBytes : min((i+1)*chunkBytes, len(data))]
		if err := call(ctx, "upload_media_chunk", map[string]any{
			"upload_id": init.UploadID, "chunk_index": i, "base64_data": base64.StdEncoding.EncodeToString(part),
		}, nil); err != nil {
			return "", err
		}
	}
	var done struct {
		MediaID string `json:"media_id"`
	}
	if err := call(ctx, "upload_media_finish", map[string]any{"upload_id": init.UploadID}, &done); err != nil {
		return "", err
	}
	if done.MediaID == "" {
		return "", errors.New("WeCom gave no media_id")
	}
	return done.MediaID, nil
}

func uploadAll(ctx context.Context, call uploadStep, files []outgoing) ([]string, error) {
	ids := make([]string, len(files))
	for i, f := range files {
		id, err := upload(ctx, call, f)
		if err != nil {
			return nil, fmt.Errorf("not sent: cannot upload %s: %w", f.name, err)
		}
		ids[i] = id
	}
	return ids, nil
}

// channelContent is a Channel reply's Markdown with the mentions before the
// text and a placeholder for each file after it, and what names them.
func channelContent(text string, files []outgoing, mentions []string) (string, map[string]any) {
	extra := map[string]any{}
	if len(mentions) > 0 {
		var tags []string
		var list []any
		for i, u := range mentions {
			ph := fmt.Sprintf("mention_%d", i+1)
			tags = append(tags, "${inline_mention:"+ph+"}")
			list = append(list, map[string]any{"placeholder": ph, "member": map[string]any{"type": 0, "userid": u}})
		}
		text = strings.Join(tags, " ") + " " + text
		extra["inline_mention_list"] = list
	}
	n := 0
	for _, f := range files {
		if f.kind == mediaFile {
			n++
			text += fmt.Sprintf("\n\n${inline_file:file_%d}", n)
		}
	}
	return text, extra
}

// attachMedia names the uploaded media in a Channel reply's extra, in the
// order channelContent placed them.
func attachMedia(extra map[string]any, files []outgoing, ids []string) {
	var inline, images []any
	for i, f := range files {
		if f.kind == mediaImage {
			images = append(images, map[string]any{"media_id": ids[i]})
		} else {
			inline = append(inline, map[string]any{"placeholder": fmt.Sprintf("file_%d", len(inline)+1), "media_id": ids[i]})
		}
	}
	if inline != nil {
		extra["inline_file_list"] = inline
	}
	if images != nil {
		extra["image_list"] = images
	}
}

// sendDirectFiles sends uploaded media as messages of their own after the
// text.
func (b *Bridge) sendDirectFiles(ctx context.Context, th *chatThread, wc *longConnection, files []outgoing, ids []string) error {
	for i, f := range files {
		if err := b.sendDirect(ctx, th, wc, map[string]any{"msgtype": f.kind, string(f.kind): map[string]any{"media_id": ids[i]}}); err != nil {
			return fmt.Errorf("sent the text, but not %s or what follows it: %w", f.name, err)
		}
	}
	return nil
}

func withFiles(files []outgoing) string {
	switch len(files) {
	case 0:
		return ""
	case 1:
		return ", with 1 file"
	}
	return fmt.Sprintf(", with %d files", len(files))
}
