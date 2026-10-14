package chatbridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// DefaultAPIURL is WeCom's HTTPS API for smart robots: the WeCom Channel's
// posts and replies, search and temporary media.
const DefaultAPIURL = "https://qyapi.weixin.qq.com/cgi-bin/aibot"

const (
	// WeCom's token lives a day; it is fetched again a little before.
	tokenLife   = 24*time.Hour - 5*time.Minute
	apiTimeout  = 30 * time.Second
	maxAPIBytes = 8 << 20
	// WeCom's codes for an expired and an invalid access_token.
	errTokenExpired = 42001
	errTokenInvalid = 40014
)

// channelAPI is WeCom's HTTPS API for the robot. Every call but get_token
// carries a token, which it fetches with a signature of the Secret and
// keeps for its day.
type channelAPI struct {
	base, botID, secret string
	now                 func() time.Time
	client              *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

func newChannelAPI(base, botID, secret string, now func() time.Time) *channelAPI {
	return &channelAPI{base: base, botID: botID, secret: secret, now: now, client: &http.Client{Timeout: apiTimeout}}
}

// call posts body to path (such as "/topic/send_reply") and decodes the
// answer into out, which may be nil. A token WeCom calls expired or invalid
// is fetched again, once.
func (a *channelAPI) call(ctx context.Context, path string, body, out any) error {
	for retried := false; ; retried = true {
		token, err := a.accessToken(ctx)
		if err != nil {
			return err
		}
		err = a.post(ctx, path, token, body, out)
		var we *WeComError
		if !retried && errors.As(err, &we) && (we.Code == errTokenExpired || we.Code == errTokenInvalid) {
			a.forget(token)
			continue
		}
		return err
	}
}

func (a *channelAPI) forget(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token == token {
		a.token = ""
	}
}

// accessToken is the cached token, fetched when there is none or its day is
// over; one fetch runs at a time.
func (a *channelAPI) accessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if a.token != "" && now.Before(a.expires) {
		return a.token, nil
	}
	n := make([]byte, 8)
	_, _ = rand.Read(n)
	nonce, t := hex.EncodeToString(n), now.Unix()
	sum := sha256.Sum256([]byte(a.secret + a.botID + strconv.FormatInt(t, 10) + nonce))
	var res struct {
		AccessToken string `json:"access_token"`
	}
	err := a.post(ctx, "/get_token", "", map[string]any{
		"bot_id": a.botID, "time": t, "nonce": nonce, "signature": hex.EncodeToString(sum[:]),
	}, &res)
	if err == nil && res.AccessToken == "" {
		err = errors.New("no access_token")
	}
	if err != nil {
		return "", fmt.Errorf("WeCom gave no token: %w", err)
	}
	a.token, a.expires = res.AccessToken, now.Add(tokenLife)
	return a.token, nil
}

