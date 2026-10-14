package chatbridge

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Attachments are the files and images a Chat Thread holds. The trigger and
// read_thread list each with an id that download_attachment takes. A
// Channel attachment's id names its post or reply and its file placeholder
// or image number, so its link, which lasts an hour, is read fresh at each
// download. A direct message's media are known only from the callback that
// carried them: their link lasts five minutes, to bytes encrypted with the
// aeskey beside it.

const (
	// attachmentsDir is the Workspace's folder for the Chat Threads'
	// downloads, one folder in it per Chat Thread.
	attachmentsDir   = ".droi-chat"
	maxDownloadBytes = 128 << 20
	downloadTimeout  = 2 * time.Minute
	// keptDirectMedia is how many direct-message media a Chat Thread keeps
	// for download_attachment.
	keptDirectMedia = 200
)

var downloadClient = &http.Client{Timeout: downloadTimeout}

type directMedia struct{ kind, url, aeskey, msgID string }

// mediaStore is what download_attachment needs between calls.
type mediaStore struct {
	mu     sync.Mutex
	direct map[string]map[string]directMedia
	order  map[string][]string
	saved  map[string]string
}

func (s *mediaStore) keep(thread, id string, m directMedia) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.direct == nil {
		s.direct, s.order = map[string]map[string]directMedia{}, map[string][]string{}
	}
	if s.direct[thread] == nil {
		s.direct[thread] = map[string]directMedia{}
	}
	s.direct[thread][id] = m
	s.order[thread] = append(s.order[thread], id)
	if q := s.order[thread]; len(q) > keptDirectMedia {
		delete(s.direct[thread], q[0])
		s.order[thread] = q[1:]
	}
}

func (s *mediaStore) lookup(thread, id string) (directMedia, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.direct[thread][id]
	return m, ok
}

func (s *mediaStore) savedAt(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saved[key]
}

func (s *mediaStore) save(key, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saved == nil {
		s.saved = map[string]string{}
	}
	s.saved[key] = path
}

// attachmentList is the lines naming attachments, after a message or a
// post's text.
func attachmentList(lines []string, note string) string {
	if len(lines) == 0 {
		return ""
	}
	return "\n\nAttachments, by the id download_attachment takes" + note + ":\n" + strings.Join(lines, "\n")
}

// channelAttachments names the files and images of a post (replyID "") or
// a reply.
func channelAttachments(c content, postID, replyID string) string {
	if c.IsRecalled {
		return ""
	}
	ref := postID
	if replyID != "" {
		ref += ":" + replyID
	}
	var lines []string
	for _, f := range c.Files {
		lines = append(lines, fmt.Sprintf("- file %q: %q", f.FileName, ref+":file:"+f.Placeholder))
	}
	for i := range c.Images {
		lines = append(lines, fmt.Sprintf("- image %d: %q", i+1, ref+":image:"+strconv.Itoa(i+1)))
	}
	return attachmentList(lines, "")
}

// directMediaOf is a direct message's image, file and video, or the images
// of a mixed message.
type directMediaOf struct {
	MsgType string   `json:"msgtype"`
	Image   mediaRef `json:"image"`
	File    mediaRef `json:"file"`
	Video   mediaRef `json:"video"`
	Mixed   struct {
		Items []struct {
			MsgType string   `json:"msgtype"`
			Image   mediaRef `json:"image"`
		} `json:"msg_item"`
	} `json:"mixed"`
}

type mediaRef struct {
	URL    string `json:"url"`
	AESKey string `json:"aeskey"`
}

