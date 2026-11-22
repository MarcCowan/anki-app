// Builds the web app and the server, then runs the server in the foreground
// (Playwright's webServer). Cross-platform: no shell-specific syntax.
import { spawn, spawnSync } from 'node:child_process'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const web = join(dirname(fileURLToPath(import.meta.url)), '..')
const server = join(web, '..', 'server')
const exe = join(server, process.platform === 'win32' ? 'anki-app.exe' : 'anki-app')

function run(cmd, args, cwd) {
  const r = spawnSync(cmd, args, { cwd, stdio: 'inherit', shell: process.platform === 'win32' })
  if (r.status !== 0) {
    console.error(`${cmd} ${args.join(' ')} failed`)
    process.exit(r.status ?? 1)
  }
}

run('npm', ['run', 'build'], web)
run('go', ['build', '-o', exe, './cmd/anki-app'], server)

const child = spawn(exe, ['serve'], { stdio: 'inherit', env: process.env })
const stop = () => child.kill()
process.on('SIGINT', stop)
process.on('SIGTERM', stop)
child.on('exit', (code) => process.exit(code ?? 0))
