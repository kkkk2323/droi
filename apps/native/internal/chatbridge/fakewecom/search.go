package fakewecom

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Search: /topic/search makes a job and names the Channels with matches,
// /topic/get_search_result pages a job's matches in one Channel. A post or
// reply matches when its text holds every keyword.

type searchJob struct {
	keywords []string
	topics   []string
}

func (w *WeCom) search(body json.RawMessage) (map[string]any, *apiError) {
	var p struct {
		Keywords []string `json:"keyword_list"`
		Topics   []string `json:"topic_id_list"`
	}
	_ = json.Unmarshal(body, &p)
	if len(p.Keywords) == 0 || len(p.Keywords) > 8 || len(p.Topics) > 8 ||
		slices.ContainsFunc(p.Keywords, func(k string) bool { return k == "" || len(k) > 32 }) {
		return nil, &apiError{40058, "invalid keyword_list"}
	}
	topics := p.Topics
	if len(topics) == 0 {
		for id := range w.channels {
			topics = append(topics, id)
		}
		slices.Sort(topics)
	}
	if w.jobs == nil {
		w.jobs = map[string]searchJob{}
	}
	job := randomID()
	w.jobs[job] = searchJob{keywords: p.Keywords, topics: topics}
	hit := []any{}
	for _, t := range topics {
		if len(w.hits(p.Keywords, t)) > 0 {
			hit = append(hit, map[string]any{"topic_id": t})
		}
	}
	return map[string]any{"jobid": job, "topic_list": hit, "has_more": false, "next_cursor": ""}, nil
}

func (w *WeCom) getSearchResult(body json.RawMessage) (map[string]any, *apiError) {
	var p struct {
		JobID   string `json:"jobid"`
		TopicID string `json:"topic_id"`
		Cursor  string `json:"cursor"`
		Limit   int    `json:"limit"`
	}
	_ = json.Unmarshal(body, &p)
	job, ok := w.jobs[p.JobID]
	if !ok || !slices.Contains(job.topics, p.TopicID) {
		return nil, &apiError{40058, "invalid jobid"}
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 20)
	hits := w.hits(job.keywords, p.TopicID)
	from, _ := strconv.Atoi(p.Cursor)
	from = min(max(from, 0), len(hits))
	to := min(from+limit, len(hits))
	next := ""
	if to < len(hits) {
		next = strconv.Itoa(to)
	}
	return map[string]any{"result_list": hits[from:to], "has_more": next != "", "next_cursor": next}, nil
}

// hits are the posts in topicID that match or have replies that match.
func (w *WeCom) hits(keywords []string, topicID string) []any {
	out := []any{}
	for _, post := range w.posts[topicID] {
		replies := []any{}
		for _, r := range w.replies[replyKey(topicID, post.ID)] {
			if matches(r, keywords) {
				replies = append(replies, map[string]any{"reply_id": r.ID, "brief_content": brief(r.Text)})
			}
		}
		if matches(post, keywords) || len(replies) > 0 {
			out = append(out, map[string]any{"post_id": post.ID, "brief_content": brief(post.Text), "reply_list": replies})
		}
	}
	return out
}

func matches(p *Post, keywords []string) bool {
	if p.Recalled {
		return false
	}
	for _, k := range keywords {
		if !strings.Contains(p.Text, k) {
			return false
		}
	}
	return true
}

var placeholders = regexp.MustCompile(`\$\{[^}]*\}|\x{200b}`)

// brief is WeCom's brief_content: plain text, at most 1024 bytes.
func brief(text string) string {
	s := strings.TrimSpace(placeholders.ReplaceAllString(text, ""))
	for len(s) > 1024 {
		r := []rune(s)
		s = string(r[:len(r)-1])
	}
	return s
}
