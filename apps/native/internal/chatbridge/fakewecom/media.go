package fakewecom

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Files and media. A Channel's files and images come with a download link
// that needs its cookie and lasts an hour; a direct message's image, file
// or video with a link that lasts five minutes, to bytes encrypted with the
// aeskey beside it. Temporary media are uploaded in chunks, over HTTPS for a
// Channel reply and over the long connection for a direct message; a
// media_id works only on the route it was uploaded by.

// Image is an image in a post or reply.
type Image struct {
	Data []byte
}

// Media is an uploaded temporary media.
type Media struct {
	// Route is "https" or "websocket".
	Route string
	// Type is file, image, voice or video.
	Type string
	Name string
	Data []byte
}

// DirectMedia is an image, file or video a user sends the robot in a direct
// message. MsgID and ReqID are made up when empty. Name, when set, comes
// back in the download's Content-Disposition.
type DirectMedia struct {
	MsgID string
	ReqID string
	From  string
	// Kind is image, file or video.
	Kind string
	Name string
	Data []byte
}

const (
	channelLinkLife = time.Hour
	directLinkLife  = 5 * time.Minute
	// CookieName is the cookie a Channel download link needs.
	CookieName     = "wwfile_ticket"
	maxChunkBytes  = 512 << 10
	maxChunks      = 100
	maxAttachments = 8
)

var maxMediaBytes = map[string]int{"file": 20 << 20, "image": 10 << 20, "video": 10 << 20, "voice": 2 << 20}

type download struct {
	data    []byte
	name    string
	cookie  string
	expires time.Time
}

type upload struct {
	route, typ, name, md5 string
	size, chunks          int
	data                  map[int][]byte
}

// link serves data at a new download link; called with w.mu held.
func (w *WeCom) link(data []byte, name, cookie string, life time.Duration) string {
	if w.downloads == nil {
		w.downloads = map[string]download{}
	}
	id := randomID()
	w.downloads[id] = download{data: data, name: name, cookie: cookie, expires: w.clock().Add(life)}
	return w.APIURL + "/download/" + id
}

// serveDownload answers a GET of a download link; it reports whether r was
// one.
func (w *WeCom) serveDownload(rw http.ResponseWriter, r *http.Request) bool {
	id, ok := strings.CutPrefix(r.URL.Path, "/download/")
	if !ok || r.Method != http.MethodGet {
		return false
	}
	w.mu.Lock()
	d, known := w.downloads[id]
	now := w.clock()
	w.mu.Unlock()
	switch {
	case !known:
		http.NotFound(rw, r)
		return true
	case !now.Before(d.expires):
		http.Error(rw, "link expired", http.StatusForbidden)
		return true
	case d.cookie != "":
		if c, err := r.Cookie(CookieName); err != nil || c.Value != d.cookie {
			http.Error(rw, "no cookie", http.StatusForbidden)
			return true
		}
	}
	if d.name != "" {
		rw.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.name}))
	}
	rw.Header().Set("Content-Type", "application/octet-stream")
	_, _ = rw.Write(d.data)
	return true
}

// channelLink is a Channel download link with its cookie; called with w.mu
// held.
func (w *WeCom) channelLink(data []byte) map[string]any {
	cookie := randomID()
	return map[string]any{"download_url": w.link(data, "", cookie, channelLinkLife), "cookie_name": CookieName, "cookie_value": cookie}
}

// HostMedia encrypts data as WeCom does a direct message's media and serves
// it for five minutes; it returns the url and aeskey a callback carries.
func (w *WeCom) HostMedia(data []byte, name string) (url, aeskey string) {
	aeskey = randomKey()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.link(Encrypt(data, aeskey), name, "", directLinkLife), aeskey
}

// SendDirectMedia pushes an image, file or video message from a user and
// returns its req_id.
func (w *WeCom) SendDirectMedia(m DirectMedia) string {
	w.t.Helper()
	if m.MsgID == "" {
		m.MsgID = randomID()
	}
	url, key := w.HostMedia(m.Data, m.Name)
	return w.Push("aibot_msg_callback", m.ReqID, map[string]any{
		"msgid":    m.MsgID,
		"aibotid":  w.robot.BotID,
		"chattype": "single",
		"from":     map[string]string{"userid": m.From},
		"msgtype":  m.Kind,
		m.Kind:     map[string]string{"url": url, "aeskey": key},
	})
}