// keepDirectMedia records the media a direct message carries and names
// them for the trigger.
func (b *Bridge) keepDirectMedia(th *thread, msgID string, body json.RawMessage) string {
	var m directMediaOf
	if json.Unmarshal(body, &m) != nil || msgID == "" {
		return ""
	}
	var lines []string
	add := func(id, label, kind string, r mediaRef) {
		if r.URL == "" {
			return
		}
		b.media.keep(th.key, id, directMedia{kind: kind, url: r.URL, aeskey: r.AESKey, msgID: msgID})
		lines = append(lines, fmt.Sprintf("- %s: %q", label, id))
	}
	switch m.MsgType {
	case "image":
		add(msgID+":image", "image", "image", m.Image)
	case "file":
		add(msgID+":file", "file", "file", m.File)
	case "video":
		add(msgID+":video", "video", "video", m.Video)
	case "mixed":
		n := 0
		for _, it := range m.Mixed.Items {
			if it.MsgType == "image" {
				n++
				add(fmt.Sprintf("%s:image:%d", msgID, n), fmt.Sprintf("image %d", n), "image", it.Image)
			}
		}
	}
	return attachmentList(lines, " within 5 minutes of the message, which WeCom's link lasts")
}

// downloadAttachment saves an attachment of the Chat Thread in its folder
// under the Workspace and says where.
func (b *Bridge) downloadAttachment(ctx context.Context, th *thread, id string) (string, error) {
	if id == "" {
		return "", errors.New("id is empty")
	}
	ws := b.o.Settings.Get().ChatBridge.Workspace
	if ws == "" {
		return "", errors.New("the Chat Bridge has no Workspace")
	}
	key := th.key + "\x00" + id
	if path := b.media.savedAt(key); path != "" {
		if fi, err := os.Stat(path); err == nil {
			return savedAnswer(path, fi.Size()), nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	var name string
	var data []byte
	var err error
	if th.kind == threadKindChannel {
		name, data, err = b.channelAttachment(ctx, th, id)
	} else {
		name, data, err = b.directAttachment(ctx, th, id)
	}
	if err != nil {
		return "", err
	}
	path, err := saveAttachment(ws, threadFolder(th), name, data)
	if err != nil {
		return "", fmt.Errorf("cannot save it: %w", err)
	}
	b.media.save(key, path)
	return savedAnswer(path, int64(len(data))), nil
}

func savedAnswer(path string, size int64) string {
	return fmt.Sprintf("Saved %s (%d bytes) at:\n%s", filepath.Base(path), size, path)
}

func unknownAttachment(id string) error {
	return fmt.Errorf("no attachment %q in this Chat Thread: use an id the messages or read_thread list", id)
}

// channelAttachment reads the post or reply again for a fresh link and
// fetches the file or image with its cookie.
func (b *Bridge) channelAttachment(ctx context.Context, th *thread, id string) (string, []byte, error) {
	parts := strings.Split(id, ":")
	var postID, replyID, kind, ref string
	switch len(parts) {
	case 3:
		postID, kind, ref = parts[0], parts[1], parts[2]
	case 4:
		postID, replyID, kind, ref = parts[0], parts[1], parts[2], parts[3]
	default:
		return "", nil, unknownAttachment(id)
	}
	b.mu.Lock()
	api := b.api
	b.mu.Unlock()
	if api == nil {
		return "", nil, errNotConnected
	}
	var c content
	var err error
	if replyID != "" {
		c, err = api.getReply(ctx, th.topicID, postID, replyID)
	} else {
		c, err = api.getPost(ctx, th.topicID, postID)
	}
	if err != nil {
		return "", nil, fmt.Errorf("cannot read the post or reply it is in: %w", err)
	}
	var link attachmentLink
	name := ""
	switch kind {
	case "file":
		for _, f := range c.Files {
			if f.Placeholder == ref {
				link, name = f.attachmentLink, f.FileName
			}
		}
	case "image":
		if n, err := strconv.Atoi(ref); err == nil && n >= 1 && n <= len(c.Images) {
			link, name = c.Images[n-1], "image-"+ref
		}
	}
	if link.URL == "" {
		return "", nil, unknownAttachment(id)
	}
	data, _, err := fetch(ctx, link.URL, link.CookieName, link.CookieValue)
	if err != nil {
		return "", nil, err
	}
	if kind == "image" {
		name += extension(data)
	}
	return name, data, nil
}

// directAttachment fetches a direct message's media and decrypts it.
func (b *Bridge) directAttachment(ctx context.Context, th *thread, id string) (string, []byte, error) {
	m, ok := b.media.lookup(th.key, id)
	if !ok {
		return "", nil, fmt.Errorf("%w; the Chat Bridge forgets them when the Host restarts", unknownAttachment(id))
	}
	data, header, err := fetch(ctx, m.url, "", "")
	if err != nil {
		return "", nil, err
	}
	if data, err = decryptMedia(data, m.aeskey); err != nil {
		return "", nil, err
	}
	name := ""
	if _, params, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil {
		name = params["filename"]
	}
	if name == "" {
		short := m.msgID
		if len(short) > 8 {
			short = short[:8]
		}
		name = m.kind + "-" + short + extension(data)
	}
	return name, data, nil
}

// fetch GETs a download link, with its cookie when it needs one. Its errors
// never carry the link.
func fetch(ctx context.Context, link, cookieName, cookieValue string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, nil, errors.New("WeCom gave an invalid download link")
	}
	if cookieName != "" {
		req.Header.Set("Cookie", cookieName+"="+cookieValue)
	}
	res, err := downloadClient.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, nil, fmt.Errorf("cannot download it: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("WeCom's download link answered HTTP %d; it may have expired", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("cannot download it: %w", err)
	}
	if len(data) > maxDownloadBytes {
		return nil, nil, fmt.Errorf("it is larger than %d MB", maxDownloadBytes>>20)
	}
	return data, res.Header, nil
}

// decryptMedia undoes WeCom's encryption of a direct message's media:
// AES-256-CBC with the aeskey's 32 bytes as they are as the key and their
// first 16 as the IV, PKCS#7-padded to a multiple of 32 bytes, so the pad
// can be up to 32 bytes long.
func decryptMedia(data []byte, aeskey string) ([]byte, error) {
	key := []byte(aeskey)
	if len(key) != 32 {
		return nil, fmt.Errorf("cannot decrypt it: its aeskey is %d bytes, not 32", len(key))
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("cannot decrypt it: WeCom sent no whole blocks")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, key[:16]).CryptBlocks(out, data)
	n := int(out[len(out)-1])
	if n < 1 || n > 32 || n > len(out) {
		return nil, errors.New("cannot decrypt it: the aeskey does not fit")
	}
	return out[:len(out)-n], nil
}

var extensions = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "image/bmp": ".bmp",
	"video/mp4": ".mp4", "application/pdf": ".pdf", "application/zip": ".zip",
}

