// Package droidpatch makes the Patched Droid (ADR 0027): a copy of an
// official droid build whose Factory LLM requests take their Authorization
// from the file DROI_LLM_KEY_FILE names, while every other request keeps the
// signed-in account's credential.
//
// droid is a `bun build --compile` executable. Bun appends a standalone module
// graph to it: a table of modules (`/$bunfs/root/...`) followed by an Offsets
// struct and the trailer "\n---- Bun! ----\n". Each JS module has its source
// and, usually, precompiled JSC bytecode, which Bun runs instead of the source;
// a patch therefore rewrites a module's source and drops its bytecode.
package droidpatch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

var trailer = []byte("\n---- Bun! ----\n")

// offsetsSizes are the sizes of the Offsets struct before the trailer:
// byte_count u64, modules {u32 off, u32 len}, entry_point_id u32, then in
// newer Bun compile_exec_argv {u32, u32} and flags u32.
var offsetsSizes = []int{32, 24}

// recordSizes are the module record sizes Bun has used: N string pointers
// ({u32 offset, u32 length}) and 4 u8 enums. Newer Bun added module_info and
// bytecode_origin_path, growing it from 36 to 44 to 52 bytes.
var recordSizes = []int{52, 44, 36}

var namePrefixes = [][]byte{[]byte("/$bunfs/"), []byte("B:/~BUN/")}

// Pointer indexes inside a module record.
const (
	ptrName = iota
	ptrContents
	ptrSourcemap
	ptrBytecode
)

// ErrNotBun is returned for a file without a Bun module graph.
var ErrNotBun = errors.New("droidpatch: not a bun --compile executable")

// Graph is the module graph of one executable.
type Graph struct {
	// PayloadStart is where the graph starts in the file; every pointer is
	// relative to it.
	PayloadStart int
	ByteCount    int
	ModulesOff   int
	ModulesLen   int
	RecordSize   int
	Modules      []Module
}

// Module is one entry of the graph.
type Module struct {
	Index    int
	Path     string
	Contents []byte
	// Sources are the original file names its sourcemap lists.
	Sources  []string
	Bytecode Span
	Content  Span
	IsJS     bool
}

// Span is a string pointer: Off is relative to the graph's payload.
type Span struct{ Off, Len uint32 }

// Name is the module's file name, chunk-xxxx.js say.
func (m Module) Name() string {
	p := bytes.LastIndexByte([]byte(m.Path), '/')
	return m.Path[p+1:]
}

// HasSource reports whether the module was built from the source file name
// (src/llm-proxy/utils.ts say); sourcemap names may carry ../ prefixes.
func (m Module) HasSource(name string) bool {
	for _, s := range m.Sources {
		if s == name || len(s) > len(name) && s[len(s)-len(name)-1] == '/' && s[len(s)-len(name):] == name {
			return true
		}
	}
	return false
}

// Parse reads the module graph of blob, the bytes of an executable.
func Parse(blob []byte) (*Graph, error) {
	pos := bytes.LastIndex(blob, trailer)
	if pos < 0 {
		return nil, ErrNotBun
	}
	for _, size := range offsetsSizes {
		start := pos - size
		if start < 0 {
			continue
		}
		byteCount := binary.LittleEndian.Uint64(blob[start:])
		modOff := int(binary.LittleEndian.Uint32(blob[start+8:]))
		modLen := int(binary.LittleEndian.Uint32(blob[start+12:]))
		if byteCount > uint64(start) || modLen == 0 || uint64(modOff+modLen) > byteCount {
			continue
		}
		payloadStart := start - int(byteCount)
		payload := blob[payloadStart:start]
		rs := recordSize(payload, modOff, modLen)
		if rs == 0 {
			continue
		}
		g := &Graph{PayloadStart: payloadStart, ByteCount: int(byteCount), ModulesOff: modOff, ModulesLen: modLen, RecordSize: rs}
		g.Modules = parseModules(payload, g)
		return g, nil
	}
	return nil, fmt.Errorf("droidpatch: found the Bun trailer but not a known module table layout")
}

func span(buf []byte, off int) Span {
	return Span{binary.LittleEndian.Uint32(buf[off:]), binary.LittleEndian.Uint32(buf[off+4:])}
}

func slice(buf []byte, s Span) []byte {
	end := uint64(s.Off) + uint64(s.Len)
	if end > uint64(len(buf)) {
		return nil
	}
	return buf[s.Off:end]
}

func recordSize(payload []byte, off, length int) int {
	for _, size := range recordSizes {
		if length%size != 0 {
			continue
		}
		ok := true
		for i := 0; i < length/size && ok; i++ {
			name := slice(payload, span(payload, off+i*size))
			ok = false
			for _, p := range namePrefixes {
				if bytes.HasPrefix(name, p) {
					ok = true
				}
			}
		}
		if ok {
			return size
		}
	}
	return 0
}

func parseModules(payload []byte, g *Graph) []Module {
	ptrs := (g.RecordSize - 4) / 8
	n := g.ModulesLen / g.RecordSize
	out := make([]Module, 0, n)
	for i := range n {
		base := g.ModulesOff + i*g.RecordSize
		side := payload[base+ptrs*8+3]
		m := Module{
			Index:    i,
			Path:     string(slice(payload, span(payload, base+ptrName*8))),
			Content:  span(payload, base+ptrContents*8),
			Bytecode: span(payload, base+ptrBytecode*8),
			IsJS:     side == 0,
		}
		m.Contents = slice(payload, m.Content)
		m.Sources = sourcemapNames(slice(payload, span(payload, base+ptrSourcemap*8)))
		out = append(out, m)
	}
	return out
}

// sourcemapNames reads the file names of Bun's serialized sourcemap:
// u32 count, u32 map length, count name pointers, count content pointers,
// every pointer relative to the sourcemap itself.
func sourcemapNames(sm []byte) []string {
	if len(sm) < 8 {
		return nil
	}
	count := int(binary.LittleEndian.Uint32(sm))
	if count < 0 || 8+count*16 > len(sm) {
		return nil
	}
	names := make([]string, 0, count)
	for j := range count {
		names = append(names, string(slice(sm, span(sm, 8+j*8))))
	}
	return names
}

// recordOffset is where module i's record starts in the file.
func (g *Graph) recordOffset(i int) int { return g.PayloadStart + g.ModulesOff + i*g.RecordSize }
