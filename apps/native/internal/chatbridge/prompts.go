package chatbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/transcript"
)

// Prompts from WeCom: the Chat Bridge shows its Chat Sessions' Prompts in
// their Chat Threads and answers them with what an allowed user picks there.
// A permission request is a card in a direct message and numbered text in a
// WeCom Channel; an AskUser is numbered text in both. The Daemon takes the
// first answer from anywhere (ADR 0003): a Prompt answered in a Client first
// is marked settled in its Chat Thread. Nothing answers a Prompt by itself.

// prompt is a Prompt shown in a Chat Thread.
type prompt struct {
	requestID string
	th        *thread
	perm      *controller.Permission
	ask       *controller.AskUser
	seq       int
	// taskID is the card's, for a permission request in a direct message.
	taskID string
	// answers are the AskUser's questions answered so far, which are asked
	// one at a time.
	answers []protocol.AskUserCollectedAnswer
	// answering: an answer from WeCom is on its way to the Daemon.
	answering bool
	settled   bool
	// choice is what the Prompt was answered with and by the WeCom user
	// who did; by is "" when a Client answered.
	choice, by string
	// resolved is the option the Daemon said it took, when it said one.
	resolved string
}

// promptBook is the Prompts shown in Chat Threads, guarded by Bridge.mu.
type promptBook struct {
	seq  int
	open map[string]*prompt
	// cards are by task id; a settled one stays, for a later click to show
	// how it was settled.
	cards map[string]*prompt
}

const (
	// cardTimeout keeps a click's answer and the card's update inside
	// WeCom's 5 s.
	cardTimeout = 4 * time.Second
	// WeCom's limits on a button_interaction card.
	maxCardButtons    = 6
	maxButtonRunes    = 10
	maxCardTitleRunes = 26
	maxQuoteRunes     = 600
)

// promptEvent follows the Controller's Prompts.
func (b *Bridge) promptEvent(e controller.Event) {
	switch e := e.(type) {
	case controller.PermissionRequested:
		perm := e.Permission
		b.asked(&prompt{requestID: perm.RequestID, perm: &perm}, perm.AssociatedSessionIDs)
	case controller.AskUserRequested:
		ask := e.AskUser
		b.asked(&prompt{requestID: ask.RequestID, ask: &ask}, []string{ask.SessionID})
	case controller.PermissionResolved:
		b.resolvedElsewhere(e.RequestID, string(e.SelectedOption))
	case controller.AskUserResolved:
		b.resolvedElsewhere(e.RequestID, "")
	}
}

// asked shows a Prompt of a Chat Session in its Chat Thread.
func (b *Bridge) asked(p *prompt, sessions []string) {
	b.mu.Lock()
	if b.prompts.open == nil {
		b.prompts.open, b.prompts.cards = map[string]*prompt{}, map[string]*prompt{}
	}
	if b.prompts.open[p.requestID] != nil {
		b.mu.Unlock()
		return
	}
	for _, th := range b.threads {
		if th.sessionID != "" && slices.Contains(sessions, th.sessionID) {
			p.th = th
		}
	}
	if p.th == nil {
		b.mu.Unlock()
		return
	}
	b.prompts.seq++
	p.seq = b.prompts.seq
	if p.perm != nil && p.th.kind == threadKindDirect {
		p.taskID = newTaskID()
		b.prompts.cards[p.taskID] = p
	}
	b.prompts.open[p.requestID] = p
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	var err error
	switch {
	case p.perm != nil && p.th.kind == threadKindDirect:
		err = b.sendDirect(ctx, p.th, map[string]any{"msgtype": "template_card", "template_card": permissionCard(p)})
	case p.perm != nil:
		err = b.sendThread(ctx, p.th, permissionText(p))
	default:
		err = b.sendThread(ctx, p.th, questionText(p, 0))
	}
	if err != nil {
		b.o.Log("cannot show a Prompt in WeCom: " + err.Error())
	}
}

// resolvedElsewhere hears that the Daemon has a Prompt's answer; when it
// was not WeCom's, the Chat Thread is told where it went.
func (b *Bridge) resolvedElsewhere(requestID, option string) {
	b.mu.Lock()
	p := b.prompts.open[requestID]
	if p == nil {
		b.mu.Unlock()
		return
	}
	delete(b.prompts.open, requestID)
	p.resolved = option
	if p.answering || p.settled {
		b.mu.Unlock()
		return
	}
	p.settle("")
	note := settledNote(p)
	b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	if err := b.sendThread(ctx, p.th, note); err != nil {
		b.o.Log("cannot tell WeCom a Prompt was answered: " + err.Error())
	}
}

