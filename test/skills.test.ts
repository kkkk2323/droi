import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, mkdir, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { scanSkills } from '../src/backend/skills/skills.ts'

async function writeSkill(root: string, name: string, skillMd: string) {
  const dir = join(root, name)
  await mkdir(dir, { recursive: true })
  await writeFile(join(dir, 'SKILL.md'), skillMd)
}

test('scanSkills applies project-over-user precedence and parses description', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-skills-'))
  const projectDir = join(dir, 'proj')
  const userFactory = join(dir, 'user-factory-skills')
  const userAgents = join(dir, 'user-agents-skills')

  await mkdir(join(projectDir, '.factory', 'skills'), { recursive: true })
  await mkdir(userFactory, { recursive: true })
  await mkdir(userAgents, { recursive: true })

  await writeSkill(userAgents, 'agent-browser', '# Agent Browser\n\nBrowse the web.\n')
  await writeSkill(userFactory, 'agent-browser', '# Agent Browser\n\nFactory override.\n')
  await writeSkill(join(projectDir, '.factory', 'skills'), 'agent-browser', '# Agent Browser\n\nProject override.\n')
  await writeSkill(userFactory, 'no-desc', 'Just text\nNo header\n')

  const defs = await scanSkills({ projectDir, userFactorySkillsDir: userFactory, agentsSkillsDir: userAgents })
  const byName = new Map(defs.map((d) => [d.name, d]))

  assert.ok(byName.has('agent-browser'))
  assert.ok(byName.has('no-desc'))

  const ab = byName.get('agent-browser')!
  assert.equal(ab.scope, 'project')
  assert.equal(ab.description, 'Project override.')
  assert.match(ab.filePath, /agent-browser[\\/]+SKILL\.md$/)

  const nd = byName.get('no-desc')!
  assert.equal(nd.scope, 'user')
  assert.ok(!('description' in nd) || !nd.description)
})

test('scanSkills filters out .system paths', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'droid-skills-system-'))
  const systemRoot = join(dir, '.system', 'skills')
  await mkdir(systemRoot, { recursive: true })
  await writeSkill(systemRoot, 'hidden-skill', '# Hidden\n\nShould not be listed.\n')

  const defs = await scanSkills({ projectDir: undefined, userFactorySkillsDir: systemRoot, agentsSkillsDir: join(dir, 'missing') })
  assert.equal(defs.length, 0)
})

