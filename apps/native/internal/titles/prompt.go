package titles

import (
	"strings"
	"unicode/utf8"
)

// What a title call sends. The rules live in the system message because that is where a model is told what
// kind of answer is wanted; the language rule is a rule and not a detection: a title in the same words as
// the question never needs a language of its own, and the fallback (the user's own words) is right by
// construction.
const (
	// System is the instruction every title call carries.
	System = `You name a chat. Answer with JSON: {"title": "..."}.
Rules: 3 to 6 words, at most 40 characters; the same language as the user's message; name what the user asked for; no quotes, no trailing punctuation, and no word like title, chat or session.`

	// Schema is the JSON Schema the answer must satisfy: one string, nothing else.
	Schema = `{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`

	// MaxTitleRunes bounds a title, in characters rather than bytes: a Chinese title of forty characters is
	// a long one, and a byte bound would cut it in half.
	MaxTitleRunes = 40
	// MaxQuestionRunes bounds what the title call reads of the user's first message.
	MaxQuestionRunes = 2000
	// MaxAnswerRunes bounds what it reads of the first answer.
	MaxAnswerRunes = 400
	// MaxTokens is the whole budget of the call: a title is a few tokens and a runaway answer is not one.
	MaxTokens = 96

	// KernelTitleLimit is how much of the first user message the Kernel keeps as a conversation's derived
	// title, in UTF-16 units (its own TITLE_LIMIT). Naming compares the stored title with that derived one to
	// tell "nobody named this yet" from "somebody did".
	KernelTitleLimit = 120
)

// Input is what a title call reads: the first thing the user asked, and the first answer, so a name can say
// both what was wanted and what came of it. An empty answer leaves the question alone.
func Input(question, answer string) string {
	var b strings.Builder
	b.WriteString("User: ")
	b.WriteString(oneLine(question, MaxQuestionRunes))
	if answer = oneLine(answer, MaxAnswerRunes); answer != "" {
		b.WriteString("\nAssistant: ")
		b.WriteString(answer)
	}
	return b.String()
}

// FirstMessage is the title a conversation gets when no model can name it: the user's own first words, on
// one line, cut to a title's length. It is also the language guarantee — the words are the user's.
func FirstMessage(text string) string {
	return oneLine(text, MaxTitleRunes)
}

// Sanitize turns what a model answered into a title, and says whether it is one. A model wraps an answer in
// quotes, prefixes it with a label or ends it with a full stop; those are not part of a name. An answer that
// is empty, several lines, a placeholder, or a copy of the question is not a title and is refused.
func Sanitize(raw, question string) (string, bool) {
	title := strings.TrimSpace(raw)
	// A label the model added, in either language the instructions use.
	for _, prefix := range []string{"Title:", "title:", "标题：", "标题:"} {
		title = strings.TrimSpace(strings.TrimPrefix(title, prefix))
	}
	title = strings.Trim(title, "\"'\u201c\u201d\u2018\u2019\u300c\u300d\u300e\u300f")
	// Several lines are not a name. Asked before the whitespace is collapsed, which would hide them.
	if strings.ContainsAny(title, "\r\n") {
		return "", false
	}
	title = strings.Join(strings.Fields(title), " ")
	if cut := []rune(title); len(cut) > MaxTitleRunes {
		title = string(cut[:MaxTitleRunes])
	}
	title = strings.TrimRight(title, ".。！!？?，,；;：:、")
	title = strings.TrimSpace(title)
	if title == "" {
		return "", false
	}
	if !utf8.ValidString(title) {
		return "", false
	}
	for _, placeholder := range []string{"Title", "Untitled", "New chat", "新会话", "无标题"} {
		if strings.EqualFold(title, placeholder) {
			return "", false
		}
	}
	// An answer that only repeats the question names nothing: the fallback would then be the same words,
	// without spending a model call to learn it.
	if echo(title, question) {
		return "", false
	}
	return title, true
}

// echo says a title is the question itself, as far as punctuation and length go.
func echo(title, question string) bool {
	asked := plain(oneLine(question, MaxTitleRunes))
	return asked != "" && strings.EqualFold(plain(title), asked)
}

// plain drops everything that is punctuation or space, for comparing a title with a question.
func plain(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\u3000' || isPunctuation(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isPunctuation(r rune) bool {
	switch r {
	case '.', ',', '!', '?', ';', ':', '、', '。', '，', '！', '？', '；', '：', '\u2018', '\u2019', '\u201c', '\u201d', '"', '\'', '(', ')', '（', '）':
		return true
	}
	return false
}

// oneLine is text on a single line, whitespace collapsed, cut to limit characters with an ellipsis.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}
