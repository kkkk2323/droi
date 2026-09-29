// A second process writing to the same Memory database: argv is the
// directory, a label and how many entries to add.
import { openMemoryStore } from '../store'

const [dir, label, count] = process.argv.slice(2)
const store = openMemoryStore(dir!)
for (let i = 0; i < Number(count); i++) {
  const result = store.add({ scope: 'project', workspace: '/w' }, 'insight', `${label} note ${i}`)
  if (!result.ok) throw new Error(result.reason)
}
store.close()
