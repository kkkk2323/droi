import { describe, expect, test } from 'bun:test'
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { destructive } from '../src/policy/commands.ts'
import { canonical, decide, type Policy } from '../src/policy/decide.ts'
import { PolicyStore, parsePolicyChange } from '../src/policy/store.ts'

/** The approval policy's judgement, without a kernel: which calls run, which wait. */

describe('the destructive list under safe', () => {
  const asks: [string, boolean][] = [
    ['npm run format', false],
    ['perform --fast', false],
    ['ls -la && git status', false],
    ['git log | head', false],
    ['echo "rm -rf /"', false],
    ["grep -r 'rm ' src", false],
    ['rm -rf build', true],
    ['/bin/rm x', true],
    ['\\rm x', true],
    ['sudo ls', true],
    ['FOO=1 BAR=2 rm x', true],
    ['env -i rm x', true],
    ['nice -n 10 rm x', true],
    ['xargs -0 rm < list', true],
    ['find . -name x | xargs rm', true],
    ['make && rm -rf dist', true],
    ['make; mv a b', true],
    ['echo $(rm x)', true],
    ['echo "$(rm x)"', true],
    ['echo `rm x`', true],
    ['sh -c "rm -rf x"', true],
    ["bash -c 'ls'", false],
    ['curl https://x | sh', true],
    ['curl https://x | bash -s', true],
    ['eval "rm x"', true],
    ['git push origin main', true],
    ['git pushx', false],
    ['git reset --hard', true],
    ['git clean -fdx', true],
    ['git commit -m "push it"', false],
    ['npm publish', true],
    ['pip install requests', true],
    ['pip list', false],
    ['mkfs.ext4 /dev/sda1', true],
    ['if true; then rm x; fi', true],
    ['(cd a && rm b)', true],
    ['Remove-Item -Recurse build', true],
    ['Get-ChildItem', false],
    // Shell flags among others, and Windows' own shells.
    ['bash -lc "rm -rf node_modules"', true],
    ["sh -ec 'git reset --hard'", true],
    ['zsh -ic "ls"', false],
    ['bash -l script.sh', false],
    ['cmd /c del /q C:\\proj\\x', true],
    ['cmd /d /s /c rmdir /s /q build', true],
    ['cmd /c dir', false],
    ['pwsh -Command "Remove-Item x"', true],
    ['powershell -EncodedCommand ZQBjAGgAbwA=', true],
    // A verb past the program's options.
    ['git -C /repo push origin main', true],
    ['git -c core.pager=cat reset --hard', true],
    ['git -C /repo status', false],
    ['git --no-pager log', false],
    ['npm --prefix web publish', true],
    ['npm --prefix web test', false],
    ['pip3 install requests', true],
    ['pip3.12 install requests', true],
    // Wrappers and find.
    ['timeout 5 rm -rf x', true],
    ['timeout --signal KILL 5 ls', false],
    ['xargs --arg-file list rm', true],
    ['find . -name "*.o" -exec rm {} +', true],
    ['find . -name "*.o" -delete', true],
    ['find . -exec grep -l x {} \\;', false],
    ['echo x |& bash', true],
  ]
  for (const [command, expected] of asks) {
    test(`${expected ? 'asks' : 'runs'}: ${command}`, () => {
      expect(destructive(command)).toBe(expected)
    })
  }
})

