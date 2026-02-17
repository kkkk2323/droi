import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

import { commitWorkflow } from '../src/backend/git/commitWorkflow.ts'
import { extractFirstJsonObject, stripCodeFences } from '../src/backend/droid/textCapture.ts'
import { parseGitRemoteHost, recommendPrTool } from '../src/backend/git/prTooling.ts'

function exec(cmd: string, args: string[], cwd: string): Promise<string> {
  return new Promise((resolve, reject) => {
    execFile(cmd, args, { cwd, timeout: 30000 }, (err, stdout, stderr) => {
      if (err) return reject(new Error(String(stderr || stdout || err.message || err).trim()))
      resolve(String(stdout || '').trim())
    })
  })
}

async function initRepo(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), 'droi-test-repo-'))
  await exec('git', ['init', '-b', 'main'], dir)
  await exec('git', ['config', 'user.email', 'test@example.com'], dir)
  await exec('git', ['config', 'user.name', 'Test'], dir)
  await writeFile(join(dir, 'README.md'), 'hello\n', 'utf8')
  await exec('git', ['add', 'README.md'], dir)
  await exec('git', ['commit', '-m', 'chore: init'], dir)
  return dir
}

test('commitWorkflow stages unstaged files when includeUnstaged=true', async () => {
  const dir = await initRepo()
  try {
    await writeFile(join(dir, 'a.txt'), 'a\n', 'utf8')

    const res = await commitWorkflow({
      projectDir: dir,
      includeUnstaged: true,
      commitMessage: 'feat: add a.txt',
      workflow: 'commit',
    })

    const head = await exec('git', ['rev-parse', 'HEAD'], dir)
    const subject = await exec('git', ['log', '-1', '--pretty=%s'], dir)
    const status = await exec('git', ['status', '--porcelain'], dir)

    assert.equal(res.ok, true)
    assert.equal(res.commitHash, head)
    assert.equal(subject, 'feat: add a.txt')
    assert.equal(status, '')
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
})

test('commitWorkflow throws when there are no staged changes', async () => {
  const dir = await initRepo()
  try {
    await assert.rejects(() => commitWorkflow({
      projectDir: dir,
      includeUnstaged: false,
      commitMessage: 'chore: noop',
      workflow: 'commit',
    }))
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
})

test('textCapture helpers strip code fences and extract JSON', () => {
  const fenced = '```json\n{ "commitMessage": "fix: x" }\n```'
  assert.equal(stripCodeFences(fenced), '{ "commitMessage": "fix: x" }')
  assert.equal(extractFirstJsonObject(fenced), '{ "commitMessage": "fix: x" }')
})

test('prTooling recommends gh for GitHub remotes', () => {
  assert.equal(parseGitRemoteHost('https://github.com/org/repo.git'), 'github.com')
  assert.equal(parseGitRemoteHost('git@github.com:org/repo.git'), 'github.com')

  assert.deepEqual(
    recommendPrTool({ originHost: 'github.com', hasGh: true, hasFlow: true }),
    { prTool: 'gh' },
  )
  assert.equal(
    recommendPrTool({ originHost: 'github.com', hasGh: false, hasFlow: true }).prTool,
    null,
  )
})

test('prTooling recommends flow for non-GitHub remotes when available', () => {
  assert.deepEqual(
    recommendPrTool({ originHost: 'codeup.aliyun.com', hasGh: true, hasFlow: true }),
    { prTool: 'flow' },
  )
  assert.equal(
    recommendPrTool({ originHost: 'codeup.aliyun.com', hasGh: true, hasFlow: false }).prTool,
    null,
  )
})
