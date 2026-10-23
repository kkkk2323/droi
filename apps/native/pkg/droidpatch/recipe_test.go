package droidpatch

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeModule struct {
	path, contents string
	sources        []string
	bytecode       int
}

// buildFake lays out a Bun module graph the way droid 0.247.0 does: a
// 52-byte record per module and a 32-byte Offsets struct before the trailer.
func buildFake(prefix []byte, mods []fakeModule) []byte {
	var payload []byte
	put := func(b []byte) Span {
		s := Span{uint32(len(payload)), uint32(len(b))}
		payload = append(payload, b...)
		payload = append(payload, 0)
		return s
	}
	type rec struct{ name, contents, sm, bc Span }
	var recs []rec
	for _, m := range mods {
		var sm []byte
		sm = binary.LittleEndian.AppendUint32(sm, uint32(len(m.sources)))
		sm = binary.LittleEndian.AppendUint32(sm, 0)
		head := 8 + len(m.sources)*16
		var names []byte
		var ptrs []byte
		for _, s := range m.sources {
			ptrs = binary.LittleEndian.AppendUint32(ptrs, uint32(head+len(names)))
			ptrs = binary.LittleEndian.AppendUint32(ptrs, uint32(len(s)))
			names = append(names, s...)
		}
		for range m.sources {
			ptrs = binary.LittleEndian.AppendUint64(ptrs, 0)
		}
		sm = append(append(sm, ptrs...), names...)
		r := rec{name: put([]byte(m.path)), contents: put([]byte(m.contents)), sm: put(sm)}
		if m.bytecode > 0 {
			r.bc = put(bytes.Repeat([]byte{0xbc}, m.bytecode))
		}
		recs = append(recs, r)
	}
	modOff := len(payload)
	for _, r := range recs {
		for _, s := range []Span{r.name, r.contents, r.sm, r.bc, {}, {}} {
			payload = binary.LittleEndian.AppendUint32(payload, s.Off)
			payload = binary.LittleEndian.AppendUint32(payload, s.Len)
		}
		payload = append(payload, 0, 1, 1, 0)
	}
	modLen := len(payload) - modOff
	blob := append(bytes.Clone(prefix), payload...)
	var off []byte
	off = binary.LittleEndian.AppendUint64(off, uint64(len(payload)))
	off = binary.LittleEndian.AppendUint32(off, uint32(modOff))
	off = binary.LittleEndian.AppendUint32(off, uint32(modLen))
	off = append(off, make([]byte, 16)...)
	blob = append(blob, off...)
	blob = append(blob, trailer...)
	return append(blob, "tail"...)
}

const fakeTarget = `import{fe,mi}from"/$bunfs/root/chunk-a.js";async function ZL(){if(await P.resolvePremBaseHost(),!fe(D())){}}` +
	`async function Jp({sessionId:e,assistantMessageId:o}){let p=k(),f=await Co().catch((h)=>{throw h});` +
	`let R={[F7]:e,"x-assistant-message-id":o};return{...f,...R,"User-Agent":$b(),[Q1]:hi.version}}`

func fakeDroid() []byte {
	return buildFake([]byte("MZ-or-Mach-O header"), []fakeModule{
		{path: "/$bunfs/root/droid", contents: "// entry", sources: []string{"src/cli.ts"}, bytecode: 64},
		{path: "/$bunfs/root/chunk-1wbt0es3.js", contents: fakeTarget, sources: []string{"../../src/llm-proxy/getModelProviderInfo.ts", "src/llm-proxy/utils.ts"}, bytecode: 2048},
		{path: "/$bunfs/root/chunk-auth.js", contents: "export function getAuthHeaders(){}", sources: []string{"src/api/config.ts"}, bytecode: 128},
	})
}

func TestGenerateFindsTheHeadersFunction(t *testing.T) {
	r, err := Generate(fakeDroid(), "0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Anchor != `return{...f,...R,"User-Agent":` {
		t.Fatalf("anchor = %q", r.Anchor)
	}
	for _, want := range []string{"if(fe(D()))", "f={...f,Authorization:`Bearer ${droiKey}`}", "Bun.env.DROI_LLM_KEY_FILE", "{status:400}"} {
		if !strings.Contains(r.Inject, want) {
			t.Fatalf("inject lacks %q: %s", want, r.Inject)
		}
	}
}

func TestApplyChangesOnlyTheTargetModule(t *testing.T) {
	blob := fakeDroid()
	r, err := Generate(blob, "0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	out, rep, err := Apply(blob, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(blob) || rep.Module != "chunk-1wbt0es3.js" {
		t.Fatalf("len %d→%d, module %q", len(blob), len(out), rep.Module)
	}
	g, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	m := g.Modules[1]
	if m.Bytecode.Len != 0 || !strings.Contains(string(m.Contents), r.Inject+r.Anchor) {
		t.Fatalf("patched module = %q, bytecode %d", m.Contents, m.Bytecode.Len)
	}
	before, _ := Parse(blob)
	for _, i := range []int{0, 2} {
		if !bytes.Equal(g.Modules[i].Contents, before.Modules[i].Contents) || g.Modules[i].Bytecode != before.Modules[i].Bytecode {
			t.Fatalf("module %d changed", i)
		}
	}
	if !bytes.Equal(blob, fakeDroid()) {
		t.Fatal("Apply changed its input")
	}
}

func TestApplyRefusesAnotherBuild(t *testing.T) {
	blob := fakeDroid()
	r, _ := Generate(blob, "0.0.1")
	r.ModuleSHA256 = strings.Repeat("0", 64)
	if _, _, err := Apply(blob, r); err == nil {
		t.Fatal("patched a module of another build")
	}
	small := buildFake(nil, []fakeModule{{path: "/$bunfs/root/chunk-x.js", contents: fakeTarget, sources: []string{"src/llm-proxy/utils.ts"}, bytecode: 16}})
	r2, err := Generate(small, "0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Apply(small, r2); err == nil || !strings.Contains(err.Error(), "no room") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRejectsOtherFiles(t *testing.T) {
	if _, err := Parse([]byte("#!/bin/sh\necho hi\n")); err != ErrNotBun {
		t.Fatalf("err = %v", err)
	}
}

// TestRealDroid patches the droid DROI_PATCH_DROID names, for a new version.
func TestRealDroid(t *testing.T) {
	path := os.Getenv("DROI_PATCH_DROID")
	if path == "" {
		t.Skip("DROI_PATCH_DROID is not set")
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Generate(blob, "real")
	if err != nil {
		t.Fatal(err)
	}
	_, rep, err := Apply(blob, r)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("DROI_PATCH_VERIFY") != "" {
		r.Platforms = []string{"test"}
		dst := filepath.Join(t.TempDir(), "droid")
		if _, err := Build(context.Background(), path, dst, r); err != nil {
			t.Fatal(err)
		}
		if err := Verify(context.Background(), path); err == nil {
			t.Fatal("the official droid passed verification")
		} else {
			t.Logf("official droid: %v", err)
		}
	}
	t.Logf("module %s sha %s, source %d of %d bytes\nanchor %s\ninject %s", rep.Module, r.ModuleSHA256, rep.SourceBytes, rep.ReusedBytes, r.Anchor, r.Inject)
}
