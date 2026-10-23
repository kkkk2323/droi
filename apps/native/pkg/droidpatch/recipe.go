package droidpatch

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// KeyFileEnv names the file a Patched Droid reads its LLM key from. The
// variable holds a path, never the key, so the key can change while droid
// runs.
const KeyFileEnv = "DROI_LLM_KEY_FILE"

// TargetSource is the original file whose module the patch changes: it
// builds the headers of every Factory LLM request (createProxyHeaders).
const TargetSource = "src/llm-proxy/utils.ts"

// Recipe is how to patch one droid version.
type Recipe struct {
	Version string `json:"version"`
	// ModuleSHA256 is the hash of the target module's source as Factory
	// built it. Inject names minified identifiers of that very source, so a
	// module with another hash is never patched.
	ModuleSHA256 string `json:"moduleSha256"`
	// Anchor occurs once in the module; Inject goes right before it.
	Anchor string `json:"anchor"`
	Inject string `json:"inject"`
	// Platforms are the GOOS/GOARCH pairs the recipe was verified on.
	Platforms []string `json:"platforms"`
}

// Supports reports whether the recipe was verified on platform (GOOS/GOARCH).
func (r Recipe) Supports(platform string) bool {
	for _, p := range r.Platforms {
		if p == platform {
			return true
		}
	}
	return false
}

