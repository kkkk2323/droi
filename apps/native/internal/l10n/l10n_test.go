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

func TestL(t *testing.T) {
	defer Set("", "en-US")
	zh["Droi %s is available"] = "Droi %s 可以更新了"
	defer delete(zh, "Droi %s is available")
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