// settle marks the Prompt answered by the WeCom user by, or (by "") by a
// Client with the option the Daemon said; b.mu is held.
func (p *prompt) settle(by string) {
	p.settled, p.answering, p.by = true, false, by
	if by == "" && p.perm != nil {
		p.choice = ""
		for _, o := range p.perm.Options {
			if string(o.Value) == p.resolved {
				p.choice = o.Label
			}
		}
	}
}

// answerInThread answers the Chat Thread's oldest waiting Prompt with what
// an allowed user wrote there, and reports whether it did. A permission
// request (only in a Channel, where it has no card) takes a number alone;
// an AskUser takes a number or the text as the answer. Anything else is a
// message for Droid.
func (b *Bridge) answerInThread(th *thread, user, text string) bool {
	if th.kind == threadKindChannel {
		text = strings.ReplaceAll(text, "@you", " ")
	}
	text = strings.TrimSpace(text)
	b.mu.Lock()
	var p *prompt
	for _, q := range b.prompts.open {
		if q.th != th || q.answering || q.settled || q.perm != nil && th.kind != threadKindChannel {
			continue
		}
		if p == nil || q.seq < p.seq {
			p = q
		}
	}
	if p == nil {
		b.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	if p.perm != nil {
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > len(p.perm.Options) {
			b.mu.Unlock()
			return false
		}
		opt := p.perm.Options[n-1]
		p.answering = true
		b.mu.Unlock()
		err = b.ctl.RespondToPermission(ctx, p.requestID, controller.PermissionAnswer{SelectedOption: opt.Value})
		b.answered(ctx, p, user, fmt.Sprintf("%d. %s", n, opt.Label), err)
		return true
	}
	q := p.ask.Questions[len(p.answers)]
	answer := askAnswer(q, text)
	if answer == "" {
		b.mu.Unlock()
		return false
	}
	p.answers = append(p.answers, protocol.AskUserCollectedAnswer{Index: q.Index, Question: q.Question, Answer: answer})
	if len(p.answers) < len(p.ask.Questions) {
		next := questionText(p, len(p.answers))
		b.mu.Unlock()
		if err := b.sendThread(ctx, th, next); err != nil {
			b.o.Log("cannot ask the next question in WeCom: " + err.Error())
		}
		return true
	}
	p.answering = true
	answers := slices.Clone(p.answers)
	b.mu.Unlock()
	err := b.ctl.RespondToAskUser(ctx, p.requestID, protocol.AskUserResult{Answers: answers})
	b.answered(ctx, p, user, answer, err)
	return true
}

// answered settles a Prompt WeCom answered and tells the Chat Thread: who
// answered a permission request in a Channel, or that a Client was first.
func (b *Bridge) answered(ctx context.Context, p *prompt, user, choice string, err error) {
	b.mu.Lock()
	note := ""
	switch {
	case err == nil:
		p.settle(user)
		p.choice = choice
		if p.perm != nil && p.th.kind == threadKindChannel {
			note = settledNote(p)
		}
	case errors.Is(err, controller.ErrPromptNotFound):
		p.settle("")
		note = settledNote(p)
	default:
		p.answering = false
		if p.ask != nil {
			p.answers = p.answers[:len(p.answers)-1]
		}
		note = "The answer did not reach Droid: " + err.Error()
	}
	th := p.th
	b.mu.Unlock()
	if note == "" {
		return
	}
	if err := b.sendThread(ctx, th, note); err != nil {
		b.o.Log("cannot tell WeCom about an answer: " + err.Error())
	}
}

// cardEvent is the body of a template_card_event.
type cardEvent struct {
	MsgID string `json:"msgid"`
	From  struct {
		UserID string `json:"userid"`
	} `json:"from"`
	Event struct {
		Type  string `json:"eventtype"`
		Click struct {
			Key    string `json:"event_key"`
			TaskID string `json:"task_id"`
		} `json:"template_card_event"`
	} `json:"event"`
}

func isCardClick(f frame) bool {
	if f.Cmd != "aibot_event_callback" {
		return false
	}
	var e cardEvent
	return json.Unmarshal(f.Body, &e) == nil && e.Event.Type == "template_card_event"
}

// answerClick answers a permission request with the option an allowed user
// clicked on its card, and updates the card to show the choice and who made
// it. A click on a card already settled only shows how it was.
func (b *Bridge) answerClick(f frame) {
	var e cardEvent
	if json.Unmarshal(f.Body, &e) != nil || e.From.UserID == "" {
		return
	}
	user, click := e.From.UserID, e.Event.Click
	if !b.admit(e.MsgID, user) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cardTimeout)
	defer cancel()
	b.mu.Lock()
	p := b.prompts.cards[click.TaskID]
	if p == nil {
		b.mu.Unlock()
		b.updateCard(ctx, f.Headers.ReqID, goneCard(click.TaskID))
		return
	}
	n, err := strconv.Atoi(click.Key)
	if p.settled || p.answering || err != nil || n < 1 || n > min(len(p.perm.Options), maxCardButtons) {
		card := settledCard(p)
		b.mu.Unlock()
		if p.settled {
			b.updateCard(ctx, f.Headers.ReqID, card)
		}
		return
	}
	opt := p.perm.Options[n-1]
	p.answering = true
	b.mu.Unlock()
	err = b.ctl.RespondToPermission(ctx, p.requestID, controller.PermissionAnswer{SelectedOption: opt.Value})
	b.mu.Lock()
	note := ""
	switch {
	case err == nil:
		p.settle(user)
		p.choice = opt.Label
	case errors.Is(err, controller.ErrPromptNotFound):
		p.settle("")
	default:
		p.answering = false
		note = "The answer did not reach Droid: " + err.Error()
	}
	card := settledCard(p)
	settled := p.settled
	b.mu.Unlock()
	if settled {
		b.updateCard(ctx, f.Headers.ReqID, card)
		return
	}
	if err := b.sendDirect(ctx, p.th, markdown(note)); err != nil {
		b.o.Log("cannot tell WeCom about an answer: " + err.Error())
	}
}

