package jsonrender

import (
	"reflect"
	"testing"
)

const table = `{"root":"t","elements":{"t":{"type":"Table","props":{"columns":[{"header":"A","key":"a"}],"rows":[{"a":"1"}]},"children":[]}}}`

func kinds(segments []Segment) []SegmentKind {
	out := []SegmentKind{}
	for _, s := range segments {
		out = append(out, s.Kind)
	}
	return out
}

func TestSplitReply(t *testing.T) {
	t.Run("a reply without the tag is one Markdown segment", func(t *testing.T) {
		want := []Segment{{Kind: Markdown, Text: "Just **text**"}}
		if got := SplitReply("Just **text**"); !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("the tag on its own line splits the reply around a drawn tree", func(t *testing.T) {
		segments := SplitReply("Before\n\n<json-render>" + table + "</json-render>\n\nAfter")
		if !reflect.DeepEqual(kinds(segments), []SegmentKind{Markdown, Render, Markdown}) {
			t.Fatalf("kinds = %v", kinds(segments))
		}
		if segments[0].Text != "Before\n" || segments[2].Text != "\nAfter" {
			t.Errorf("text = %q, %q", segments[0].Text, segments[2].Text)
		}
		if spec := segments[1].Spec; spec.Root != "t" || spec.Elements["t"].Type != "Table" {
			t.Errorf("spec = %+v", spec)
		}
	})

	t.Run("a tag mid-line and one spanning lines both split", func(t *testing.T) {
		got := kinds(SplitReply("See <json-render>" + table + "</json-render> below"))
		if !reflect.DeepEqual(got, []SegmentKind{Markdown, Render, Markdown}) {
			t.Errorf("mid-line: %v", got)
		}
		got = kinds(SplitReply("<json-render>\n" + table + "\n</json-render>"))
		if !reflect.DeepEqual(got, []SegmentKind{Render}) {
			t.Errorf("spanning: %v", got)
		}
	})

	t.Run("a tag inside a code fence is text about the format", func(t *testing.T) {
		text := "```html\n<json-render>{}</json-render>\n```"
		if got := SplitReply(text); !reflect.DeepEqual(got, []Segment{{Kind: Markdown, Text: text}}) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("a tag inside an inline code span is text about the format", func(t *testing.T) {
		text := "Script 显示和 `<json-render>` 渲染都做完了。\n\nMore"
		if got := SplitReply(text); !reflect.DeepEqual(got, []Segment{{Kind: Markdown, Text: text}}) {
			t.Errorf("got %+v", got)
		}
		got := kinds(SplitReply("``<json-render>`` then <json-render>" + table + "</json-render>"))
		if !reflect.DeepEqual(got, []SegmentKind{Markdown, Render}) {
			t.Errorf("double backticks: %v", got)
		}
	})

	t.Run("a tag still open is pending; JSON that does not parse is shown as code", func(t *testing.T) {
		segments := SplitReply("Here:\n<json-render>{\"root\":")
		if last := segments[len(segments)-1]; last.Kind != Pending || last.Raw != `{"root":` {
			t.Errorf("last = %+v", last)
		}
		want := []Segment{{Kind: Markdown, Text: "```json\n{oops}\n```"}}
		if got := SplitReply("<json-render>{oops}</json-render>"); !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v", got)
		}
	})
}

func TestParseSpec(t *testing.T) {
	t.Run("fills in missing props and children, drops elements without a type", func(t *testing.T) {
		got := ParseSpec(`{"root":"a","elements":{"a":{"type":"Text"},"b":{"props":{}}}}`)
		want := &Spec{Root: "a", Elements: map[string]Element{"a": {Type: "Text", Props: map[string]any{}, Children: []string{}}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("a root that is not among the elements draws nothing", func(t *testing.T) {
		if ParseSpec(`{"root":"x","elements":{}}`) != nil || ParseSpec("[]") != nil {
			t.Error("expected nil")
		}
	})
}

func TestChildrenOf(t *testing.T) {
	spec := ParseSpec(`{"root":"a","elements":{"a":{"type":"Box","children":["b","zz","a"]},"b":{"type":"Text"}}}`)
	if got := ChildrenOf(spec, "a", map[string]bool{"a": true}); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("got %v", got)
	}
}

func TestToneOf(t *testing.T) {
	for in, want := range map[string]Tone{"green": ToneSuccess, "Warning": ToneWarning, "red": ToneError, "magenta": ToneDefault} {
		if got := ToneOf(in); got != want {
			t.Errorf("ToneOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPropReaders(t *testing.T) {
	props := map[string]any{"s": "x", "n": 2.5, "ns": " 4 ", "b": true, "list": []any{"a", 1.0, true}, "obj": map[string]any{"k": 1.0}}
	if Str(props, "n") != "2.5" || Str(props, "b") != "true" || Str(props, "missing") != "" {
		t.Error("Str")
	}
	if n, ok := Num(props, "ns"); !ok || n != 4 {
		t.Error("Num of a numeric string")
	}
	if _, ok := Num(props, "s"); ok {
		t.Error("Num of text")
	}
	if got := Strings(props, "list"); !reflect.DeepEqual(got, []string{"a", "1"}) {
		t.Errorf("Strings = %v", got)
	}
	if got := Cell(props["obj"]); got != `{"k":1}` {
		t.Errorf("Cell = %q", got)
	}
	if Cell(nil) != "" || Cell(3.0) != "3" {
		t.Error("Cell")
	}
	if Fraction(5, 0) != 0 || Fraction(5, 2) != 1 || Fraction(-1, 2) != 0 || Fraction(1, 4) != 0.25 {
		t.Error("Fraction")
	}
}
