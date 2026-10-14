package fakewecom

import (
	"encoding/json"
	"time"
)

// Click is a click on a button of a button_interaction card the robot sent
// in a direct message. MsgID and ReqID are made up when empty.
type Click struct {
	MsgID  string
	ReqID  string
	From   string
	TaskID string
	Key    string
}

type clicked struct {
	taskID string
	at     time.Time
}

// CardUpdateWindow is how long after a click WeCom takes the card's
// aibot_respond_update_msg.
const CardUpdateWindow = 5 * time.Second

// The Fake WeCom's own codes for a card update it refuses; WeCom's are not
// known.
const (
	errNotAClick    = 600001
	errUpdateLate   = 600002
	errWrongCard    = 600003
	errNotACardBody = 600004
)

// Click pushes the template_card_event of a click and returns its req_id,
// which the card's update must be sent on within CardUpdateWindow.
func (w *WeCom) Click(c Click) string {
	w.t.Helper()
	if c.MsgID == "" {
		c.MsgID = randomID()
	}
	if c.ReqID == "" {
		c.ReqID = randomID()
	}
	w.mu.Lock()
	w.clicks[c.ReqID] = clicked{taskID: c.TaskID, at: time.Now()}
	w.mu.Unlock()
	return w.Push("aibot_event_callback", c.ReqID, map[string]any{
		"msgid":        c.MsgID,
		"aibotid":      w.robot.BotID,
		"chattype":     "single",
		"from":         map[string]string{"userid": c.From},
		"msgtype":      "event",
		"response_url": "https://qyapi.weixin.qq.com/cgi-bin/aibot/response?response_code=fake",
		"event": map[string]any{
			"eventtype": "template_card_event",
			"template_card_event": map[string]string{
				"card_type": "button_interaction",
				"event_key": c.Key,
				"task_id":   c.TaskID,
			},
		},
	})
}

// checkCardUpdate answers an aibot_respond_update_msg as WeCom does: only on
// a click's req_id, within CardUpdateWindow, for the card clicked.
func (w *WeCom) checkCardUpdate(reqID string, body json.RawMessage) (int, string) {
	w.mu.Lock()
	c, ok := w.clicks[reqID]
	w.mu.Unlock()
	if !ok {
		return errNotAClick, "req_id is not a card click's"
	}
	if time.Since(c.at) > CardUpdateWindow {
		return errUpdateLate, "the card can no longer be updated"
	}
	var b struct {
		ResponseType string `json:"response_type"`
		Card         struct {
			CardType string `json:"card_type"`
			TaskID   string `json:"task_id"`
			Buttons  []any  `json:"button_list"`
		} `json:"template_card"`
	}
	if json.Unmarshal(body, &b) != nil || b.ResponseType != "update_template_card" || b.Card.CardType != "button_interaction" || len(b.Card.Buttons) == 0 {
		return errNotACardBody, "not a button_interaction card update"
	}
	if b.Card.TaskID != c.taskID {
		return errWrongCard, "task_id is not the clicked card's"
	}
	return 0, "ok"
}