// extension is the file name extension data's type has, "" when unknown.
func extension(data []byte) string {
	t, _, _ := strings.Cut(http.DetectContentType(data), ";")
	return extensions[t]
}

// threadFolder is the Chat Thread's folder in the Workspace's attachments
// folder: WeCom's ids hold / and +, so it is named by a hash of them.
func threadFolder(th *thread) string {
	sum := sha256.Sum256([]byte(th.key))
	return th.kind + "-" + hex.EncodeToString(sum[:6])
}

// saveAttachment writes data as name in the Chat Thread's folder, under a
// name of its own when one is taken, and keeps the folder out of Git.
func saveAttachment(ws, folder, name string, data []byte) (string, error) {
	root := filepath.Join(ws, attachmentsDir)
	dir := filepath.Join(root, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ignore := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(ignore); errors.Is(err, fs.ErrNotExist) {
		_ = os.WriteFile(ignore, []byte("*\n"), 0o644)
	}
	name = safeName(name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) && i < 1000 {
			name = fmt.Sprintf("%s-%d%s", stem, i+1, ext)
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return "", err
		}
		return path, f.Close()
	}
}

// safeName is a file name WeCom gave, as one file in one folder on macOS,
// Linux and Windows.
func safeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "attachment"
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		r := []rune(strings.TrimSuffix(name, ext))
		for len(string(r))+len(ext) > 200 {
			r = r[:len(r)-1]
		}
		name = string(r) + ext
	}
	return name
}
