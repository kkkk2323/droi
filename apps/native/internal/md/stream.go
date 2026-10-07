package md

import "strings"

// Stream holds a Markdown document that grows as it streams in. Blocks
// before the last safe boundary are parsed once and kept; only the tail
// after it is parsed again on every Append, so a long reply costs about the
// same per token as a short one.
type Stream struct {
	src       strings.Builder
	committed []Block
	commitAt  int // bytes of src the committed blocks cover
	tail      []Block
	all       []Block
	done      bool

	// Version changes whenever Blocks does.
	Version int
}

// NewStream starts a stream with text already received.
func NewStream(initial string) *Stream {
	s := &Stream{}
	s.Append(initial)
	return s
}

// Complete parses a finished document.
func Complete(src string) *Stream {
	s := &Stream{}
	s.src.WriteString(src)
	s.Finish()
	return s
}

func (s *Stream) Source() string { return s.src.String() }

func (s *Stream) Done() bool { return s.done }

// Append adds streamed text.
func (s *Stream) Append(delta string) {
	if delta == "" && s.Version > 0 {
		return
	}
	s.src.WriteString(delta)
	src := s.src.String()
	if b := boundary(src, s.commitAt); b > s.commitAt {
		s.committed = append(s.committed, Parse(src[s.commitAt:b])...)
		s.commitAt = b
	}
	s.tail = Parse(Remend(src[s.commitAt:]))
	s.all = append(s.all[:0], s.committed...)
	s.all = append(s.all, s.tail...)
	s.Version++
}

// Finish parses the whole document once more, as a document parsed at once
// would be, for what splitting at boundaries got wrong.
func (s *Stream) Finish() {
	src := s.src.String()
	s.all = Parse(src)
	s.committed, s.tail, s.commitAt = s.all, nil, len(src)
	s.done = true
	s.Version++
}

// Blocks returns the document's blocks. The slice is reused: do not keep
// it across Appends.
func (s *Stream) Blocks() []Block { return s.all }

// boundary returns the start of the last line from which the rest of src
// parses on its own: a line that starts at column 0 after a blank line,
// outside fenced code. It returns from when there is none past it.
func boundary(src string, from int) int {
	best := from
	fence := ""
	prevBlank := false
	for i := from; i < len(src); {
		j := strings.IndexByte(src[i:], '\n')
		if j < 0 {
			// The last line is still coming in.
			break
		}
		line := src[i : i+j]
		blank := strings.TrimSpace(line) == ""
		if fence == "" && prevBlank && !blank && line[0] != ' ' && line[0] != '\t' && !strings.HasPrefix(line, "|") {
			best = i
		}
		if f := fenceOf(line); f != "" {
			if fence == "" {
				fence = f
			} else if strings.HasPrefix(strings.TrimSpace(line), fence) && strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), fence[:1])) == "" {
				fence = ""
			}
		}
		prevBlank = blank && fence == ""
		i += j + 1
	}
	return best
}

// fenceOf returns the fence a line opens or closes, as ``` or ~~~~.
func fenceOf(line string) string {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || len(t) < 3 {
		return ""
	}
	c := t[0]
	if c != '`' && c != '~' {
		return ""
	}
	n := 0
	for n < len(t) && t[n] == c {
		n++
	}
	if n < 3 {
		return ""
	}
	return t[:n]
}

// Remend closes what a partial document leaves open, so that it renders as
// it will once complete: a fenced code block, and bold, strikethrough and
// inline code in its last paragraph.
func Remend(src string) string {
	fence := ""
	lastPara := 0
	for i := 0; i < len(src); {
		j := strings.IndexByte(src[i:], '\n')
		end := i + j
		if j < 0 {
			end = len(src)
		}
		line := src[i:end]
		if f := fenceOf(line); f != "" {
			if fence == "" {
				fence = f
			} else if strings.HasPrefix(strings.TrimSpace(line), fence) {
				fence = ""
				lastPara = end
			}
		} else if fence == "" && strings.TrimSpace(line) == "" {
			lastPara = end
		}
		if j < 0 {
			break
		}
		i = end + 1
	}
	if fence != "" {
		if !strings.HasSuffix(src, "\n") {
			src += "\n"
		}
		return src + fence
	}
	para := src[lastPara:]
	var closers string
	code := strings.Count(para, "`") - 3*strings.Count(para, "```")
	if code%2 == 1 {
		closers += "`"
	} else {
		noCode := stripCodeSpans(para)
		if strings.Count(noCode, "**")%2 == 1 {
			closers += "**"
		}
		if strings.Count(noCode, "~~")%2 == 1 {
			closers += "~~"
		}
	}
	if closers == "" {
		return src
	}
	return strings.TrimRight(src, " ") + closers
}

func stripCodeSpans(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == '`' {
			in = !in
			continue
		}
		if !in {
			b.WriteRune(r)
		}
	}
	return b.String()
}