func (b *Bridge) updateCard(ctx context.Context, reqID string, card map[string]any) {
	b.mu.Lock()
	wc := b.wecom
	b.mu.Unlock()
	if wc == nil {
		return
	}
	if _, err := wc.call(ctx, "aibot_respond_update_msg", reqID, map[string]any{"response_type": "update_template_card", "template_card": card}); err != nil {
		b.o.Log("cannot update a card in WeCom: " + err.Error())
	}
}

// sendThread sends Markdown to the Chat Thread: a reply under the post in a
// Channel, a message in a direct message.
func (b *Bridge) sendThread(ctx context.Context, th *thread, text string) error {
	if th.kind != threadKindChannel {
		return b.sendDirect(ctx, th, markdown(text))
	}
	b.mu.Lock()
	api := b.api
	b.mu.Unlock()
	if api == nil {
		return errNotConnected
	}
	_, err := api.sendReply(ctx, th.topicID, th.postID, text)
	return err
}

// sendDirect sends a message body to a direct message, under the latest
// message's req_id while WeCom takes replies to it.
func (b *Bridge) sendDirect(ctx context.Context, th *thread, body map[string]any) error {
	b.mu.Lock()
	wc, reqID, at, user := b.wecom, th.reqID, th.reqAt, th.userID
	b.mu.Unlock()
	if wc == nil {
		return errNotConnected
	}
	if reqID != "" && b.o.Now().Sub(at) < replyWindow {
		_, err := wc.call(ctx, "aibot_respond_msg", reqID, body)
		return err
	}
	push := map[string]any{"chatid": user, "chat_type": 1}
	for k, v := range body {
		push[k] = v
	}
	_, err := wc.call(ctx, "aibot_send_msg", "", push)
	return err
}

func markdown(text string) map[string]any {
	return map[string]any{"msgtype": "markdown", "markdown": map[string]any{"content": text}}
}

func newTaskID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "droi-" + hex.EncodeToString(b)
}

func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// toolNames are the tools a permission request asks to run.
func toolNames(perm *controller.Permission) string {
	names := make([]string, len(perm.ToolUses))
	for i, u := range perm.ToolUses {
		names[i] = u.ToolUse.Name
	}
	return strings.Join(names, ", ")
}

// toolDetails are the tool calls a permission request asks to run, as the
// window shows them.
func toolDetails(perm *controller.Permission) string {
	var lines []string
	for _, u := range perm.ToolUses {
		if sp := transcript.ParseScriptPermission(u.Details.Raw); sp != nil {
			for _, c := range sp.Calls {
				lines = append(lines, c.Tool+": "+c.Detail)
			}
			continue
		}
		input, _ := json.Marshal(u.ToolUse.Input)
		lines = append(lines, transcript.PermissionDetail(u.Details.Raw, input))
	}
	return strings.Join(lines, "\n")
}

// permissionCard is a permission request as a button_interaction card: a
// button per option (WeCom shows six at most), each with the option's label,
// or its number when a label is too long for a button and the labels are
// listed above.
func permissionCard(p *prompt) map[string]any {
	options := p.perm.Options[:min(len(p.perm.Options), maxCardButtons)]
	numbered := slices.ContainsFunc(options, func(o protocol.ToolConfirmationListItem) bool {
		return len([]rune(o.Label)) > maxButtonRunes
	})
	var rows, buttons []map[string]any
	for i, o := range options {
		key, text := strconv.Itoa(i+1), o.Label
		if numbered {
			rows = append(rows, map[string]any{"keyname": key, "value": o.Label})
			text = key
		}
		style := 1
		if o.Value == protocol.ToolConfirmationOutcomeCancel {
			style = 2
		}
		buttons = append(buttons, map[string]any{"text": text, "style": style, "key": key})
	}
	card := cardFrame(p)
	card["button_list"] = buttons
	if rows != nil {
		card["horizontal_content_list"] = rows
	}
	return card
}

