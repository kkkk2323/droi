import test from 'node:test'
import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { mkdtemp, realpath, rm, stat, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { createWorkspace, getWorkspaceInfo, pushBranch, removeWorktree } from '../src/backend/git/workspaceManager.ts'

function runGit(args: string[], cwd: string): Promise<string> {
  return new Promise((resolvePromise, rejectPromise) => {
    execFile('git', args, { cwd, timeout: 8000 }, (err, stdout, stderr) => {
      if (err) {
        rejectPromise(new Error(String(stderr || err.message || 'git command failed').trim()))
        return
      }
      resolvePromise(String(stdout || '').trim())
    })
  })
}

test('workspaceManager detects worktrees and uses common repoRoot', async () => {
  const repoDir = await mkdtemp(join(tmpdir(), 'droid-worktree-'))

  await runGit(['init'], repoDir)

  await runGit(['config', 'user.email', 'test@example.com'], repoDir)
  await runGit(['config', 'user.name', 'test'], repoDir)
  await writeFile(join(repoDir, 'a.txt'), 'a\n')
  await runGit(['add', 'a.txt'], repoDir)
  await runGit(['commit', '-m', 'init'], repoDir)
  await runGit(['branch', '-M', 'main'], repoDir)

  const repoReal = await realpath(repoDir)
  const mainInfo = await getWorkspaceInfo(repoDir)
  assert.equal(mainInfo.repoRoot, repoReal)
  assert.equal(mainInfo.projectDir, repoReal)
  assert.equal(mainInfo.workspaceType, 'branch')

  const wtInfo = await createWorkspace({
    projectDir: repoDir,
    mode: 'worktree',
    branch: 'feature/x',
    baseBranch: 'main',
  })
  const expectedWorktreeDir = resolve(repoDir, '.worktrees', 'feature--x')

  const s = await stat(expectedWorktreeDir)
  assert.ok(s.isDirectory())
  const expectedWorktreeReal = await realpath(expectedWorktreeDir)

  assert.equal(wtInfo.repoRoot, repoReal)
  assert.equal(wtInfo.projectDir, expectedWorktreeReal)
  assert.equal(wtInfo.workspaceType, 'worktree')
  assert.equal(wtInfo.branch, 'feature/x')
})

test('workspaceManager removeWorktree removes the worktree directory', async () => {
  const repoDir = await mkdtemp(join(tmpdir(), 'droid-worktree-remove-'))
  await runGit(['init'], repoDir)
  await runGit(['config', 'user.email', 'test@example.com'], repoDir)
  await runGit(['config', 'user.name', 'test'], repoDir)
  await writeFile(join(repoDir, 'a.txt'), 'a\n')
  await runGit(['add', 'a.txt'], repoDir)
  await runGit(['commit', '-m', 'init'], repoDir)
  await runGit(['branch', '-M', 'main'], repoDir)

  const repoReal = await realpath(repoDir)
  const wtInfo = await createWorkspace({
    projectDir: repoDir,
    mode: 'worktree',
    branch: 'feature/remove',
    baseBranch: 'main',
  })

  await removeWorktree({ repoRoot: repoReal, worktreeDir: wtInfo.projectDir, force: true })
  await assert.rejects(stat(wtInfo.projectDir))
})

test('workspaceManager removeWorktree tolerates already-removed worktrees', async () => {
  const repoDir = await mkdtemp(join(tmpdir(), 'droid-worktree-remove-missing-'))
  await runGit(['init'], repoDir)
  await runGit(['config', 'user.email', 'test@example.com'], repoDir)
  await runGit(['config', 'user.name', 'test'], repoDir)
  await writeFile(join(repoDir, 'a.txt'), 'a\n')
  await runGit(['add', 'a.txt'], repoDir)
  await runGit(['commit', '-m', 'init'], repoDir)
  await runGit(['branch', '-M', 'main'], repoDir)

  const repoReal = await realpath(repoDir)
  const wtInfo = await createWorkspace({
    projectDir: repoDir,
    mode: 'worktree',
    branch: 'feature/remove-missing',
    baseBranch: 'main',
  })

  await rm(wtInfo.projectDir, { recursive: true, force: true })
  await removeWorktree({ repoRoot: repoReal, worktreeDir: wtInfo.projectDir, force: true })

  const listOut = await runGit(['worktree', 'list', '--porcelain'], repoReal)
  assert.equal(listOut.includes(wtInfo.projectDir), false)
})

test('workspaceManager pushBranch pushes the current branch to origin', async () => {
  const repoDir = await mkdtemp(join(tmpdir(), 'droid-worktree-push-'))
  await runGit(['init'], repoDir)
  await runGit(['config', 'user.email', 'test@example.com'], repoDir)
  await runGit(['config', 'user.name', 'test'], repoDir)
  await writeFile(join(repoDir, 'a.txt'), 'a\n')
  await runGit(['add', 'a.txt'], repoDir)
  await runGit(['commit', '-m', 'init'], repoDir)
  await runGit(['branch', '-M', 'main'], repoDir)

  const remoteRoot = await mkdtemp(join(tmpdir(), 'droid-remote-'))
  const remoteBare = join(remoteRoot, 'origin.git')
  await runGit(['init', '--bare', 'origin.git'], remoteRoot)
  await runGit(['remote', 'add', 'origin', remoteBare], repoDir)

  const wtInfo = await createWorkspace({
    projectDir: repoDir,
    mode: 'worktree',
    branch: 'droi/push-test',
    baseBranch: 'main',
  })
  await writeFile(join(wtInfo.projectDir, 'b.txt'), 'b\n')
  await runGit(['add', 'b.txt'], wtInfo.projectDir)
  await runGit(['commit', '-m', 'feat: worktree'], wtInfo.projectDir)

  const res = await pushBranch({ projectDir: wtInfo.projectDir, remote: 'origin' })
  assert.equal(res.remote, 'origin')
  assert.equal(res.branch, 'droi/push-test')

  const refLine = await runGit(['show-ref', '--verify', `refs/heads/${res.branch}`], remoteBare)
  assert.ok(refLine.includes(`refs/heads/${res.branch}`))
})
