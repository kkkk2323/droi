// Command factory-upstream compares the Factory plugin Droi's rewrite layer is ported from
// (pkg/llmproxy/factory/PORTED-FROM.md) with its upstream, and writes what changed in the parts Droi ports as a
// Markdown report. The weekly upstream Automation runs it; it reads the upstream repository only.
//
//	go run ./apps/native/cmd/factory-upstream -out <folder>
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const defaultURL = "https://github.com/magpie-community/plugins"

func main() {
	cache, _ := os.UserCacheDir()
	ported := flag.String("ported", filepath.Join("apps", "native", "pkg", "llmproxy", "factory", "PORTED-FROM.md"), "the PORTED-FROM.md that names the commit and what is ported")
	repo := flag.String("repo", filepath.Join(cache, "droi", "magpie-plugins"), "a clone of the upstream repository; made when missing")
	url := flag.String("url", defaultURL, "the upstream repository")
	to := flag.String("to", "", "the upstream commit to compare with; default: upstream's HEAD")
	fetch := flag.Bool("fetch", true, "fetch upstream first")
	out := flag.String("out", "", "a folder to write the report into, as <date>-<commit>.md; default: standard output")
	flag.Parse()

	if *fetch {
		head, err := fetchHead(*repo, *url)
		if err != nil {
			fail(err)
		}
		if *to == "" {
			*to = head
		}
	}
	if *to == "" {
		fail(fmt.Errorf("-to is needed with -fetch=false"))
	}
	r, err := Compare(*ported, *repo, *to)
	if err != nil {
		fail(err)
	}
	text := r.Markdown(time.Now())
	if *out == "" {
		fmt.Print(text)
		return
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}
	path := filepath.Join(*out, time.Now().Format("2006-01-02")+"-"+short(r.To)+".md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("%s\naction: %s\n", path, r.Action())
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "factory-upstream:", err)
	os.Exit(1)
}
