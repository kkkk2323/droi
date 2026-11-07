package l10n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// keys are the literal first arguments of every L call in the native app's
// sources (package app's L, and l10n.L or l10n.N elsewhere), by file:line.
func keys(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || d.Name() == "build" || d.Name() == "l10n") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == "L" {
				return false // package app's L, which passes its key on
			}
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				if fn.Name != "L" || f.Name.Name != "app" {
					return true
				}
			case *ast.SelectorExpr:
				if x, ok := fn.X.(*ast.Ident); !ok || x.Name != "l10n" || fn.Sel.Name != "L" && fn.Sel.Name != "N" {
					return true
				}
			default:
				return true
			}
			at := fset.Position(call.Pos()).String()
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s: L takes a string literal, so that the catalog can be checked", at)
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Errorf("%s: %v", at, err)
				return true
			}
			out[s] = at
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var verb = regexp.MustCompile(`%(\[\d+\])?[sdvq]`)

func verbs(s string) []string {
	var out []string
	for _, m := range verb.FindAllString(s, -1) {
		out = append(out, m[len(m)-1:])
	}
	slices.Sort(out)
	return out
}

// Every L key has its translation, with the same values to fill, and the
// table has no key the sources no longer use. DROI_L10N_MISSING=file
// writes the keys without a translation there, one Go-quoted key a line.
func TestCatalog(t *testing.T) {
	used := keys(t)
	var missing []string
	for k, at := range used {
		zhText, ok := zh[k]
		if !ok {
			missing = append(missing, k)
			continue
		}
		if !slices.Equal(verbs(k), verbs(zhText)) {
			t.Errorf("%s: %q fills %v, its translation %q %v", at, k, verbs(k), zhText, verbs(zhText))
		}
	}
	slices.Sort(missing)
	if out := os.Getenv("DROI_L10N_MISSING"); out != "" {
		var b strings.Builder
		for _, k := range missing {
			b.WriteString(strconv.Quote(k) + "\n")
		}
		if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range missing {
		t.Errorf("%s: no translation for %q", used[k], k)
	}
	for k := range zh {
		if _, ok := used[k]; !ok {
			t.Errorf("zh.go: %q is no longer used", k)
		}
	}
}

// setZh gives key a translation for the test, then puts back the table's own;
// deleting it instead would leave the catalog without it for later tests.
func setZh(t *testing.T, key, text string) {
	old, had := zh[key]
	zh[key] = text
	t.Cleanup(func() {
		if had {
			zh[key] = old
		} else {
			delete(zh, key)
		}
	})
}

func TestL(t *testing.T) {
	defer Set("", "en-US")
	setZh(t, "Droi %s is available", "Droi %s 可以更新了")
	if got := L("Droi %s is available", "1.2"); got != "Droi 1.2 is available" {
		t.Errorf("English: %q", got)
	}
	if !Set("", "zh-CN") || Code() != Chinese {
		t.Fatal("a Chinese system did not switch to Chinese")
	}
	if got := L("Droi %s is available", "1.2"); got != "Droi 1.2 可以更新了" {
		t.Errorf("Chinese: %q", got)
	}
	if got := L("No translation"); got != "No translation" {
		t.Errorf("a key without a translation reads %q", got)
	}
	if !Set("en", "") || Code() != English {
		t.Fatal("picking English on a Chinese system did not switch")
	}
	if Set("en", "zh-TW") {
		t.Fatal("the system's locale overrode the pick")
	}
	if got := L("100%"); got != "100%" {
		t.Errorf("a key without values was formatted: %q", got)
	}
}

func TestAdd(t *testing.T) {
	defer Set("", "en-US")
	defer added.Store(added.Load())
	setZh(t, "Settings", "设置")
	Add(map[string]string{"Bot %s": "机器人 %s", "Settings": "另一个"})
	Add(map[string]string{"Posts": "帖子"})
	if got := L("Bot %s", "A"); got != "Bot A" {
		t.Errorf("English: %q", got)
	}
	Set("zh-Hans", "")
	for key, want := range map[string]string{"Bot %s": "机器人 %s", "Posts": "帖子", "Settings": "设置"} {
		if got := T(key); got != want {
			t.Errorf("T(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestSetAppName(t *testing.T) {
	defer SetAppName("")
	defer Set("", "en-US")
	setZh(t, "Quit Droi", "退出 Droi")
	SetAppName("Acme Agent")
	for key, want := range map[string]string{
		"Quit Droi":                   "Quit Acme Agent",
		"Droi’s Memory, Droi's droid": "Acme Agent’s Memory, Acme Agent's droid",
		"Droid runs droi-memory":      "Droid runs droi-memory",
		"DroiX and XDroi":             "DroiX and XDroi",
	} {
		if got := T(key); got != want {
			t.Errorf("T(%q) = %q, want %q", key, got, want)
		}
	}
	if got := L("Droi %s", "Droi"); got != "Acme Agent Droi" {
		t.Errorf("a value was renamed: %q", got)
	}
	Set("zh-Hans", "")
	if got := T("Quit Droi"); got != "退出 Acme Agent" {
		t.Errorf("Chinese: %q", got)
	}
	SetAppName("")
	if got := T("Quit Droi"); got != "退出 Droi" || AppName() != "Droi" {
		t.Errorf("back to Droi: %q, %q", got, AppName())
	}
}

// The window's copy that named Droid names the app Droi instead, so another
// app built on these packages shows its own name there: the words Q14 moved
// off Droid, in both languages.
func TestTheWindowsCopyNamesAnotherAppsName(t *testing.T) {
	defer SetAppName("")
	defer Set("", "en-US")
	keys := []string{
		"Appended to Droi's system prompt in every new Session from this computer or a paired phone. Existing Sessions and subagents keep theirs.",
		"Droi deletes old ephemeral worktrees to stay within this limit. It keeps one that is in use, holds uncommitted changes or unpublished commits, or has an open pull request, and never deletes the local branch.",
		"Droi finished",
		"Droi has a question",
		"Droi has a queued question",
		"Droi keeps a worktree with uncommitted changes; delete it in Settings → Worktrees.",
		"Droi keeps working and reads your answer later",
		"Droi needs input",
		"Droi wants to run %s",
		"How much Droi may do without asking for approval.",
		"Image from Droi",
		"Not connected to Droi: %s",
		"Notify when Droi finishes",
		"Notify when Droi needs input",
		"Off: Droi starts every Session with no recollection of earlier ones. Turn it on to let it remember preferences, conventions and past work on this computer.",
		"Plays when Droi finishes and the Session stops.",
		"Plays when Droi stops to ask for permission or an answer.",
		"Prompts Droi runs on a schedule, on this computer, while Droi is open.",
		"Reconnecting to Droi…",
		"Renews caches while Droi runs tools, not while a session is idle.",
		"What should Droi work on?",
		"When Droi finishes",
		"When Droi needs input",
		"Where should Droi work?",
		"Worktrees let a session work on its own checkout and branch, beside your own. Droi makes and cleans them up; these settings are shared with the droid CLI and the Factory App on this computer.",
	}
	const name = "Acme Agent"
	for _, key := range keys {
		if _, ok := zh[key]; !ok {
			t.Errorf("the catalogue has no Chinese for %q", key)
		}
	}
	SetAppName(name)
	for _, key := range keys {
		if got := T(key); strings.Contains(got, "Droi") || !strings.Contains(got, name) {
			t.Errorf("English %q reads %q", key, got)
		}
	}
	Set("zh-Hans", "")
	for _, key := range keys {
		translation, got := zh[key], T(key)
		if strings.Contains(got, "Droi") {
			t.Errorf("Chinese %q reads %q", key, got)
		}
		// Chinese that names the app names the app it runs in, not Droi.
		if strings.Contains(translation, "Droi") != strings.Contains(got, name) {
			t.Errorf("Chinese %q reads %q, from %q", key, got, translation)
		}
	}
}