// Report says what Apply changed.
type Report struct {
	Version        string `json:"version"`
	Module         string `json:"module"`
	OriginalSHA256 string `json:"originalSha256"`
	PatchedSHA256  string `json:"patchedSha256"`
	SourceBytes    int    `json:"sourceBytes"`
	ReusedBytes    int    `json:"reusedBytes"`
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// Target finds the module built from TargetSource.
func (g *Graph) Target() (*Module, error) {
	var found *Module
	for i := range g.Modules {
		m := &g.Modules[i]
		if !m.IsJS || !m.HasSource(TargetSource) {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("droidpatch: %s is in more than one module", TargetSource)
		}
		found = m
	}
	if found == nil {
		return nil, fmt.Errorf("droidpatch: no module is built from %s", TargetSource)
	}
	return found, nil
}

// Apply returns a patched copy of blob. It refuses unless the target module
// is exactly the one the recipe was made for, and checks afterwards that no
// other module changed.
func Apply(blob []byte, r Recipe) ([]byte, Report, error) {
	g, err := Parse(blob)
	if err != nil {
		return nil, Report{}, err
	}
	m, err := g.Target()
	if err != nil {
		return nil, Report{}, err
	}
	if got := sha(m.Contents); got != r.ModuleSHA256 {
		return nil, Report{}, fmt.Errorf("droidpatch: %s is not the module the %s recipe was made for", m.Name(), r.Version)
	}
	if bytes.Count(m.Contents, []byte(r.Anchor)) != 1 || r.Inject == "" {
		return nil, Report{}, errors.New("droidpatch: the recipe's anchor is not in the module exactly once")
	}
	source := bytes.Replace(m.Contents, []byte(r.Anchor), []byte(r.Inject+r.Anchor), 1)
	// The new source goes where the module's bytecode was. Bun runs a
	// module's bytecode when it has some, so the bytecode pointer is
	// cleared anyway, and its room is free; other modules keep their offsets.
	if m.Bytecode.Len == 0 || uint32(len(source))+1 > m.Bytecode.Len {
		return nil, Report{}, fmt.Errorf("droidpatch: %s has no room for its patched source", m.Name())
	}
	out := bytes.Clone(blob)
	at := g.PayloadStart + int(m.Bytecode.Off)
	copy(out[at:], source)
	clear(out[at+len(source) : at+int(m.Bytecode.Len)])
	rec := g.recordOffset(m.Index)
	binary.LittleEndian.PutUint32(out[rec+ptrContents*8:], m.Bytecode.Off)
	binary.LittleEndian.PutUint32(out[rec+ptrContents*8+4:], uint32(len(source)))
	binary.LittleEndian.PutUint64(out[rec+ptrBytecode*8:], 0)

	after, err := Parse(out)
	if err != nil || len(after.Modules) != len(g.Modules) {
		return nil, Report{}, errors.New("droidpatch: the patched file does not parse")
	}
	for i, a := range after.Modules {
		b := g.Modules[i]
		if i == m.Index {
			if !bytes.Equal(a.Contents, source) || a.Bytecode.Len != 0 {
				return nil, Report{}, errors.New("droidpatch: the patched module does not read back")
			}
			continue
		}
		if a.Path != b.Path || a.Content != b.Content || a.Bytecode != b.Bytecode {
			return nil, Report{}, fmt.Errorf("droidpatch: patching moved %s", b.Name())
		}
	}
	return out, Report{
		Version:        r.Version,
		Module:         m.Name(),
		OriginalSHA256: sha(blob),
		PatchedSHA256:  sha(out),
		SourceBytes:    len(source),
		ReusedBytes:    int(m.Bytecode.Len),
	}, nil
}

const ident = `[A-Za-z_$][\w$]*`

// The shapes Generate looks for in the minified createProxyHeaders and the
// customer-deployment check beside it (droid 0.247.0).
var (
	headersReturn = regexp.MustCompile(`return\{\.\.\.(` + ident + `),\.\.\.` + ident + `,"User-Agent":`)
	authAssign    = `[,{;]\s*(?:let\s+)?%s=await ` + ident + `\(\)`
	premCheck     = regexp.MustCompile(`resolvePremBaseHost\(\),!(` + ident + `)\((` + ident + `)\(\)\)`)
)

// injectTemplate replaces the LLM request's Authorization with the key file's
// key. It keeps the login headers otherwise (the organization id included),
// refuses customer deployments, whose authentication differs, and fails the
// request without a retry (status 400) when the key cannot be read, so it
// never falls back to billing the signed-in account.
const injectTemplate = `if(Bun.env.DROI_LLM_KEY_FILE){if(PREM(ENV()))throw Object.assign(new Error("Droi LLM key: customer deployments are not supported"),{status:400});let droiKey;try{droiKey=(await Bun.file(Bun.env.DROI_LLM_KEY_FILE).text()).trim()}catch{}if(!droiKey||!/^fk-\S+$/.test(droiKey))throw Object.assign(new Error("Droi LLM key file is missing or does not hold a Factory key"),{status:400});AUTH={...AUTH,Authorization:` + "`Bearer ${droiKey}`" + `}}`

// Generate makes a recipe for the droid in blob by finding the headers
// function's shapes. It is for new droid versions; a recipe it makes is only
// trusted after the recipe was verified against a running droid.
func Generate(blob []byte, version string) (Recipe, error) {
	g, err := Parse(blob)
	if err != nil {
		return Recipe{}, err
	}
	m, err := g.Target()
	if err != nil {
		return Recipe{}, err
	}
	src := string(m.Contents)
	ret := headersReturn.FindAllStringSubmatchIndex(src, -1)
	if len(ret) != 1 {
		return Recipe{}, fmt.Errorf("droidpatch: expected one LLM headers return, found %d", len(ret))
	}
	anchor := src[ret[0][0]:ret[0][1]]
	auth := src[ret[0][2]:ret[0][3]]
	fn := src[:ret[0][0]]
	if i := strings.LastIndex(fn, "async function "); i >= 0 {
		fn = fn[i:]
	} else {
		return Recipe{}, errors.New("droidpatch: the LLM headers return is not in an async function")
	}
	if !strings.Contains(fn, `"x-assistant-message-id":`) {
		return Recipe{}, errors.New("droidpatch: the LLM headers function lacks x-assistant-message-id")
	}
	if !regexp.MustCompile(fmt.Sprintf(authAssign, regexp.QuoteMeta(auth))).MatchString(fn) {
		return Recipe{}, errors.New("droidpatch: the auth headers are not a variable of the headers function")
	}
	prem := premCheck.FindAllStringSubmatch(src, -1)
	if len(prem) == 0 {
		return Recipe{}, errors.New("droidpatch: no customer deployment check in the module")
	}
	for _, p := range prem[1:] {
		if p[1] != prem[0][1] || p[2] != prem[0][2] {
			return Recipe{}, errors.New("droidpatch: ambiguous customer deployment check")
		}
	}
	inject := strings.NewReplacer("PREM", prem[0][1], "ENV", prem[0][2], "AUTH", auth).Replace(injectTemplate)
	return Recipe{Version: version, ModuleSHA256: sha(m.Contents), Anchor: anchor, Inject: inject}, nil
}