// post makes one call. Its errors never carry the URL, which holds the
// token.
func (a *channelAPI) post(ctx context.Context, path, token string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	u := a.base + path
	if token != "" {
		u += "?access_token=" + url.QueryEscape(token)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: invalid API URL", path)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("cannot reach WeCom's API for %s: %w", path, err)
	}
	defer res.Body.Close()
	data, err = io.ReadAll(io.LimitReader(res.Body, maxAPIBytes))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("WeCom's API answered %s with HTTP %d", path, res.StatusCode)
	}
	var status struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return fmt.Errorf("WeCom's API answered %s with no JSON", path)
	}
	if status.Errcode != 0 {
		return &WeComError{Code: status.Errcode, Message: status.Errmsg}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

// member is WeCom's UserInfo: a member (type 0) or a robot (type 1).
type member struct {
	Type   int    `json:"type"`
	UserID string `json:"userid"`
	BotID  string `json:"aibotid"`
}

// content is a post (PostInfo) or a reply (ReplyInfo).
type content struct {
	PostID        string `json:"post_id"`
	ReplyID       string `json:"reply_id"`
	ParentReplyID string `json:"parent_reply_id"`
	Creator       member `json:"creator"`
	// CreatorInfo is the name one version of WeCom's docs gives creator.
	CreatorInfo member `json:"creator_info"`
	CreateTime  int64  `json:"create_time"`
	IsRecalled  bool   `json:"is_recalled"`
	IsModified  bool   `json:"is_modified"`
	Content     string `json:"content"`
	Files       []struct {
		Placeholder string `json:"placeholder"`
		FileName    string `json:"file_name"`
		attachmentLink
	} `json:"inline_file_list"`
	Images   []attachmentLink `json:"image_list"`
	Mentions []struct {
		Placeholder string `json:"placeholder"`
		Member      member `json:"member"`
	} `json:"inline_mention_list"`
}

// attachmentLink is where a Channel file or image downloads, for an hour,
// with its cookie.
type attachmentLink struct {
	URL         string `json:"download_url"`
	CookieName  string `json:"cookie_name"`
	CookieValue string `json:"cookie_value"`
}

func (c content) author() member {
	if c.Creator.UserID == "" && c.Creator.BotID == "" {
		return c.CreatorInfo
	}
	return c.Creator
}

// channelName is the name of the WeCom Channel topicID.
func (a *channelAPI) channelName(ctx context.Context, topicID string) (string, error) {
	var res struct {
		Topics []struct {
			TopicID string `json:"topic_id"`
			Name    string `json:"topic_name"`
		} `json:"topic_list"`
	}
	if err := a.call(ctx, "/topic/get_topic_list", map[string]any{"type": 0, "topic_id_list": []string{topicID}}, &res); err != nil {
		return "", err
	}
	for _, t := range res.Topics {
		if t.TopicID == topicID {
			return t.Name, nil
		}
	}
	return "", errors.New("the robot is not in the WeCom Channel")
}

// getPost is one post of a WeCom Channel.
func (a *channelAPI) getPost(ctx context.Context, topicID, postID string) (content, error) {
	var res struct {
		Posts []content `json:"post_list"`
	}
	if err := a.call(ctx, "/topic/get_post_list", map[string]any{"topic_id": topicID, "type": 0, "post_id_list": []string{postID}}, &res); err != nil {
		return content{}, err
	}
	for _, p := range res.Posts {
		if p.PostID == postID {
			return p, nil
		}
	}
	return content{}, errors.New("WeCom has no such post")
}

// getReply is one reply under a post.
func (a *channelAPI) getReply(ctx context.Context, topicID, postID, replyID string) (content, error) {
	var res struct {
		Replies []content `json:"reply_list"`
	}
	if err := a.call(ctx, "/topic/get_reply_list", map[string]any{
		"topic_id": topicID, "post_id": postID, "type": 0, "reply_id_list": []string{replyID},
	}, &res); err != nil {
		return content{}, err
	}
	for _, r := range res.Replies {
		if r.ReplyID == replyID {
			return r, nil
		}
	}
	return content{}, errors.New("WeCom has no such reply")
}

// replyPage is a page of the replies under a post, oldest first, and the
// cursor of the next page, "" after the last.
func (a *channelAPI) replyPage(ctx context.Context, topicID, postID, cursor string, limit int) ([]content, string, error) {
	var res struct {
		Replies    []content `json:"reply_list"`
		HasMore    bool      `json:"has_more"`
		NextCursor string    `json:"next_cursor"`
	}
	if err := a.call(ctx, "/topic/get_reply_list", map[string]any{
		"topic_id": topicID, "post_id": postID, "type": 1, "sort_type": 0, "cursor": cursor, "limit": limit,
	}, &res); err != nil {
		return nil, "", err
	}
	if !res.HasMore {
		res.NextCursor = ""
	}
	return res.Replies, res.NextCursor, nil
}

// sendReply posts Markdown under a post and returns the reply's id; extra
// adds the files, images and mentions its placeholders name.
func (a *channelAPI) sendReply(ctx context.Context, topicID, postID, text string, extra map[string]any) (string, error) {
	var res struct {
		ReplyID string `json:"reply_id"`
	}
	body := map[string]any{"topic_id": topicID, "post_id": postID, "content": text}
	for k, v := range extra {
		body[k] = v
	}
	err := a.call(ctx, "/topic/send_reply", body, &res)
	return res.ReplyID, err
}
