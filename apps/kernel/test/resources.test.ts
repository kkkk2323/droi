import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { mkdirSync, symlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { loadResources, split } from '../src/resources/index.ts'
import { config, tempData } from './helpers.ts'
import { userDirFromEnv } from '../src/config.ts'

/**
 * The files the kernel reads around a conversation: skills, prompt templates and the `AGENTS.md` chain.
 *
 * The loader is a pure function of two directories, so most of this suite needs no kernel at all — which is
 * the point: what a person puts on disk is what the model is told about.
 */

let data: ReturnType<typeof tempData>

beforeEach(() => {
  data = tempData()
})
afterEach(() => data.cleanup())

/** Write `files` under `root`, creating the directories they need. */
function write(root: string, files: Record<string, string>): string {
  for (const [path, body] of Object.entries(files)) {
    const full = join(root, path)
    mkdirSync(join(full, '..'), { recursive: true })
    writeFileSync(full, body)
  }
  return root
}

function skill(name: string, description: string, extra = ''): string {
  return `---\nname: ${name}\ndescription: ${description}\n---\n\nDo the thing.\n${extra}`
}

describe('skills', () => {
  test('YAML block descriptions, quoted booleans and loose-file names', () => {
    const userDir = write(join(data.dir, 'user'), {
      'skills/loose.md':
        '---\ndescription: >-\n  Review changes\n  with care.\ndisable-model-invocation: "true"\n---\n',
      'skills/literal/SKILL.md': '---\ndescription: |\n  One line.\n  Another line.\n---\n',
    })
    const loaded = loadResources({ userDir, workspace: undefined })
    expect(loaded.skills.map((s) => [s.name, s.description, s.hidden])).toEqual([
      ['literal', 'One line.\nAnother line.', false],
      ['loose', 'Review changes with care.', false],
    ])
  })

  test('follows linked skills once, without walking a symlink cycle', () => {
    const userDir = write(join(data.dir, 'user'), {
      'skills/review/SKILL.md': skill('review', 'Read changes.'),
    })
    symlinkSync(join(userDir, 'skills'), join(userDir, 'skills/loop'), 'dir')
    symlinkSync(join(userDir, 'skills/review'), join(userDir, 'skills/alias'), 'dir')
    expect(loadResources({ userDir, workspace: undefined }).skills).toHaveLength(1)
  })

  test('reports collisions within one scope as well as across scopes', () => {
    const userDir = write(join(data.dir, 'user'), {
      'skills/a/SKILL.md': skill('same', 'First.'),
      'skills/b/SKILL.md': skill('same', 'Second.'),
    })
    const loaded = loadResources({ userDir, workspace: undefined })
    expect(loaded.skills).toHaveLength(1)
    expect(loaded.diagnostics).toHaveLength(1)
  })
  test('a SKILL.md is a skill, named by its frontmatter or by its directory', () => {
    const userDir = write(join(data.dir, 'user'), {
      'skills/code-review/SKILL.md': skill(
        'code-review',
        'Review the changes since a fixed point.',
      ),
      // No name in the frontmatter: the directory names it.
      'skills/ship-it/SKILL.md': '---\ndescription: Cut a release.\n---\n\nShip it.\n',
      // A group directory: still one skill, and the walk finds it.
      'skills/team/triage/SKILL.md': skill('triage', 'Sort the issue pile.'),
      // A loose file directly under the root counts too.
      'skills/hand-write.md': '---\nname: hand-write\ndescription: Written by hand.\n---\n',
    })
    const loaded = loadResources({ userDir, workspace: undefined })
    expect(loaded.skills.map((item) => [item.name, item.scope])).toEqual([
      ['code-review', 'personal'],
      ['hand-write', 'personal'],
      ['ship-it', 'personal'],
      ['triage', 'personal'],
    ])
    expect(loaded.skills[0]?.description).toBe('Review the changes since a fixed point.')
    expect(loaded.skills[0]?.path).toBe(join(userDir, 'skills/code-review/SKILL.md'))
    expect(loaded.diagnostics).toEqual([])
  })

  test('a bad name, a missing description and a hidden skill', () => {
    const userDir = write(join(data.dir, 'user'), {
      'skills/bad/SKILL.md': skill('Not A Name', 'Whatever.'),
      'skills/empty/SKILL.md': '---\nname: empty\n---\n',
      'skills/hidden/SKILL.md': `---\nname: hidden\ndescription: Not for the model.\ndisable-model-invocation: true\n---\n`,
    })
    const loaded = loadResources({ userDir, workspace: undefined })
    expect(loaded.skills.map((item) => item.name)).toEqual(['hidden'])
    expect(loaded.skills[0]?.hidden).toBe(true)
    expect(loaded.diagnostics.length).toBe(2)
    expect(loaded.diagnostics.map((item) => item.message).join(' ')).toContain('not a skill name')
  })

  test('the workspace wins a name collision, and the loser is reported', () => {
    const userDir = write(join(data.dir, 'user'), {
      'skills/review/SKILL.md': skill('review', 'The personal one.'),
    })
    const workspace = write(join(data.dir, 'repo'), {
      '.factory/skills/review/SKILL.md': skill('review', 'The repository one.'),
      '.factory/skills/local/SKILL.md': skill('local', 'Only in this repository.'),
    })
    const loaded = loadResources({ userDir, workspace })
    const review = loaded.skills.find((item) => item.name === 'review')
    expect(review?.description).toBe('The repository one.')
    expect(review?.scope).toBe('workspace')
    expect(loaded.skills.map((item) => item.name)).toEqual(['review', 'local'])
    expect(loaded.diagnostics.length).toBe(1)
    expect(loaded.diagnostics[0]?.message).toContain('.factory/skills/review/SKILL.md')
  })

  test('no directories at all is not an error', () => {
    const loaded = loadResources({
      userDir: join(data.dir, 'nothing'),
      workspace: join(data.dir, 'elsewhere'),
    })
    expect(loaded).toEqual({ skills: [], prompts: [], contextFiles: [], diagnostics: [] })
  })
})

describe('prompt templates', () => {
  test('one file is one command, with its description and its body', () => {
    const userDir = write(join(data.dir, 'user'), {
      'prompts/fix-bug.md':
        '---\ndescription: Fix the bug the user names.\n---\n\nFind and fix $ARGUMENTS.\n',
      'prompts/no-frontmatter.md': 'Just a body with $1 in it.\n',
      'prompts/ignored.txt': 'not a template',
    })
    const workspace = write(join(data.dir, 'repo'), {
      '.factory/prompts/release.md': '---\ndescription: Cut a release.\n---\n',
    })
    const loaded = loadResources({ userDir, workspace })
    expect(loaded.prompts.map((item) => [item.name, item.scope])).toEqual([
      ['fix-bug', 'personal'],
      ['no-frontmatter', 'personal'],
      ['release', 'workspace'],
    ])
    const fix = loaded.prompts.find((item) => item.name === 'fix-bug')
    expect(fix?.description).toBe('Fix the bug the user names.')
    expect(fix?.body.trim()).toBe('Find and fix $ARGUMENTS.')
  })
})

describe('the AGENTS.md chain', () => {
  test('a nested linked worktree shadows the main checkout context of the same name', () => {
    const root = write(join(data.dir, 'repo'), {
      'AGENTS.md': 'main instructions',
      '.git/worktrees/feature/commondir': '../..',
    })
    const workspace = join(root, '.worktrees/feature')
    write(workspace, {
      '.git': `gitdir: ${join(root, '.git/worktrees/feature')}\n`,
      'AGENTS.md': 'worktree instructions',
    })
    const loaded = loadResources({ userDir: join(data.dir, 'user'), workspace })
    expect(loaded.contextFiles.map((f) => f.text)).toEqual(['worktree instructions'])
  })
  test("the user's own first, then from the outermost directory in", () => {
    const userDir = write(join(data.dir, 'user'), { 'AGENTS.md': 'the person\n' })
    const workspace = write(join(data.dir, 'repo', 'packages', 'app'), {
      'AGENTS.md': 'the app\n',
      CLIENT: '',
    })
    write(join(data.dir, 'repo'), { 'CLAUDE.md': 'the repository\n' })
    const loaded = loadResources({ userDir, workspace })
    const texts = loaded.contextFiles.map((file) => file.text.trim())
    expect(texts[0]).toBe('the person')
    // `repo` then `packages/app`; the directories in between have nothing.
    expect(texts.at(-2)).toBe('the repository')
    expect(texts.at(-1)).toBe('the app')
    expect(loaded.contextFiles.every((file) => file.bytes > 0)).toBe(true)
  })

  test('AGENTS.override.md wins over AGENTS.md, and CLAUDE.md is read when nothing else is there', () => {
    const userDir = write(join(data.dir, 'user'), {
      'AGENTS.md': 'the plain one\n',
      'AGENTS.override.md': 'the override\n',
    })
    const workspace = write(join(data.dir, 'repo'), { 'CLAUDE.md': 'the claude one\n' })
    const loaded = loadResources({ userDir, workspace })
    const text = loaded.contextFiles.map((file) => file.text.trim())
    expect(text[0]).toBe('the override')
    expect(text.at(-1)).toBe('the claude one')
  })
})

describe('the frontmatter reader', () => {
  test('flat pairs, quoted values, booleans, and a file without frontmatter', () => {
    expect(split('---\nname: x\ndescription: "a: b"\nhidden: true\n---\nbody')).toEqual({
      frontmatter: { name: 'x', description: 'a: b', hidden: true },
      body: 'body',
    })
    expect(split('no frontmatter here')).toEqual({ frontmatter: {}, body: 'no frontmatter here' })
    // A byte-order mark is not part of the first line.
    expect(split('\uFEFF---\nname: x\n---\n').frontmatter).toEqual({ name: 'x' })
  })
})

describe('DROI_USER_DIR', () => {
  test('is the Host-given directory, else the brand folder, and never a relative path', () => {
    expect(userDirFromEnv({ DROI_USER_DIR: '/tmp/somewhere' } as never)).toBe('/tmp/somewhere')
    expect(userDirFromEnv({} as never).endsWith('.droi')).toBe(true)
    expect(() => userDirFromEnv({ DROI_USER_DIR: 'relative/path' } as never)).toThrow()
    // A blank value is the same as none, which is what an unset shell variable looks like.
    expect(userDirFromEnv({ DROI_USER_DIR: '  ' } as never).endsWith('.droi')).toBe(true)
  })

  test('the test harness never points the kernel at the real folder', () => {
    expect(config(data.dir).userDir).toBe(join(data.dir, 'user'))
  })
})
