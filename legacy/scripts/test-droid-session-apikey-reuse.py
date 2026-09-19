#!/usr/bin/env python3

import json
import os
import subprocess
import tempfile
import time
from pathlib import Path
import select
from typing import Optional


DROID = os.environ.get('DROID_BIN') or str(Path.home() / '.local' / 'bin' / 'droid')
MODEL = os.environ.get('DROID_MODEL') or 'kimi-k2.5'


def run(cmd: list[str], *, cwd: Optional[str] = None) -> None:
  subprocess.run(cmd, cwd=cwd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def start_droid_exec(api_key: str, cwd: str) -> subprocess.Popen:
  env = dict(os.environ)
  env['FACTORY_API_KEY'] = api_key
  return subprocess.Popen(
    [DROID, 'exec', '--input-format', 'stream-jsonrpc', '--output-format', 'stream-jsonrpc', '--cwd', cwd, '--model', MODEL],
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    text=True,
    env=env,
  )


def write_req(p: subprocess.Popen, req: dict) -> None:
  assert p.stdin is not None
  p.stdin.write(json.dumps(req, ensure_ascii=False) + '\n')
  p.stdin.flush()


def read_line(p: subprocess.Popen, timeout_s: float) -> Optional[str]:
  assert p.stdout is not None
  if p.poll() is not None:
    return None

  r, _w, _x = select.select([p.stdout], [], [], timeout_s)
  if not r:
    return None
  line = p.stdout.readline()
  return line or None


def wait_for_response(p: subprocess.Popen, target_id: str, timeout_s: float = 20.0) -> dict:
  start = time.time()
  while time.time() - start < timeout_s:
    line = read_line(p, timeout_s=timeout_s)
    if not line:
      continue
    try:
      msg = json.loads(line)
    except Exception:
      continue
    if msg.get('type') == 'response' and msg.get('id') == target_id:
      return msg
    if msg.get('type') == 'response' and msg.get('error') and msg.get('id') is None:
      # Some errors come back as id:null; surface them.
      return msg
  raise TimeoutError(f'timeout waiting for response id={target_id}')


def capture_assistant_text(p: subprocess.Popen, timeout_s: float = 40.0) -> str:
  start = time.time()
  out: list[str] = []
  while time.time() - start < timeout_s:
    line = read_line(p, timeout_s=timeout_s)
    if not line:
      if p.poll() is not None:
        break
      continue
    try:
      msg = json.loads(line)
    except Exception:
      continue
    if msg.get('type') != 'notification':
      continue
    if msg.get('method') != 'droid.session_notification':
      continue
    notif = (msg.get('params') or {}).get('notification') or {}
    if notif.get('type') == 'assistant_text_delta':
      out.append(str(notif.get('textDelta') or ''))
      # Fast-path: if the model already emitted a newline, usually it's done.
      if ''.join(out).strip().endswith(('.', '!', '?')) or '\n' in ''.join(out):
        return ''.join(out)
  return ''.join(out)


def stop_proc(p: subprocess.Popen) -> None:
  try:
    if p.stdin:
      try:
        p.stdin.close()
      except Exception:
        pass
    p.terminate()
    p.wait(timeout=3)
  except Exception:
    try:
      p.kill()
    except Exception:
      pass


def main() -> int:
  key1 = os.environ.get('KEY1', '').strip()
  key2 = os.environ.get('KEY2', '').strip()
  if not key1 or not key2:
    print('Missing KEY1/KEY2 env vars')
    return 2

  base = Path(tempfile.mkdtemp(prefix='droid-apikey-reuse-'))
  repo = base / 'repo'
  repo.mkdir(parents=True, exist_ok=True)
  run(['git', 'init'], cwd=str(repo))
  (repo / 'README.md').write_text('test\n', encoding='utf-8')
  run(['git', 'add', '.'], cwd=str(repo))
  run(['git', 'commit', '-m', 'init'], cwd=str(repo))

  token = f'TOKEN-{int(time.time())}-{os.urandom(3).hex()}'

  # --- Turn 1: create session under KEY1 ---
  p1 = start_droid_exec(key1, str(repo))
  try:
    write_req(p1, {
      'jsonrpc': '2.0',
      'factoryApiVersion': '1.0.0',
      'type': 'request',
      'id': '1',
      'method': 'droid.initialize_session',
      'params': {
        'machineId': 'm-apikey-reuse',
        'cwd': str(repo),
        'modelId': MODEL,
        'autonomyLevel': 'auto-low',
      },
    })
    r1 = wait_for_response(p1, '1', timeout_s=30)
    sid1 = str(((r1.get('result') or {}).get('sessionId') or '')).strip()
    if not sid1:
      raise RuntimeError(f'initialize_session missing sessionId: {r1}')

    write_req(p1, {
      'jsonrpc': '2.0',
      'factoryApiVersion': '1.0.0',
      'type': 'request',
      'id': '2',
      'method': 'droid.add_user_message',
      'params': {
        'text': f'Remember this token: {token}. Reply ONLY OK.',
      },
    })
    _ = wait_for_response(p1, '2', timeout_s=10)
    a1 = capture_assistant_text(p1, timeout_s=40)
  finally:
    stop_proc(p1)

  # --- Turn 2: attempt resume under KEY2 ---
  p2 = start_droid_exec(key2, str(repo))
  load_ok = False
  load_err = ''
  try:
    write_req(p2, {
      'jsonrpc': '2.0',
      'factoryApiVersion': '1.0.0',
      'type': 'request',
      'id': '1',
      'method': 'droid.initialize_session',
      'params': {
        'machineId': 'm-apikey-reuse',
        'cwd': str(repo),
        'modelId': MODEL,
        'autonomyLevel': 'auto-low',
      },
    })
    _ = wait_for_response(p2, '1', timeout_s=30)

    write_req(p2, {
      'jsonrpc': '2.0',
      'factoryApiVersion': '1.0.0',
      'type': 'request',
      'id': '2',
      'method': 'droid.load_session',
      'params': { 'sessionId': sid1 },
    })
    rload = wait_for_response(p2, '2', timeout_s=30)
    if rload.get('error'):
      load_err = str((rload.get('error') or {}).get('message') or '').strip() or 'unknown'
      load_ok = False
    else:
      load_ok = True

    write_req(p2, {
      'jsonrpc': '2.0',
      'factoryApiVersion': '1.0.0',
      'type': 'request',
      'id': '3',
      'method': 'droid.add_user_message',
      'params': {
        'text': 'What token did I ask you to remember? Reply ONLY the token.',
      },
    })
    _ = wait_for_response(p2, '3', timeout_s=10)
    a2 = capture_assistant_text(p2, timeout_s=60)
  finally:
    stop_proc(p2)

  a1s = (a1 or '').strip()
  a2s = (a2 or '').strip()

  print('turn1.sessionId', sid1)
  print('turn1.assistant', a1s[:200])
  print('turn2.load_session.ok', str(load_ok).lower())
  if load_err:
    print('turn2.load_session.error', load_err)
  print('turn2.assistant', a2s[:400])
  print('token.expected', token)
  print('token.found_in_turn2', str(token in a2s))
  return 0


if __name__ == '__main__':
  raise SystemExit(main())
