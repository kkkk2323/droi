import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, mkdir, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { scanSlashCommands, resolveSlashCommandText } from '../src/backend/slashCommands/slashCommands.ts'

test('scanSlashCommands finds commands and applies project-over-user precedence', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-slash-'))
  const projectDir = join(dir, 'proj')
  const userRoot = join(dir, 'user-commands')
  await mkdir(join(projectDir, '.factory', 'commands'), { recursive: true })
  await mkdir(join(projectDir, '.factory', 'commands', 'git'), { recursive: true })
  await mkdir(userRoot, { recursive: true })

  await writeFile(join(userRoot, 'review.md'), `---\ndescription: user review\n---\nUSER $ARGUMENTS\n`)
  await writeFile(join(projectDir, '.factory', 'commands', 'review.md'), `---\ndescription: project review\nargument-hint: <branch>\n---\nPROJECT $ARGUMENTS in $PROJECT_DIR\n`)
  await writeFile(join(projectDir, '.factory', 'commands', 'git', 'status.md'), `Show status`)

  const cmds = await scanSlashCommands({ projectDir, userCommandsDir: userRoot })
  assert.ok(cmds.has('review'))
  assert.ok(cmds.has('git/status'))

  const review = cmds.get('review')!
  assert.equal(review.scope, 'project')
  assert.equal(review.description, 'project review')
  assert.equal(review.argumentHint, '<branch>')
  assert.match(review.filePath, /review\.md$/)
  assert.equal(review.body, 'PROJECT $ARGUMENTS in $PROJECT_DIR')
})

test('scanSlashCommands works without projectDir (user commands only)', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-slash-user-only-'))
  const userRoot = join(dir, 'user-commands')
  await mkdir(userRoot, { recursive: true })
  await writeFile(join(userRoot, 'create-pr.md'), `Hello $ARGUMENTS`)

  const cmds = await scanSlashCommands({ projectDir: undefined, userCommandsDir: userRoot })
  assert.ok(cmds.has('create-pr'))
  assert.equal(cmds.get('create-pr')!.scope, 'user')
})

test('resolveSlashCommandText expands variables and supports // escape', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-slash-resolve-'))
  const projectDir = join(dir, 'proj')
  const userRoot = join(dir, 'user-commands')
  await mkdir(join(projectDir, '.factory', 'commands'), { recursive: true })
  await mkdir(userRoot, { recursive: true })

  await writeFile(join(projectDir, '.factory', 'commands', 'review.md'), `Hello $ARGUMENTS from $PROJECT_DIR`)

  const cmds = await scanSlashCommands({ projectDir, userCommandsDir: userRoot })

  const r1 = resolveSlashCommandText({ text: '/review feature/login', commands: cmds, projectDir })
  assert.equal(r1.matched, true)
  assert.equal(r1.expandedText, `Hello feature/login from ${projectDir}`)
  assert.equal(r1.command?.name, 'review')

  const r2 = resolveSlashCommandText({ text: '//review literal', commands: cmds, projectDir })
  assert.equal(r2.matched, false)
  assert.equal(r2.expandedText, '/review literal')

  const r3 = resolveSlashCommandText({ text: '/unknown x', commands: cmds, projectDir })
  assert.equal(r3.matched, false)
  assert.equal(r3.expandedText, '/unknown x')
})