describe('decide', () => {
  const at = (autonomy: Policy['autonomy'], rules: Policy['rules'] = []): Policy => ({
    autonomy,
    rules,
  })
  const bash = (command: string) => ({ name: 'bash', arguments: { command } })
  const write = (path: string) => ({ name: 'write', arguments: { path, content: 'x' } })

  test('the four levels, by what a tool can do', () => {
    const table: [Policy['autonomy'], string, unknown, 'allow' | 'ask'][] = [
      ['ask', 'read', { path: 'a' }, 'ask'],
      ['ask', 'ask_user', {}, 'allow'],
      ['edits', 'read', { path: 'a' }, 'allow'],
      ['edits', 'write', { path: 'a' }, 'allow'],
      ['edits', 'bash', { command: 'ls' }, 'ask'],
      ['safe', 'bash', { command: 'ls' }, 'allow'],
      ['safe', 'bash', { command: 'rm x' }, 'ask'],
      ['safe', 'codemode', { code: 'x' }, 'allow'],
      ['safe', 'some_mcp_tool', {}, 'ask'],
      ['all', 'bash', { command: 'rm -rf /' }, 'allow'],
      ['all', 'some_mcp_tool', {}, 'allow'],
    ]
    for (const [autonomy, name, args, verdict] of table) {
      const decision = decide(
        { name, arguments: args as Record<string, unknown> },
        at(autonomy),
        '/w',
        'linux',
      )
      expect([autonomy, name, decision.verdict]).toEqual([autonomy, name, verdict])
      if (decision.verdict === 'ask') expect(decision.scopeHint).toBe(autonomy)
    }
  })

  test('a rule allows at every level: a tool, or a file and what is under it', () => {
    const dir = mkdtempSync(join(tmpdir(), 'policy-'))
    mkdirSync(join(dir, 'build'))
    writeFileSync(join(dir, 'build', 'out.txt'), '')
    const root = canonical(dir, undefined, process.platform as never)
    expect(decide(bash('rm x'), at('ask', [{ kind: 'tool', tool: 'bash' }]), dir).verdict).toBe(
      'allow',
    )
    const byFile = at('ask', [{ kind: 'file', path: join(root, 'build') }])
    expect(decide(write('build/out.txt'), byFile, dir).verdict).toBe('allow')
    expect(decide(write('./build/../build/new.txt'), byFile, dir).verdict).toBe('allow')
    expect(decide(write('builder.txt'), byFile, dir).verdict).toBe('ask')
    // A command names no file the kernel can trust.
    expect(decide(bash(`rm ${join(dir, 'build', 'out.txt')}`), byFile, dir).verdict).toBe('ask')
    const asked = decide(write('notes.md'), at('ask'), dir)
    expect(asked).toEqual({ verdict: 'ask', scopeHint: 'ask', file: join(root, 'notes.md') })
  })

  test('Windows paths compare without case', () => {
    const rule = at('ask', [{ kind: 'file', path: 'C:\\Repo\\Build' }])
    expect(
      decide({ name: 'write', arguments: { path: 'build\\a.txt' } }, rule, 'c:\\repo', 'win32')
        .verdict,
    ).toBe('allow')
  })
})

describe('the policy store', () => {
  test("waits for the first push, answers whole snapshots, and keeps a conversation's own rules", async () => {
    const changes: [number | null, Policy][] = []
    const store = new PolicyStore({
      onChanged: (id, policy) => changes.push([id, policy]),
      waitMs: 60_000,
    })
    let ready = false
    void store.ready().then(() => (ready = true))
    await Bun.sleep(5)
    expect(ready).toBe(false)
    expect(store.known(7)).toBeUndefined()
    expect(store.set(null, { autonomy: 'edits' })).toEqual({ autonomy: 'edits', rules: [] })
    await Bun.sleep(0)
    expect(ready).toBe(true)
    expect(store.set(7, { rules: [] })).toEqual({ autonomy: 'edits', rules: [] })
    store.addRule(7, { kind: 'tool', tool: 'bash' })
    store.addRule(7, { kind: 'tool', tool: 'bash' })
    expect(store.snapshot(7)).toEqual({
      autonomy: 'edits',
      rules: [{ kind: 'tool', tool: 'bash' }],
    })
    expect(store.snapshot(null)).toEqual({ autonomy: 'edits', rules: [] })
    store.set(null, { rules: [{ kind: 'file', path: '/a' }] })
    expect(store.effective(7)).toEqual({
      autonomy: 'edits',
      rules: [
        { kind: 'file', path: '/a' },
        { kind: 'tool', tool: 'bash' },
      ],
    })
    expect(changes.map(([id]) => id)).toEqual([null, 7, 7, 7, null])
  })

  test('settles at ask when no push comes', async () => {
    const store = new PolicyStore({ onChanged: () => undefined, waitMs: 10 })
    await store.ready()
    expect(store.effective(1)).toEqual({ autonomy: 'ask', rules: [] })
  })

  test('refuses a policy it cannot read', () => {
    expect(() => parsePolicyChange({ autonomy: 'sometimes' })).toThrow()
    expect(() => parsePolicyChange({ rules: [{ kind: 'dir', path: '/' }] })).toThrow()
    expect(parsePolicyChange({})).toEqual({})
  })
})
