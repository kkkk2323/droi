---
name: Factory plugin upstream
schedule: 0 9 * * 1
workingDirectory: /path/to/droi
---

Check whether the Factory plugin Droi's rewrite layer is ported from moved upstream.

1. Run `go run ./apps/native/cmd/factory-upstream -out <folder for reports>` from this checkout. It prints the report's path and `action: none` or `action: review`. It reads github.com/magpie-community/plugins only; never send a request to Factory.
2. Read the report.
   - `Action: none` because Droi is ported from upstream's latest commit: stop.
   - `Action: none` with a commit to move to: change the `commit:` line of `apps/native/pkg/llmproxy/factory/PORTED-FROM.md` to it and open a pull request that says nothing Droi ports changed.
   - `Action: review`: for each ported name that changed or is gone, and each file Droi follows whole, port the change into `apps/native/pkg/llmproxy/factory` as `PORTED-FROM.md` maps it (a refused phrase in `client-phrases.json` goes into `phrases.go`), update `PORTED-FROM.md` (commit, line numbers, deliberate differences), run `go test ./apps/native/pkg/llmproxy/...`, and open a pull request with the report in its description.
3. End with the report's path and what you did.
