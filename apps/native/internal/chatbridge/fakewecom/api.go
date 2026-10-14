package fakewecom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The Fake WeCom's HTTPS API stands in for qyapi.weixin.qq.com/cgi-bin/aibot:
// get_token, and the Channel's posts and replies. APIURL is its base, for
// the Chat Bridge's settable API URL. Tokens live a day by the Fake WeCom's
// clock (SetClock); a call with an unknown one gets 853005, as the real
// aibot API answers, and an expired one 42001, as WeCom's other APIs do.

// Post is a post or a reply in a WeCom Channel. ID and Time are made up
// when empty.
type Post struct {
	ID string
	// From is the member who wrote it; Robot the robot's aibotid when a
	// robot did.
	From  string
	Robot string
	Time  time.Time
	// Text is Markdown with WeCom's placeholders: ${inline_mention:ph},
	// ${inline_file:ph}, ${inline_mention_all}.
	Text     string
	Mentions []Mention
	Files    []File
	Images   []Image
	Recalled bool
	// ParentReplyID is the reply a reply answers, "" for one under the post.
	ParentReplyID string
}

// Mention is an @ in a post: of a member (UserID) or a robot (BotID).
type Mention struct {
	Placeholder string
	UserID      string
	BotID       string
}

// File is a file in a post.
type File struct {
	Placeholder string
	Name        string
	Data        []byte
}

// APICall is a call the Chat Bridge made to the HTTPS API, its token
// left out.
type APICall struct {
	Path string
	Body json.RawMessage
}

// AtBot is an @ of the robot in a Channel post (ReplyID "") or a reply.
// MsgID and ReqID are made up when empty.
type AtBot struct {
	MsgID   string
	ReqID   string
	TopicID string
	PostID  string
	ReplyID string
	From    string
}

const tokenLife = 24 * time.Hour

type apiError struct {
	code int
	msg  string
}

// apiHandler answers one API path with w.mu held.
type apiHandler func(w *WeCom, body json.RawMessage) (map[string]any, *apiError)

var apiHandlers = map[string]apiHandler{
	"/topic/get_topic_list":    (*WeCom).getTopicList,
	"/topic/get_post_list":     (*WeCom).getPostList,
	"/topic/get_reply_list":    (*WeCom).getReplyList,
	"/topic/send_reply":        (*WeCom).sendReply,
	"/topic/search":            (*WeCom).search,
	"/topic/get_search_result": (*WeCom).getSearchResult,
	"/upload_media_init":       httpsUpload((*WeCom).uploadInit),
	"/upload_media_chunk":      httpsUpload((*WeCom).uploadChunk),
	"/upload_media_finish":     httpsUpload((*WeCom).uploadFinish),
	"/topic/post_emoji":        (*WeCom).postEmoji,
}

// SetClock makes now the Fake WeCom's clock, which get_token's time is
// checked against and tokens expire by.
func (w *WeCom) SetClock(now func() time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.now = now
}

func (w *WeCom) clock() time.Time {
	if w.now == nil {
		return time.Now()
	}
	return w.now()
}

// AddChannel adds a WeCom Channel the robot is in.
func (w *WeCom) AddChannel(topicID, name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.channels[topicID] = name
}

// AddPost adds a post to a Channel and returns its id.
func (w *WeCom) AddPost(topicID string, p Post) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	p = w.fill(p)
	w.posts[topicID] = append(w.posts[topicID], &p)
	return p.ID
}

// AddReply adds a reply under a post and returns its id.
func (w *WeCom) AddReply(topicID, postID string, r Post) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	r = w.fill(r)
	k := replyKey(topicID, postID)
	w.replies[k] = append(w.replies[k], &r)
	return r.ID
}

func (w *WeCom) fill(p Post) Post {
	if p.ID == "" {
		p.ID = randomID()
	}
	if p.Time.IsZero() {
		p.Time = w.clock()
	}
	return p
}

func replyKey(topicID, postID string) string { return topicID + "\x00" + postID }

// Replies are the replies under a post, the robot's among them.
func (w *WeCom) Replies(topicID, postID string) []Post {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []Post
	for _, r := range w.replies[replyKey(topicID, postID)] {
		out = append(out, *r)
	}
	return out
}

