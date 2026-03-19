import test from 'node:test'
import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { mkdir, mkdtemp, realpath, rm, stat, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import {
  createWorkspace,
  getWorkspaceInfo,
  pushBranch,
  removeWorktree,
  switchWorkspaceBranch,
} from '../src/backend/git/workspaceManager.ts'

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
  assert.equal(mainInfo.workspaceDir, repoReal)
  assert.equal(mainInfo.workspaceType, 'branch')

  const wtInfo = await createWorkspace({
    workspaceDir: repoDir,
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
  assert.equal(wtInfo.workspaceDir, expectedWorktreeReal)
  assert.equal(wtInfo.workspaceType, 'worktree')
  assert.equal(wtInfo.branch, 'feature/x')
})

test('workspaceManager preserves package cwd metadata for subdirectories', async () => {
  const repoDir = await mkdtemp(join(tmpdir(), 'droid-worktree-subdir-'))

  await runGit(['init'], repoDir)
  await runGit(['config', 'user.email', 'test@example.com'], repoDir)
  await runGit(['config', 'user.name', 'test'], repoDir)
  await mkdir(join(repoDir, 'packages', 'foo'), { recursive: true })
  await writeFile(join(repoDir, 'packages', 'foo', 'index.ts'), 'export const foo = 1\n')
  await runGit(['add', '.'], repoDir)
  await runGit(['commit', '-m', 'init'], repoDir)
  await runGit(['branch', '-M', 'main'], repoDir)

  const subdir = join(repoDir, 'packages', 'foo')
  const info = await getWorkspaceInfo(subdir)

  assert.equal(info.repoRoot, await realpath(repoDir))
  assert.equal(info.workspaceDir, await realpath(repoDir))
  assert.equal(info.projectDir, await realpath(subdir))
  assert.equal(info.cwdSubpath, join('packages', 'foo'))
})

test('workspaceManager rebases subdirectory cwd into new worktrees', async () => {
  const repoDir = await mkdtemp(join(tmpdir(), 'droid-worktree-rebase-subdir-'))

  await runGit(['init'], repoDir)
  await runGit(['config', 'user.email', 'test@example.com'], repoDir)
  await runGit(['config', 'user.name', 'test'], repoDir)
  await mkdir(join(repoDir, 'packages', 'foo'), { recursive: true })
  await writeFile(join(repoDir, 'packages', 'foo', 'index.ts'), 'export const foo = 1\n')
  await runGit(['add', '.'], repoDir)
  await runGit(['commit', '-m', 'init'], repoDir)
  await runGit(['branch', '-M', 'main'], repoDir)

  const wtInfo = await createWorkspace({
    workspaceDir: repoDir,
    projectDir: join(repoDir, 'packages', 'foo'),
    mode: 'worktree',
    branch: 'feature/pkg-foo',
    baseBranch: 'main',
    cwdSubpath: join('packages', 'foo'),
  })

  const expectedWorkspaceDir = await realpath(resolve(repoDir, '.worktrees', 'feature--pkg-foo'))
  const expectedProjectDir = await realpath(join(expectedWorkspaceDir, 'packages', 'foo'))

  assert.equal(wtInfo.workspaceDir, expectedWorkspaceDir)
  assert.equal(wtInfo.projectDir, expectedProjectDir)
  assert.equal(wtInfo.cwdSubpath, join('packages', 'foo'))
})

test('workspaceManager derives subdirectory cwd when creating worktrees without explicit cwdSubpath', async () => {
  const repoDir = await mkdtemp(join(tmpdir(), 'droid-worktree-derive-subdir-'))

  await runGit(['init'], repoDir)
  await runGit(['config', 'user.email', 'test@example.com'], repoDir)
  await runGit(['config', 'user.name', 'test'], repoDir)
  await mkdir(join(repoDir, 'packages', 'foo'), { recursive: true })
  await writeFile(join(repoDir, 'packages', 'foo', 'index.ts'), 'export const foo = 1\n')
  await runGit(['add', '.'], repoDir)
  await runGit(['commit', '-m', 'init'], repoDir)
  await runGit(['branch', '-M', 'main'], repoDir)

  const wtInfo = await createWorkspace({
    workspaceDir: repoDir,
    projectDir: join(repoDir, 'packages', 'foo'),
    mode: 'worktree',
    branch: 'feature/pkg-foo-derived',
    baseBranch: 'main',
  })

  const expectedWorkspaceDir = await realpath(
    resolve(repoDir, '.worktrees', 'feature--pkg-foo-derived'),
  )
  const expectedProjectDir = await realpath(join(expectedWorkspaceDir, 'packages', 'foo'))

  assert.equal(wtInfo.workspaceDir, expectedWorkspaceDir)
  assert.equal(wtInfo.projectDir, expectedProjectDir)
  assert.equal(wtInfo.cwdSubpath, join('packages', 'foo'))
})

test('workspaceManager switches branches using workspaceDir while preserving project cwd', async () => {
  const repoDir = await mkdtemp(join(tmpdir(), 'droid-worktree-switch-workspace-'))

  await runGit(['init'], repoDir)
  await runGit(['config', 'user.email', 'test@example.com'], repoDir)
  await runGit(['config', 'user.name', 'test'], repoDir)
  await mkdir(join(repoDir, 'packages', 'foo'), { recursive: true })
  await writeFile(join(repoDir, 'packages', 'foo', 'index.ts'), 'export const foo = 1\n')
  await runGit(['add', '.'], repoDir)
  await runGit(['commit', '-m', 'init'], repoDir)
  await runGit(['branch', '-M', 'main'], repoDir)
  await runGit(['checkout', '-b', 'feature/foo'], repoDir)
  await runGit(['checkout', 'main'], repoDir)

  const switched = await switchWorkspaceBranch({
    workspaceDir: repoDir,
    branch: 'feature/foo',
    cwdSubpath: join('packages', 'foo'),
  })

  assert.equal(switched.workspaceDir, await realpath(repoDir))
  assert.equal(switched.projectDir, await realpath(join(repoDir, 'packages', 'foo')))
  assert.equal(switched.branch, 'feature/foo')
  assert.equal(switched.cwdSubpath, join('packages', 'foo'))
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
    workspaceDir: repoDir,
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
    workspaceDir: repoDir,
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

test('workspaceManager createWorktree does not keep upstream to origin/main', async () => {
  const repoDir = await mkdtemp(join(tmpdir(), 'droid-worktree-no-upstream-'))
  await runGit(['init'], repoDir)
  await runGit(['config', 'user.email', 'test@example.com'], repoDir)
  await runGit(['config', 'user.name', 'test'], repoDir)
  await writeFile(join(repoDir, 'a.txt'), 'a\n')
  await runGit(['add', 'a.txt'], repoDir)
  await runGit(['commit', '-m', 'init'], repoDir)
  await runGit(['branch', '-M', 'main'], repoDir)

  const remoteRoot = await mkdtemp(join(tmpdir(), 'droid-remote-no-upstream-'))
  const remoteBare = join(remoteRoot, 'origin.git')
  await runGit(['init', '--bare', 'origin.git'], remoteRoot)
  await runGit(['remote', 'add', 'origin', remoteBare], repoDir)
  await runGit(['push', '-u', 'origin', 'main'], repoDir)

  const branch = 'droi/no-upstream-worktree'
  await createWorkspace({
    workspaceDir: repoDir,
    projectDir: repoDir,
    mode: 'worktree',
    branch,
    baseBranch: 'main',
  })

  await assert.rejects(runGit(['config', '--get', `branch.${branch}.merge`], repoDir))
  await assert.rejects(runGit(['config', '--get', `branch.${branch}.remote`], repoDir))
})

test('workspaceManager createBranch does not keep upstream to origin/main', async () => {
  const repoDir = await mkdtemp(join(tmpdir(), 'droid-branch-no-upstream-'))
  await runGit(['init'], repoDir)
  await runGit(['config', 'user.email', 'test@example.com'], repoDir)
  await runGit(['config', 'user.name', 'test'], repoDir)
  await writeFile(join(repoDir, 'a.txt'), 'a\n')
  await runGit(['add', 'a.txt'], repoDir)
  await runGit(['commit', '-m', 'init'], repoDir)
  await runGit(['branch', '-M', 'main'], repoDir)

  const remoteRoot = await mkdtemp(join(tmpdir(), 'droid-remote-no-upstream-branch-'))
  const remoteBare = join(remoteRoot, 'origin.git')
  await runGit(['init', '--bare', 'origin.git'], remoteRoot)
  await runGit(['remote', 'add', 'origin', remoteBare], repoDir)
  await runGit(['push', '-u', 'origin', 'main'], repoDir)

  const branch = 'droi/no-upstream-branch'
  const branchInfo = await createWorkspace({
    workspaceDir: repoDir,
    projectDir: repoDir,
    mode: 'branch',
    branch,
    baseBranch: 'main',
  })
  assert.equal(branchInfo.branch, branch)

  await assert.rejects(runGit(['config', '--get', `branch.${branch}.merge`], repoDir))
  await assert.rejects(runGit(['config', '--get', `branch.${branch}.remote`], repoDir))
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
    workspaceDir: repoDir,
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
