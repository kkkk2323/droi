package fakewecom

import (
	"encoding/json"
	"slices"
)

// Emoji are the emoji the robot put on a post (replyID "") or a reply, in
// the order it put them.
func (w *WeCom) Emoji(topicID, postID, replyID string) []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.emoji[emojiKey(topicID, postID, replyID)])
}

func emojiKey(topicID, postID, replyID string) string {
	return topicID + "\x00" + postID + "\x00" + replyID
}

// postEmoji puts (action 1) or removes (action 0) one of WeCom's emoji,
// 1 to 115. Putting one twice or removing one that is not there changes
// nothing.
func (w *WeCom) postEmoji(body json.RawMessage) (map[string]any, *apiError) {
	var p struct {
		TopicID   string `json:"topic_id"`
		PostID    string `json:"post_id"`
		ReplyID   string `json:"reply_id"`
		EmojiType int    `json:"emoji_type"`
		Action    *int   `json:"action"`
	}
	_ = json.Unmarshal(body, &p)
	if !w.hasPost(p.TopicID, p.PostID) {
		return nil, &apiError{40058, "invalid post_id"}
	}
	if p.ReplyID != "" && !slices.ContainsFunc(w.replies[replyKey(p.TopicID, p.PostID)], func(r *Post) bool { return r.ID == p.ReplyID }) {
		return nil, &apiError{40058, "invalid reply_id"}
	}
	if p.EmojiType < 1 || p.EmojiType > 115 || p.Action == nil || (*p.Action != 0 && *p.Action != 1) {
		return nil, &apiError{40058, "invalid emoji_type or action"}
	}
	if w.emoji == nil {
		w.emoji = map[string][]int{}
	}
	k := emojiKey(p.TopicID, p.PostID, p.ReplyID)
	if i := slices.Index(w.emoji[k], p.EmojiType); *p.Action == 0 && i >= 0 {
		w.emoji[k] = slices.Delete(w.emoji[k], i, i+1)
	} else if *p.Action == 1 && i < 0 {
		w.emoji[k] = append(w.emoji[k], p.EmojiType)
	}
	return map[string]any{}, nil
}
