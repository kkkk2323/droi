package memory

import (
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var (
	project = Slot{Scope: ScopeProject, Workspace: "/Users/dev/app"}
	other   = Slot{Scope: ScopeProject, Workspace: "/Users/dev/other"}
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, dir
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func add(t *testing.T, s *Store, slot Slot, text string, category ...Category) Entry {
	t.Helper()
	c := CategoryInsight
	if len(category) > 0 {
		c = category[0]
	}
	r, err := s.Add(slot, c, text)
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK {
		t.Fatal(r.Reason)
	}
	return r.Entry
}

func texts(entries []Entry) []string {
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Text)
	}
	return out
}

func eq[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}$`)

func TestStoreAddsReadsReplacesAndRemoves(t *testing.T) {
	s, _ := openTestStore(t)
	entry := add(t, s, project, "  pnpm check runs format, lint and typecheck  ", CategoryConvention)
	if !idPattern.MatchString(entry.ID) {
		t.Fatalf("id %q", entry.ID)
	}
	eq(t, entry, Entry{ID: entry.ID, Scope: ScopeProject, Workspace: "/Users/dev/app", Category: CategoryConvention, Day: today(), Text: "pnpm check runs format, lint and typecheck"})
	got, ok := getEntry(t, s, entry.ID)
	if !ok {
		t.Fatal("not found")
	}
	eq(t, got, entry)

	replaced := must[WriteResult](t)(s.Replace(entry.ID, "pnpm check runs format, lint and all typechecks"))
	if !replaced.OK || replaced.Entry.ID != entry.ID || !strings.Contains(replaced.Entry.Text, "all") {
		t.Fatalf("%+v", replaced)
	}

	eq(t, must[bool](t)(s.Remove(entry.ID)), true)
	if _, ok := getEntry(t, s, entry.ID); ok {
		t.Fatal("still there")
	}
	eq(t, must[bool](t)(s.Remove(entry.ID)), false)
	eq(t, must[WriteResult](t)(s.Replace(entry.ID, "x")).OK, false)
}

func getEntry(t *testing.T, s *Store, id string) (Entry, bool) {
	t.Helper()
	e, ok, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return e, ok
}

func TestStoreGivesEveryEntryItsOwnID(t *testing.T) {
	s, _ := openTestStore(t)
	ids := map[string]bool{}
	for i := 0; i < 300; i++ {
		ids[add(t, s, project, "note "+strconv.Itoa(i)).ID] = true
	}
	eq(t, len(ids), 300)
	for id := range ids {
		if !idPattern.MatchString(id) {
			t.Fatalf("id %q", id)
		}
	}
}

func TestStoreKeepsEachWorkspaceAndTheGlobalMemoryApart(t *testing.T) {
	s, _ := openTestStore(t)
	add(t, s, project, "app uses vitest")
	add(t, s, other, "other uses jest")
	add(t, s, GlobalSlot, "answer in Chinese", CategoryPreference)
	eq(t, texts(must[[]Entry](t)(s.List(project, ""))), []string{"app uses vitest"})
	eq(t, texts(must[[]Entry](t)(s.List(other, ""))), []string{"other uses jest"})
	global := must[[]Entry](t)(s.List(GlobalSlot, ""))
	if len(global) != 1 || global[0].Scope != ScopeGlobal || global[0].Workspace != "" || global[0].Category != CategoryPreference {
		t.Fatalf("%+v", global)
	}
}

func TestStoreListsOneCategory(t *testing.T) {
	s, _ := openTestStore(t)
	add(t, s, project, "a failure", CategoryFailure)
	add(t, s, project, "a convention", CategoryConvention)
	eq(t, texts(must[[]Entry](t)(s.List(project, CategoryFailure))), []string{"a failure"})
}

func TestStoreRefusesASecretAndSaysWhy(t *testing.T) {
	s, _ := openTestStore(t)
	r := must[WriteResult](t)(s.Add(project, CategoryToolQuirk, "the proxy key is fk-"+strings.Repeat("a1B2", 7)))
	if r.OK || !strings.Contains(r.Reason, "Factory API key") {
		t.Fatalf("%+v", r)
	}
	eq(t, must[[]Entry](t)(s.List(project, "")), []Entry{})
	entry := add(t, s, project, "the proxy needs a key")
	eq(t, must[WriteResult](t)(s.Replace(entry.ID, "password=hunter2")).OK, false)
	got, _ := getEntry(t, s, entry.ID)
	eq(t, got.Text, "the proxy needs a key")
}

func TestStoreMarksTheSoftLimitAndRefusesPastTheHardOne(t *testing.T) {
	s, _ := openTestStore(t)
	chunk := strings.Repeat("x", 49_000)
	for i := 0; i < 4; i++ {
		r := must[WriteResult](t)(s.Add(project, CategoryInsight, strconv.Itoa(i)+chunk))
		if !r.OK || r.OverSoftLimit {
			t.Fatalf("%d: %+v", i, r.Reason)
		}
	}
	r := must[WriteResult](t)(s.Add(project, CategoryInsight, "4"+chunk))
	if !r.OK || !r.OverSoftLimit {
		t.Fatalf("%+v", r.Reason)
	}
	summary := must[[]SlotSummary](t)(s.Summaries())[0]
	if summary.Workspace != project.Workspace || !summary.OverSoftLimit {
		t.Fatalf("%+v", summary)
	}

	add(t, s, project, strings.Repeat("z", Limits[ScopeProject].Hard-must[int](t)(s.Size(project))))
	refused := must[WriteResult](t)(s.Add(project, CategoryInsight, "one more"))
	if refused.OK || !strings.Contains(refused.Reason, "Settings → Memory") {
		t.Fatalf("%+v", refused)
	}
	// Other Memories are unaffected.
	eq(t, must[WriteResult](t)(s.Add(other, CategoryInsight, "fine")).OK, true)
}

func TestStoreHasSmallerLimitsForGlobalMemory(t *testing.T) {
	s, _ := openTestStore(t)
	add(t, s, GlobalSlot, strings.Repeat("y", Limits[ScopeGlobal].Hard))
	eq(t, must[WriteResult](t)(s.Add(GlobalSlot, CategoryPreference, "more")).OK, false)
}

func search(t *testing.T, s *Store, o SearchOptions) []Entry {
	t.Helper()
	return must[[]Entry](t)(s.Search(o))
}

func TestStoreSearchesByTrigramAnywhereInAWord(t *testing.T) {
	s, _ := openTestStore(t)
	add(t, s, project, "Run the E2E suite with pnpm test:e2e")
	add(t, s, project, "Typecheck with pnpm typecheck")
	add(t, s, other, "pnpm here too")
	eq(t, texts(search(t, s, SearchOptions{Query: "e2e", Slot: project})), []string{"Run the E2E suite with pnpm test:e2e"})
	eq(t, len(search(t, s, SearchOptions{Query: "pnpm", Slot: project})), 2)
	eq(t, len(search(t, s, SearchOptions{Query: "pnpm", Slot: project, Limit: 1})), 1)
}

func TestStoreWantsEveryTermAndSettlesForAnyOnlyWhenNothingHasThemAll(t *testing.T) {
	s, _ := openTestStore(t)
	release := "Release: bump the version, tag, push; the workflow builds the DMG"
	phone := "The phone reads the desktop version at startup"
	add(t, s, project, release)
	add(t, s, project, phone)
	add(t, s, project, "Typecheck with pnpm typecheck")
	find := func(q string) []string { return texts(search(t, s, SearchOptions{Query: q, Slot: project})) }
	eq(t, find("version release"), []string{release})
	eq(t, len(find("version")), 2)
	eq(t, find("typecheck missing"), []string{"Typecheck with pnpm typecheck"})
	both := find("phone release")
	slices.Sort(both)
	eq(t, both, []string{release, phone})
	eq(t, find("nothing here"), []string{})
}

func TestStoreKeepsAQuotedPhraseWhole(t *testing.T) {
	s, _ := openTestStore(t)
	add(t, s, project, "memory search is keyword based")
	add(t, s, project, "search the memory before acting")
	eq(t, texts(search(t, s, SearchOptions{Query: `"memory search"`, Slot: project})), []string{"memory search is keyword based"})
	eq(t, len(search(t, s, SearchOptions{Query: "memory search", Slot: project})), 2)
}

func TestStoreMatchesTwoCharacterWordsAsSubstringsAndDropsOneCharacterOnes(t *testing.T) {
	s, _ := openTestStore(t)
	add(t, s, project, "查日志要用 anlan 命令", CategoryToolQuirk)
	add(t, s, project, "部署前先跑测试", CategoryConvention)
	add(t, s, project, "pnpm 发版要先用 CHANGELOG 记一笔", CategoryConvention)
	find := func(q string) []string { return texts(search(t, s, SearchOptions{Query: q, Slot: project})) }
	eq(t, find("日志"), []string{"查日志要用 anlan 命令"})
	eq(t, search(t, s, SearchOptions{Query: "部署", Slot: project, Category: CategoryFailure}), []Entry{})
	eq(t, len(find("部署前先跑")), 1)
	// Both terms must hold while one entry has them all.
	eq(t, find("pnpm 发版"), []string{"pnpm 发版要先用 CHANGELOG 记一笔"})
	// The short term still counts when no entry has both.
	eq(t, find("pnpm 日志"), []string{"pnpm 发版要先用 CHANGELOG 记一笔", "查日志要用 anlan 命令"})
	// "用" alone would match two of the three entries; beside another term it is ignored.
	eq(t, find("用 anlan"), []string{"查日志要用 anlan 命令"})
	eq(t, len(find("用")), 2)
}

func TestStoreTreatsSearchSyntaxAsPlainText(t *testing.T) {
	s, _ := openTestStore(t)
	add(t, s, project, `quotes "inside" and 100%_done`)
	eq(t, len(search(t, s, SearchOptions{Query: `"inside"`, Slot: project})), 1)
	eq(t, len(search(t, s, SearchOptions{Query: "%_", Slot: project})), 1)
	eq(t, search(t, s, SearchOptions{Query: "OR NOT", Slot: project}), []Entry{})
}

func TestStoreKeepsSearchInStepWithReplaceAndRemove(t *testing.T) {
	s, _ := openTestStore(t)
	entry := add(t, s, project, "uses webpack")
	must[WriteResult](t)(s.Replace(entry.ID, "uses vite"))
	eq(t, search(t, s, SearchOptions{Query: "webpack", Slot: project}), []Entry{})
	eq(t, len(search(t, s, SearchOptions{Query: "vite", Slot: project})), 1)
	must[bool](t)(s.Remove(entry.ID))
	eq(t, search(t, s, SearchOptions{Query: "vite", Slot: project}), []Entry{})
}

func TestStoreGivesTheCorrectionsNewestFirstWithinBothCaps(t *testing.T) {
	s, _ := openTestStore(t)
	for i := 0; i < 25; i++ {
		add(t, s, project, "correction "+strconv.Itoa(i), CategoryCorrection)
	}
	add(t, s, project, "not a correction", CategoryInsight)
	found := must[[]Entry](t)(s.Corrections([]Slot{project}, CorrectionCaps{20, 2_000}))
	eq(t, len(found), 20)
	eq(t, found[0].Text, "correction 24")
	eq(t, texts(must[[]Entry](t)(s.Corrections([]Slot{project}, CorrectionCaps{20, 30}))), []string{"correction 24", "correction 23"})
	add(t, s, GlobalSlot, "global correction", CategoryCorrection)
	add(t, s, other, "elsewhere", CategoryCorrection)
	eq(t, texts(must[[]Entry](t)(s.Corrections([]Slot{project, GlobalSlot}, CorrectionCaps{2, 2_000}))), []string{"global correction", "correction 24"})
}

var isoTime = regexp.MustCompile(`^\d{4}-\d\d-\d\dT`)

func TestStoreSummarisesEveryProjectMemoryAndTheGlobalMemory(t *testing.T) {
	s, _ := openTestStore(t)
	eq(t, must[[]SlotSummary](t)(s.Summaries()), []SlotSummary{{Scope: ScopeGlobal}})
	add(t, s, project, "abc")
	add(t, s, project, "de")
	if err := s.MarkConsolidated(project); err != nil {
		t.Fatal(err)
	}
	got := must[[]SlotSummary](t)(s.Summaries())[0]
	if !isoTime.MatchString(got.LastConsolidated) {
		t.Fatalf("lastConsolidated %q", got.LastConsolidated)
	}
	got.LastConsolidated = ""
	eq(t, got, SlotSummary{Scope: ScopeProject, Workspace: "/Users/dev/app", Entries: 2, Chars: 5})
}

func TestStoreLogsCallsAndSaysHowTheSearchesOfEachMemoryWent(t *testing.T) {
	s, _ := openTestStore(t)
	eq(t, must[string](t)(s.LoggedSince()), "")
	pnpm := add(t, s, project, "uses pnpm", CategoryConvention)
	add(t, s, project, "tests beside code", CategoryConvention)
	add(t, s, project, "never push to main", CategoryCorrection)
	add(t, s, GlobalSlot, "terse answers", CategoryPreference)
	q := "q"
	log := func(c Call) {
		if err := s.LogCall(c); err != nil {
			t.Fatal(err)
		}
	}
	searchCall := func(slot Slot, found []string) {
		log(Call{SessionID: "s1", Tool: "memory_search", Slot: &slot, Query: &q, OK: true, Found: found})
	}
	searchCall(project, []string{pnpm.ID})
	searchCall(project, []string{pnpm.ID})
	searchCall(project, nil)
	log(Call{SessionID: "s1", Tool: "memory_search", Slot: &project, Query: &q})
	log(Call{SessionID: "s1", Tool: "memory_add", Slot: &project, OK: true, Written: pnpm.ID, Similar: []string{pnpm.ID}})
	log(Call{Tool: "memory_list"})

	if !isoTime.MatchString(must[string](t)(s.LoggedSince())) {
		t.Fatal("loggedSince")
	}
	// The correction is left out: the hook puts it in front of every Session.
	eq(t, must[Usage](t)(s.Usage(project)), Usage{Searches: 3, EmptySearches: 1, NeverFound: 1})
	eq(t, must[Usage](t)(s.Usage(GlobalSlot)), Usage{NeverFound: 1})
	eq(t, must[Usage](t)(s.Usage(other)), Usage{})
}

// TestWriterProcess is the second process of the concurrency test; it runs
// only when that test starts it.
func TestWriterProcess(t *testing.T) {
	dir := os.Getenv("DROI_MEMORY_TEST_WRITER_DIR")
	if dir == "" {
		t.Skip("run by TestStoreLetsTwoProcessesWriteAtOnce")
	}
	label := os.Getenv("DROI_MEMORY_TEST_WRITER_LABEL")
	count, _ := strconv.Atoi(os.Getenv("DROI_MEMORY_TEST_WRITER_COUNT"))
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < count; i++ {
		add(t, s, Slot{Scope: ScopeProject, Workspace: "/w"}, label+" note "+strconv.Itoa(i))
	}
}

func TestStoreLetsTwoProcessesWriteAtOnce(t *testing.T) {
	s, dir := openTestStore(t)
	start := func(label string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWriterProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "DROI_MEMORY_TEST_WRITER_DIR="+dir, "DROI_MEMORY_TEST_WRITER_LABEL="+label, "DROI_MEMORY_TEST_WRITER_COUNT=150")
		var out strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if t.Failed() {
				t.Log(out.String())
			}
		})
		return cmd
	}
	a, b := start("a"), start("b")
	if err := a.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := b.Wait(); err != nil {
		t.Fatal(err)
	}
	eq(t, len(must[[]Entry](t)(s.List(Slot{Scope: ScopeProject, Workspace: "/w"}, ""))), 300)
}

func applySlice(t *testing.T, s *Store, sent []Entry, c SliceChanges) string {
	t.Helper()
	return must[string](t)(s.ApplySlice(sent, c))
}

func TestApplySliceRewritesRemovesAndMergesOnlyWhatWasSent(t *testing.T) {
	s, _ := openTestStore(t)
	var sent []Entry
	for _, text := range []string{"a one", "b two", "c three", "d four"} {
		sent = append(sent, add(t, s, project, text, CategoryConvention))
	}
	a, b, c, d := sent[0], sent[1], sent[2], sent[3]
	untouched := add(t, s, project, "not sent", CategoryConvention)
	eq(t, applySlice(t, s, sent, SliceChanges{
		Keep:    []string{a.ID},
		Rewrite: []Rewrite{{ID: b.ID, Text: "b rewritten"}},
		Merge:   []Merge{{IDs: []string{c.ID, d.ID}, Text: "c and d"}},
	}), "")
	got := texts(must[[]Entry](t)(s.List(project, CategoryConvention)))
	slices.Sort(got)
	want := []string{"a one", "b rewritten", "c and d", "not sent"}
	slices.Sort(want)
	eq(t, got, want)
	kept, _ := getEntry(t, s, untouched.ID)
	eq(t, kept.Text, "not sent")
	for _, e := range must[[]Entry](t)(s.List(project, "")) {
		if e.Text == "c and d" && e.Category != CategoryConvention {
			t.Fatalf("%+v", e)
		}
	}
}

func TestApplySliceRejectsAnIDItWasNotSent(t *testing.T) {
	s, _ := openTestStore(t)
	a := add(t, s, project, "a", CategoryConvention)
	outsider := add(t, s, project, "outsider", CategoryConvention)
	reason := applySlice(t, s, []Entry{a}, SliceChanges{Remove: []string{outsider.ID}, Merge: []Merge{{IDs: []string{a.ID}, Text: "x"}}})
	if !strings.Contains(reason, outsider.ID) {
		t.Fatalf("reason %q", reason)
	}
	eq(t, len(must[[]Entry](t)(s.List(project, ""))), 2)
}

func TestApplySliceRejectsAnAnswerThatDropsAnEntry(t *testing.T) {
	s, _ := openTestStore(t)
	a, b := add(t, s, project, "a"), add(t, s, project, "b")
	reason := applySlice(t, s, []Entry{a, b}, SliceChanges{Keep: []string{a.ID}})
	if !strings.Contains(reason, b.ID) {
		t.Fatalf("reason %q", reason)
	}
	eq(t, len(must[[]Entry](t)(s.List(project, ""))), 2)
}

func TestApplySliceRejectsANameGivenTwice(t *testing.T) {
	s, _ := openTestStore(t)
	a := add(t, s, project, "a")
	eq(t, applySlice(t, s, []Entry{a}, SliceChanges{Keep: []string{a.ID}, Remove: []string{a.ID}}), "The answer names an entry more than once.")
}

func TestApplySliceRejectsASliceThatChangedWhileTheModelWorked(t *testing.T) {
	s, _ := openTestStore(t)
	a := add(t, s, project, "a")
	must[WriteResult](t)(s.Replace(a.ID, "a changed meanwhile"))
	if reason := applySlice(t, s, []Entry{a}, SliceChanges{Remove: []string{a.ID}}); reason == "" {
		t.Fatal("applied")
	}
	got, _ := getEntry(t, s, a.ID)
	eq(t, got.Text, "a changed meanwhile")
}

func TestApplySliceRejectsRewrittenTextThatCarriesASecret(t *testing.T) {
	s, _ := openTestStore(t)
	a := add(t, s, project, "a")
	if reason := applySlice(t, s, []Entry{a}, SliceChanges{Rewrite: []Rewrite{{ID: a.ID, Text: "password=x1"}}}); reason == "" {
		t.Fatal("applied")
	}
}

func TestSearchTerms(t *testing.T) {
	eq(t, SearchTerms(`  "memory search"  pnpm a `), []string{"memory search", "pnpm"})
	eq(t, SearchTerms("用"), []string{"用"})
	eq(t, SearchTerms(`"" `), []string{})
}

func TestProjectSlotKeysByTheRealPath(t *testing.T) {
	dir := t.TempDir()
	real := must[string](t)(evalSymlinks(dir))
	eq(t, ProjectSlot(dir), Slot{Scope: ScopeProject, Workspace: real})
	eq(t, ProjectSlot("/nowhere/gone"), Slot{Scope: ScopeProject, Workspace: "/nowhere/gone"})
}