// Encrypt is WeCom's encryption of a direct message's media: AES-256-CBC
// with the aeskey's 32 bytes as the key and their first 16 as the IV,
// PKCS#7-padded to a multiple of 32 bytes.
func Encrypt(data []byte, aeskey string) []byte {
	key := []byte(aeskey)
	n := 32 - len(data)%32
	padded := append(slices.Clone(data), bytes.Repeat([]byte{byte(n)}, n)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(out, padded)
	return out
}

func randomKey() string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

// Media is the temporary media uploaded as id.
func (w *WeCom) Media(id string) (Media, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	m, ok := w.media[id]
	return m, ok
}

// mediaOf is the media id of typ uploaded by route; called with w.mu held.
func (w *WeCom) mediaOf(id, typ, route string) (Media, *apiError) {
	m, ok := w.media[id]
	if !ok || m.Type != typ || m.Route != route {
		return Media{}, &apiError{40007, "invalid media_id"}
	}
	return m, nil
}

func (w *WeCom) uploadInit(route string, body json.RawMessage) (map[string]any, *apiError) {
	var p struct {
		Type        string `json:"type"`
		Filename    string `json:"filename"`
		TotalSize   int    `json:"total_size"`
		TotalChunks int    `json:"total_chunks"`
		MD5         string `json:"md5"`
	}
	_ = json.Unmarshal(body, &p)
	limit := maxMediaBytes[p.Type]
	if limit == 0 || p.Filename == "" || len(p.Filename) > 256 || p.TotalSize <= 0 || p.TotalSize > limit ||
		p.TotalChunks < 1 || p.TotalChunks > maxChunks {
		return nil, &apiError{40058, "invalid upload parameters"}
	}
	if w.uploads == nil {
		w.uploads = map[string]*upload{}
	}
	id := randomID()
	w.uploads[id] = &upload{route: route, typ: p.Type, name: p.Filename, md5: p.MD5, size: p.TotalSize, chunks: p.TotalChunks, data: map[int][]byte{}}
	return map[string]any{"upload_id": id}, nil
}

func (w *WeCom) uploadChunk(route string, body json.RawMessage) (map[string]any, *apiError) {
	var p struct {
		UploadID   string `json:"upload_id"`
		ChunkIndex int    `json:"chunk_index"`
		Data       string `json:"base64_data"`
	}
	_ = json.Unmarshal(body, &p)
	u := w.uploads[p.UploadID]
	if u == nil || u.route != route {
		return nil, &apiError{40058, "invalid upload_id"}
	}
	data, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil || len(data) == 0 || len(data) > maxChunkBytes || p.ChunkIndex < 0 || p.ChunkIndex >= u.chunks {
		return nil, &apiError{40058, "invalid chunk"}
	}
	if _, dup := u.data[p.ChunkIndex]; !dup {
		u.data[p.ChunkIndex] = data
	}
	return map[string]any{}, nil
}

func (w *WeCom) uploadFinish(route string, body json.RawMessage) (map[string]any, *apiError) {
	var p struct {
		UploadID string `json:"upload_id"`
	}
	_ = json.Unmarshal(body, &p)
	u := w.uploads[p.UploadID]
	if u == nil || u.route != route {
		return nil, &apiError{40058, "invalid upload_id"}
	}
	var all []byte
	for i := range u.chunks {
		c, ok := u.data[i]
		if !ok {
			return nil, &apiError{40058, "missing chunk"}
		}
		all = append(all, c...)
	}
	sum := md5.Sum(all)
	if len(all) != u.size || u.md5 != "" && !strings.EqualFold(u.md5, hex.EncodeToString(sum[:])) {
		return nil, &apiError{40058, "size or md5 mismatch"}
	}
	delete(w.uploads, p.UploadID)
	if w.media == nil {
		w.media = map[string]Media{}
	}
	id := "media-" + randomID()
	w.media[id] = Media{Route: route, Type: u.typ, Name: u.name, Data: all}
	var created any = w.clock().Unix()
	if route == "websocket" {
		// The long connection gives created_at as a string.
		created = strconv.FormatInt(w.clock().Unix(), 10)
	}
	return map[string]any{"type": u.typ, "media_id": id, "created_at": created}, nil
}

func httpsUpload(step func(*WeCom, string, json.RawMessage) (map[string]any, *apiError)) apiHandler {
	return func(w *WeCom, body json.RawMessage) (map[string]any, *apiError) { return step(w, "https", body) }
}

func websocketUpload(step func(*WeCom, string, json.RawMessage) (map[string]any, *apiError)) apiHandler {
	return func(w *WeCom, body json.RawMessage) (map[string]any, *apiError) { return step(w, "websocket", body) }
}

// frameHandlers answer the long connection's frames that carry a body back
// or are checked; they run with w.mu held, and nil and nil is a plain ok.
var frameHandlers = map[string]apiHandler{
	"aibot_upload_media_init":   websocketUpload((*WeCom).uploadInit),
	"aibot_upload_media_chunk":  websocketUpload((*WeCom).uploadChunk),
	"aibot_upload_media_finish": websocketUpload((*WeCom).uploadFinish),
	"aibot_respond_msg":         (*WeCom).checkMessage,
	"aibot_send_msg":            (*WeCom).checkMessage,
}

// checkMessage refuses a media message whose media_id the long connection
// did not upload.
func (w *WeCom) checkMessage(body json.RawMessage) (map[string]any, *apiError) {
	var p map[string]json.RawMessage
	_ = json.Unmarshal(body, &p)
	var typ string
	_ = json.Unmarshal(p["msgtype"], &typ)
	if _, media := maxMediaBytes[typ]; !media {
		return nil, nil
	}
	var m struct {
		MediaID string `json:"media_id"`
	}
	_ = json.Unmarshal(p[typ], &m)
	if _, e := w.mediaOf(m.MediaID, typ, "websocket"); e != nil {
		return nil, e
	}
	return nil, nil
}