// APICalls are the calls to path (such as "/topic/send_reply") so far.
func (w *WeCom) APICalls(path string) []APICall {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []APICall
	for _, c := range w.calls {
		if c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

// FailAPI answers the next call to path with a WeCom error.
func (w *WeCom) FailAPI(path string, errcode int, errmsg string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.apiFail[path] = append(w.apiFail[path], failure{errcode, errmsg})
}

// ExpireTokens makes every token issued so far invalid, as WeCom may
// before its day is over.
func (w *WeCom) ExpireTokens() {
	w.mu.Lock()
	defer w.mu.Unlock()
	clear(w.tokens)
}

// AtBot pushes the topic_at_bot event of an @ of the robot in a Channel and
// returns its req_id.
func (w *WeCom) AtBot(a AtBot) string {
	w.t.Helper()
	if a.MsgID == "" {
		a.MsgID = randomID()
	}
	at := map[string]string{"post_id": a.PostID}
	if a.ReplyID != "" {
		at["reply_id"] = a.ReplyID
	}
	w.mu.Lock()
	now := w.clock()
	w.mu.Unlock()
	return w.Push("aibot_event_callback", a.ReqID, map[string]any{
		"msgid":       a.MsgID,
		"aibotid":     w.robot.BotID,
		"from":        map[string]string{"userid": a.From},
		"msgtype":     "event",
		"create_time": now.Unix(),
		"event":       map[string]any{"eventtype": "topic_at_bot", "topic_at_bot": at},
		"topic_id":    a.TopicID,
	})
}

// GroupCopy pushes the copy WeCom makes of a Channel @ for robots written
// for group chats (compat_info.source channel), and returns its req_id.
func (w *WeCom) GroupCopy(from, text string) string {
	w.t.Helper()
	return w.Push("aibot_msg_callback", "", map[string]any{
		"msgid":        randomID(),
		"aibotid":      w.robot.BotID,
		"chatid":       "op7_" + randomID(),
		"chattype":     "group",
		"from":         map[string]string{"userid": from},
		"msgtype":      "text",
		"response_url": "https://qyapi.weixin.qq.com/cgi-bin/aibot/response?response_code=fake",
		"text":         map[string]string{"content": text},
		"compat_info":  map[string]string{"source": "channel"},
	})
}

func (w *WeCom) serveAPI(rw http.ResponseWriter, r *http.Request) {
	if w.serveDownload(rw, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(rw, "POST only", http.StatusMethodNotAllowed)
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}
	body := json.RawMessage(data)
	if !json.Valid(body) {
		body = json.RawMessage("null")
	}
	w.mu.Lock()
	w.calls = append(w.calls, APICall{Path: r.URL.Path, Body: body})
	out, e := w.answer(r, body)
	w.notify()
	w.mu.Unlock()
	if out == nil && e == nil {
		http.NotFound(rw, r)
		return
	}
	if e != nil {
		out = map[string]any{"errcode": e.code, "errmsg": e.msg}
	} else {
		out["errcode"], out["errmsg"] = 0, "ok"
	}
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(out)
}

// answer is called with w.mu held; nil and nil is an unknown path.
func (w *WeCom) answer(r *http.Request, body json.RawMessage) (map[string]any, *apiError) {
	path := r.URL.Path
	handler, known := apiHandlers[path]
	if !known && path != "/get_token" {
		return nil, nil
	}
	if q := w.apiFail[path]; len(q) > 0 {
		w.apiFail[path] = q[1:]
		return nil, &apiError{q[0].errcode, q[0].errmsg}
	}
	if path == "/get_token" {
		return w.getToken(body)
	}
	token := r.URL.Query().Get("access_token")
	expires, ok := w.tokens[token]
	switch {
	case token == "":
		return nil, &apiError{41001, "access_token missing"}
	case !ok:
		return nil, &apiError{853005, "cli token invalid, please re-fetch"}
	case !w.clock().Before(expires):
		return nil, &apiError{42001, "access_token expired"}
	}
	return handler(w, body)
}

func (w *WeCom) getToken(body json.RawMessage) (map[string]any, *apiError) {
	var p struct {
		BotID     string `json:"bot_id"`
		Time      int64  `json:"time"`
		Nonce     string `json:"nonce"`
		Signature string `json:"signature"`
	}
	_ = json.Unmarshal(body, &p)
	sum := sha256.Sum256([]byte(w.robot.Secret + p.BotID + strconv.FormatInt(p.Time, 10) + p.Nonce))
	now := w.clock()
	skew := now.Sub(time.Unix(p.Time, 0))
	if p.BotID != w.robot.BotID || p.Signature != hex.EncodeToString(sum[:]) ||
		p.Nonce == "" || len(p.Nonce) > 64 || skew > time.Minute || skew < -time.Minute {
		return nil, &apiError{853000, "invalid bot_id or secret"}
	}
	token := "tok-" + randomID()
	w.tokens[token] = now.Add(tokenLife)
	return map[string]any{"access_token": token}, nil
}

func (w *WeCom) getTopicList(body json.RawMessage) (map[string]any, *apiError) {
	var p struct {
		Type    int      `json:"type"`
		TopicID []string `json:"topic_id_list"`
	}
	_ = json.Unmarshal(body, &p)
	list := []any{}
	for id, name := range w.channels {
		if p.Type == 1 || slices.Contains(p.TopicID, id) {
			list = append(list, map[string]any{
				"topic_id": id, "topic_name": name, "topic_desc": "",
				"creator": map[string]any{"type": 0, "userid": "HuangJunXiang"}, "permission": 0, "create_time": 1791954607,
			})
		}
	}
	return map[string]any{"topic_list": list, "has_more": false}, nil
}

// listQuery is what get_post_list and get_reply_list take.
type listQuery struct {
	TopicID  string   `json:"topic_id"`
	PostID   string   `json:"post_id"`
	Type     int      `json:"type"`
	PostIDs  []string `json:"post_id_list"`
	ReplyIDs []string `json:"reply_id_list"`
	SortType int      `json:"sort_type"`
	Cursor   string   `json:"cursor"`
	Limit    int      `json:"limit"`
}

func (w *WeCom) getPostList(body json.RawMessage) (map[string]any, *apiError) {
	var q listQuery
	_ = json.Unmarshal(body, &q)
	if _, ok := w.channels[q.TopicID]; !ok {
		return nil, &apiError{40058, "invalid topic_id"}
	}
	list, more, next := w.list(w.posts[q.TopicID], q, q.PostIDs, "post_id")
	return map[string]any{"post_list": list, "has_more": more, "next_cursor": next}, nil
}

func (w *WeCom) getReplyList(body json.RawMessage) (map[string]any, *apiError) {
	var q listQuery
	_ = json.Unmarshal(body, &q)
	if !w.hasPost(q.TopicID, q.PostID) {
		return nil, &apiError{40058, "invalid post_id"}
	}
	list, more, next := w.list(w.replies[replyKey(q.TopicID, q.PostID)], q, q.ReplyIDs, "reply_id")
	return map[string]any{"reply_list": list, "has_more": more, "next_cursor": next}, nil
}

func (w *WeCom) hasPost(topicID, postID string) bool {
	return slices.ContainsFunc(w.posts[topicID], func(p *Post) bool { return p.ID == postID })
}

// list is type 0 (by id) or type 1 (paged, from cursor) of items.
func (w *WeCom) list(items []*Post, q listQuery, ids []string, idKey string) ([]any, bool, string) {
	out := []any{}
	if q.Type == 0 {
		for _, p := range items {
			if slices.Contains(ids, p.ID) {
				out = append(out, w.info(p, idKey))
			}
		}
		return out, false, ""
	}
	items = slices.Clone(items)
	if q.SortType == 1 {
		slices.Reverse(items)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 50)
	from, _ := strconv.Atoi(q.Cursor)
	from = min(max(from, 0), len(items))
	to := min(from+limit, len(items))
	for _, p := range items[from:to] {
		out = append(out, w.info(p, idKey))
	}
	if to < len(items) {
		return out, true, strconv.Itoa(to)
	}
	return out, false, ""
}

func member(userID, botID string) map[string]any {
	if botID != "" {
		return map[string]any{"type": 1, "aibotid": botID}
	}
	return map[string]any{"type": 0, "userid": userID}
}

// info is a post or reply as WeCom's PostInfo / ReplyInfo, with fresh
// download links for its files and images.
func (w *WeCom) info(p *Post, idKey string) map[string]any {
	mentions := []any{}
	for _, m := range p.Mentions {
		mentions = append(mentions, map[string]any{"placeholder": m.Placeholder, "member": member(m.UserID, m.BotID)})
	}
	files := []any{}
	for _, f := range p.Files {
		file := w.channelLink(f.Data)
		file["placeholder"], file["file_name"] = f.Placeholder, f.Name
		files = append(files, file)
	}
	images := []any{}
	for _, i := range p.Images {
		images = append(images, w.channelLink(i.Data))
	}
	out := map[string]any{
		idKey:                 p.ID,
		"creator":             member(p.From, p.Robot),
		"create_time":         p.Time.Unix(),
		"is_recalled":         p.Recalled,
		"is_modified":         false,
		"content":             p.Text,
		"inline_file_list":    files,
		"inline_mention_list": mentions,
		"image_list":          images,
		"emoji_list":          []any{},
	}
	if idKey == "reply_id" {
		out["parent_reply_id"] = p.ParentReplyID
	}
	return out
}

func (w *WeCom) sendReply(body json.RawMessage) (map[string]any, *apiError) {
	var p struct {
		TopicID       string `json:"topic_id"`
		PostID        string `json:"post_id"`
		ParentReplyID string `json:"parent_reply_id"`
		Content       string `json:"content"`
		Mentions      []struct {
			Placeholder string `json:"placeholder"`
			Member      struct {
				Type   int    `json:"type"`
				UserID string `json:"userid"`
			} `json:"member"`
		} `json:"inline_mention_list"`
		Files []struct {
			Placeholder string `json:"placeholder"`
			MediaID     string `json:"media_id"`
		} `json:"inline_file_list"`
		Images []struct {
			MediaID string `json:"media_id"`
		} `json:"image_list"`
	}
	_ = json.Unmarshal(body, &p)
	if !w.hasPost(p.TopicID, p.PostID) {
		return nil, &apiError{40058, "invalid post_id"}
	}
	if p.Content == "" || len(p.Content) > 20480 {
		return nil, &apiError{40058, "invalid content"}
	}
	if len(p.Mentions) > maxAttachments || len(p.Files) > maxAttachments || len(p.Images) > maxAttachments {
		return nil, &apiError{40058, "too many mentions, files or images"}
	}
	r := w.fill(Post{Robot: w.robot.BotID, Text: p.Content, ParentReplyID: p.ParentReplyID})
	for _, m := range p.Mentions {
		// Only members can be @-ed, each where the content holds its placeholder.
		if m.Member.Type != 0 || m.Member.UserID == "" || !strings.Contains(p.Content, "${inline_mention:"+m.Placeholder+"}") {
			return nil, &apiError{40058, "invalid inline_mention_list"}
		}
		r.Mentions = append(r.Mentions, Mention{Placeholder: m.Placeholder, UserID: m.Member.UserID})
	}
	for _, f := range p.Files {
		media, e := w.mediaOf(f.MediaID, "file", "https")
		if e != nil {
			return nil, e
		}
		if !strings.Contains(p.Content, "${inline_file:"+f.Placeholder+"}") {
			return nil, &apiError{40058, "invalid inline_file_list"}
		}
		r.Files = append(r.Files, File{Placeholder: f.Placeholder, Name: media.Name, Data: media.Data})
	}
	for _, i := range p.Images {
		media, e := w.mediaOf(i.MediaID, "image", "https")
		if e != nil {
			return nil, e
		}
		r.Images = append(r.Images, Image{Data: media.Data})
	}
	k := replyKey(p.TopicID, p.PostID)
	w.replies[k] = append(w.replies[k], &r)
	return map[string]any{"reply_id": r.ID}, nil
}
