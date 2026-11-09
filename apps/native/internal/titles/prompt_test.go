package titles

import (
	"strings"
	"testing"
)

// A title is one line, without the decorations a model adds: quotes, a label, a full stop. Those are not part
// of a name, and a list that shows them looks like a list of model answers.
func TestSanitizeStripsTheDecorationsOfAnAnswer(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{`"deploy the fix"`, "deploy the fix"},
		{`"deploy the fix."`, "deploy the fix"},
		{"Title: deploy the fix", "deploy the fix"},
		{"标题：修复部署脚本。", "修复部署脚本"},
		{"  deploy   the fix  ", "deploy the fix"},
		{"「修复部署脚本」", "修复部署脚本"},
		{"deploy the fix!", "deploy the fix"},
	}
	for _, c := range cases {
		got, ok := Sanitize(c.raw, "how do I deploy?")
		if !ok || got != c.want {
			t.Errorf("Sanitize(%q) = %q, %v; want %q", c.raw, got, ok, c.want)
		}
	}
}

// What is not a title is refused, so the caller can fall back to the user's own words instead of writing a
// line nobody wants to read.
func TestSanitizeRefusesWhatIsNotATitle(t *testing.T) {
	cases := []struct{ name, raw, question string }{
		{"empty", "", "deploy the fix"},
		{"only decoration", `""`, "deploy the fix"},
		{"several lines", "deploy the fix\nand the tests", "deploy the fix"},
		{"the placeholder", "Untitled", "deploy the fix"},
		{"the other placeholder", "新会话", "修复部署"},
		{"the question itself", "deploy the fix!", "deploy the fix?"},
		{"the question with its punctuation", "修复部署脚本。", "修复部署脚本"},
	}
	for _, c := range cases {
		if got, ok := Sanitize(c.raw, c.question); ok {
			t.Errorf("%s: Sanitize(%q) = %q, and it was accepted", c.name, c.raw, got)
		}
	}
}

// A title longer than the bound is cut, not refused: the model was told the length and missed it, and the
// first forty characters still name the conversation.
func TestSanitizeCutsALongTitle(t *testing.T) {
	got, ok := Sanitize(strings.Repeat("很", 60), "做一件事")
	if !ok {
		t.Fatal("a long title was refused")
	}
	if n := len([]rune(got)); n != MaxTitleRunes {
		t.Fatalf("the title is %d characters, want %d", n, MaxTitleRunes)
	}
}

// The fallback is the user's own first words: one line, cut to a title, never empty when there were words.
func TestFirstMessageIsTheUsersOwnWords(t *testing.T) {
	if got := FirstMessage("fix the deploy\n\nplease"); got != "fix the deploy please" {
		t.Fatalf("FirstMessage = %q", got)
	}
	long := strings.Repeat("字", 60)
	got := FirstMessage(long)
	if n := len([]rune(got)); n != MaxTitleRunes {
		t.Fatalf("the fallback is %d characters, want %d", n, MaxTitleRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("a cut fallback should say so: %q", got)
	}
	if FirstMessage("   ") != "" {
		t.Fatal("spaces made a title")
	}
}

// The call reads the question and the answer, both on one line, and leaves the answer out when there was
// none: a name made of a question alone is still a name.
func TestInputCarriesTheQuestionAndTheAnswer(t *testing.T) {
	got := Input("fix the deploy\nplease", "done, the branch is green")
	if !strings.HasPrefix(got, "User: fix the deploy please") {
		t.Fatalf("the question is not the first line: %q", got)
	}
	if !strings.Contains(got, "Assistant: done, the branch is green") {
		t.Fatalf("the answer is missing: %q", got)
	}
	if only := Input("修复部署脚本", ""); strings.Contains(only, "Assistant:") {
		t.Fatalf("an empty answer was written out: %q", only)
	}
}

// The instruction says the two things that cannot be checked here: the language follows the user's, and the
// answer is JSON with one field. A prompt that lost either would change every title without failing a build.
func TestTheInstructionNamesTheLanguageAndTheSchema(t *testing.T) {
	for _, want := range []string{"same language as the user", "JSON", "40 characters"} {
		if !strings.Contains(System, want) {
			t.Errorf("the instruction does not say %q: %s", want, System)
		}
	}
	if !strings.Contains(Schema, `"required":["title"]`) {
		t.Errorf("the schema does not require a title: %s", Schema)
	}
}
