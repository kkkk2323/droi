package chatbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Search runs in two steps at WeCom: /topic/search makes a job that names
// the Channels with matches, /topic/get_search_result pages a job's matches
// in one Channel. search_channel searches the Chat Thread's own Channel;
// its cursor carries the job, so a later page does not search again.

const (
	maxSearchKeywords = 8
	maxKeywordBytes   = 32
	searchPage        = 20
)

type searchCursor struct {
	Job    string `json:"j"`
	Cursor string `json:"c"`
}

func (c searchCursor) String() string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (b *Bridge) searchChannel(ctx context.Context, th *chatThread, keywords []string, cursor string) (string, error) {
	if err := th.channelOnly("search_channel"); err != nil {
		return "", err
	}
	var words []string
	for _, k := range keywords {
		if k = strings.TrimSpace(k); k != "" {
			words = append(words, k)
		}
		if len(k) > maxKeywordBytes {
			return "", fmt.Errorf("keyword %q is %d bytes; WeCom takes at most %d", k, len(k), maxKeywordBytes)
		}
	}
	if len(words) == 0 || len(words) > maxSearchKeywords {
		return "", fmt.Errorf("give 1 to %d keywords", maxSearchKeywords)
	}
	api, _, err := b.connection()
	if err != nil {
		return "", err
	}
	b.mu.Lock()
	name := th.channelName
	b.mu.Unlock()
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = fmt.Sprintf("%q", w)
	}
	where := fmt.Sprintf("%s in the WeCom Channel %s", strings.Join(quoted, " "), channelLabel(name, th.topicID))
	var at searchCursor
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &at) != nil || at.Job == "" {
			return "", errors.New("invalid cursor: use one search_channel gave")
		}
	} else {
		var res struct {
			JobID  string `json:"jobid"`
			Topics []struct {
				TopicID string `json:"topic_id"`
			} `json:"topic_list"`
		}
		if err := api.call(ctx, "/topic/search", map[string]any{"keyword_list": words, "topic_id_list": []string{th.topicID}}, &res); err != nil {
			return "", fmt.Errorf("cannot search: %w", err)
		}
		hit := false
		for _, t := range res.Topics {
			hit = hit || t.TopicID == th.topicID
		}
		if !hit || res.JobID == "" {
			return "No matches for " + where + ".", nil
		}
		at.Job = res.JobID
	}
	var res struct {
		Results []struct {
			PostID  string `json:"post_id"`
			Brief   string `json:"brief_content"`
			Replies []struct {
				ReplyID string `json:"reply_id"`
				Brief   string `json:"brief_content"`
			} `json:"reply_list"`
		} `json:"result_list"`
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
	}
	if err := api.call(ctx, "/topic/get_search_result", map[string]any{
		"jobid": at.Job, "topic_id": th.topicID, "cursor": at.Cursor, "limit": searchPage,
	}, &res); err != nil {
		return "", fmt.Errorf("cannot read the matches: %w", err)
	}
	if len(res.Results) == 0 {
		return "No more matches for " + where + ".", nil
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Matches for %s, with a brief of each:\n\n", where)
	for _, r := range res.Results {
		this := ""
		if r.PostID == th.postID {
			this = " (this Chat Thread's post)"
		}
		fmt.Fprintf(&out, "Post %s%s: %s\n", r.PostID, this, oneLine(r.Brief))
		for _, reply := range r.Replies {
			fmt.Fprintf(&out, "- reply %s: %s\n", reply.ReplyID, oneLine(reply.Brief))
		}
		out.WriteString("\n")
	}
	if res.HasMore && res.NextCursor != "" {
		fmt.Fprintf(&out, "More matches: call search_channel with the same keywords and cursor %q.\n", searchCursor{Job: at.Job, Cursor: res.NextCursor})
	} else {
		out.WriteString("No more matches.\n")
	}
	out.WriteString("read_thread with a post_id shows that post with its replies and attachments.")
	return out.String(), nil
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