// settledCard is a permission request's card once it is answered: the
// choice and who made it, and no options.
func settledCard(p *prompt) map[string]any {
	card := cardFrame(p)
	card["sub_title_text"] = cutRunes(outcome(p), 112)
	card["button_list"] = []map[string]any{{"text": "Answered", "style": 2, "key": "answered"}}
	return card
}

// goneCard is a card the Chat Bridge no longer knows, such as one from
// before the Host restarted.
func goneCard(taskID string) map[string]any {
	return map[string]any{
		"card_type":      "button_interaction",
		"main_title":     map[string]any{"title": "Permission request"},
		"sub_title_text": "Droid no longer waits for this answer.",
		"button_list":    []map[string]any{{"text": "Closed", "style": 2, "key": "closed"}},
		"task_id":        taskID,
	}
}

func cardFrame(p *prompt) map[string]any {
	return map[string]any{
		"card_type":  "button_interaction",
		"main_title": map[string]any{"title": cutRunes("Droid asks to run "+toolNames(p.perm), maxCardTitleRunes)},
		"quote_area": map[string]any{"quote_text": cutRunes(toolDetails(p.perm), maxQuoteRunes)},
		"task_id":    p.taskID,
	}
}

// permissionText is a permission request as a reply under a Channel post,
// which shows no cards.
func permissionText(p *prompt) string {
	var s strings.Builder
	fmt.Fprintf(&s, "Droid asks to run %s:\n%s\n\n", toolNames(p.perm), toolDetails(p.perm))
	for i, o := range p.perm.Options {
		fmt.Fprintf(&s, "%d. %s\n", i+1, o.Label)
	}
	s.WriteString("\nTo answer, @ me with the number alone, such as \"@robot 1\".")
	return s.String()
}

// questionText is the AskUser's ith question as text with numbered options.
func questionText(p *prompt, i int) string {
	q := p.ask.Questions[i]
	var s strings.Builder
	s.WriteString("Droid asks")
	if len(p.ask.Questions) > 1 {
		fmt.Fprintf(&s, " (question %d of %d)", i+1, len(p.ask.Questions))
	}
	if q.Topic != "" {
		s.WriteString(", " + q.Topic)
	}
	s.WriteString(":\n" + q.Question + "\n\n")
	for j, o := range q.Options {
		fmt.Fprintf(&s, "%d. %s\n", j+1, o)
	}
	how := "Answer"
	if p.th.kind == threadKindChannel {
		how = "@ me"
	}
	switch {
	case len(q.Options) == 0:
		fmt.Fprintf(&s, "%s with your answer.", how)
	case q.MultiSelect != nil && *q.MultiSelect:
		fmt.Fprintf(&s, "\n%s with one or more numbers, such as 1,3, or in your own words.", how)
	default:
		fmt.Fprintf(&s, "\n%s with a number, or in your own words.", how)
	}
	return s.String()
}

// askAnswer is the answer text gives a question: the options its numbers
// pick, else the text itself.
func askAnswer(q protocol.AskUserQuestion, text string) string {
	nums := strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '，' || r == '、' || r == ' ' })
	multi := q.MultiSelect != nil && *q.MultiSelect
	if len(nums) == 0 || len(nums) > 1 && !multi {
		return text
	}
	var picked []string
	for _, s := range nums {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > len(q.Options) {
			return text
		}
		if !slices.Contains(picked, q.Options[n-1]) {
			picked = append(picked, q.Options[n-1])
		}
	}
	return strings.Join(picked, ", ")
}

// outcome says on a settled card how it was answered.
func outcome(p *prompt) string {
	switch {
	case p.by != "":
		return fmt.Sprintf("%s answered: %s", p.by, p.choice)
	case p.choice != "":
		return "Answered in Droi: " + p.choice
	}
	return "Droid no longer waits for this answer: it was answered in Droi, or the turn ended."
}

// settledNote is the one line a Chat Thread gets when its Prompt is
// settled where the thread cannot see it.
func settledNote(p *prompt) string {
	what := ""
	if p.perm != nil {
		what = "the permission request to run " + toolNames(p.perm)
	} else {
		what = fmt.Sprintf("the question %q", p.ask.Questions[min(len(p.answers), len(p.ask.Questions)-1)].Question)
	}
	switch {
	case p.by != "":
		return fmt.Sprintf("%s answered %s: %s", p.by, what, p.choice)
	case p.choice != "":
		return fmt.Sprintf("%s was answered in Droi: %s", capitalize(what), p.choice)
	}
	return fmt.Sprintf("Droid no longer waits for an answer to %s: it was answered in Droi, or the turn ended.", what)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
