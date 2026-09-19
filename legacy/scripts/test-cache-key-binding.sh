#!/bin/bash
#!/bin/bash
#
# Determine whether Factory prompt cache is:
#   (a) persisted across sessions
#   (b) isolated per API key, or shared across keys (e.g. org-level)
#
# This script is designed to be *definitive* by adding two controls:
#   1) It runs droid exec with an ISOLATED HOME by default, so browser-login creds
#      cannot silently override FACTORY_API_KEY.
#   2) It performs an INVALID KEY check and aborts if droid exec still succeeds.
#
# Test modes:
#   - TEST_MODE=same-session (default):
#       Create ONE session with KEY_A, then continue that SAME session with KEY_A and KEY_B.
#       This directly answers "session" vs "session+key" caching.
#   - TEST_MODE=multi-session:
#       Independent sessions only (useful when session continuation is broken).

set -euo pipefail

ORIG_HOME="$HOME"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="${TEST_CWD:-$(cd "$SCRIPT_DIR/.." && pwd)}"

KEY_A="${TEST_KEY_A:-}"
KEY_B="${TEST_KEY_B:-}"
MODEL="${TEST_MODEL:-kimi-k2.5}"
MODE="${TEST_MODE:-same-session}"
RUNS_PER_KEY="${TEST_RUNS_PER_KEY:-4}"
PAYLOAD_LINES="${TEST_PAYLOAD_LINES:-2500}"
FOLLOW_TURNS="${TEST_FOLLOW_TURNS:-2}"

if [[ -z "$KEY_A" || -z "$KEY_B" ]]; then
  echo "Usage: TEST_KEY_A=fk-xxx TEST_KEY_B=fk-yyy bash $0"
  exit 1
fi

if [[ "$KEY_A" == "$KEY_B" ]]; then
  echo "ERROR: TEST_KEY_A and TEST_KEY_B must be different."
  exit 1
fi

if ! [[ "$RUNS_PER_KEY" =~ ^[0-9]+$ ]] || [[ "$RUNS_PER_KEY" -lt 2 ]]; then
  echo "ERROR: TEST_RUNS_PER_KEY must be an integer >= 2 (got: $RUNS_PER_KEY)"
  exit 1
fi

if ! [[ "$PAYLOAD_LINES" =~ ^[0-9]+$ ]] || [[ "$PAYLOAD_LINES" -lt 100 ]]; then
  echo "ERROR: TEST_PAYLOAD_LINES must be an integer >= 100 (got: $PAYLOAD_LINES)"
  exit 1
fi

if ! [[ "$FOLLOW_TURNS" =~ ^[0-9]+$ ]] || [[ "$FOLLOW_TURNS" -lt 1 ]]; then
  echo "ERROR: TEST_FOLLOW_TURNS must be an integer >= 1 (got: $FOLLOW_TURNS)"
  exit 1
fi

DROID="${DROID_PATH:-$ORIG_HOME/.local/bin/droid}"
if [[ ! -x "$DROID" ]]; then
  DROID="$(command -v droid || true)"
fi

if [[ -z "$DROID" || ! -x "$DROID" ]]; then
  echo "ERROR: droid binary not found. Set DROID_PATH or ensure 'droid' is on PATH."
  exit 1
fi

USE_ISOLATED_HOME="${TEST_USE_REAL_HOME:-0}"
ISOLATED_HOME=""
TMP_DIR=""

cleanup() {
  if [[ -n "$TMP_DIR" && -d "$TMP_DIR" ]]; then
    rm -rf "$TMP_DIR" || true
  fi
  if [[ -n "$ISOLATED_HOME" && -d "$ISOLATED_HOME" ]]; then
    rm -rf "$ISOLATED_HOME" || true
  fi
}
trap cleanup EXIT

if [[ "$USE_ISOLATED_HOME" == "0" ]]; then
  ISOLATED_HOME="$(mktemp -d)"
  export HOME="$ISOLATED_HOME"
else
  ISOLATED_HOME="(real home)"
fi

SESSIONS_DIR="$HOME/.factory/sessions"
SESSION_DIR_NAME=$(echo "$PROJECT_DIR" | sed 's|/|-|g')

PROMPT_ID=$(python3 -c "import uuid; print(uuid.uuid4())")
export PROMPT_ID

TMP_DIR=$(mktemp -d)
PROMPT_FILE="$TMP_DIR/prompt-$PROMPT_ID.txt"

export PAYLOAD_LINES

python3 - << 'PY' > "$PROMPT_FILE"
import os
pid = os.environ.get('PROMPT_ID', 'missing')
lines = int(os.environ.get('PAYLOAD_LINES', '2500'))

# Large deterministic payload to encourage prompt caching.
# (Make it big enough to be worth caching, but not enormous.)
payload_line = "CACHE_PAYLOAD_" + ("0123456789abcdef" * 16)

print(f"CACHE_KEY_BINDING_TEST_PROMPT_V2 id={pid}")
print("Reply with exactly: OK")
print("Do not use tools. Do not add any other text.")
print("\nPAYLOAD_BEGIN")
for i in range(lines):
    print(f"{i:04d} {payload_line}")
print("PAYLOAD_END")
PY

read_usage() {
  local session_id="$1"
  local settings_file="$SESSIONS_DIR/$SESSION_DIR_NAME/$session_id.settings.json"
  python3 -c "
import json
with open('$settings_file') as f:
  d = json.load(f)
u = d.get('tokenUsage', {})
print(u.get('inputTokens', 0), u.get('outputTokens', 0), u.get('cacheReadTokens', 0), u.get('cacheCreationTokens', 0))"
}

wait_for_settings() {
  local session_id="$1"
  local settings_file="$SESSIONS_DIR/$SESSION_DIR_NAME/$session_id.settings.json"
  local tries=0
  while [[ ! -f "$settings_file" && $tries -lt 30 ]]; do
    sleep 0.2
    tries=$((tries + 1))
  done
  [[ -f "$settings_file" ]]
}

run_exec() {
  local key="$1"
  local quiet="${2:-0}"
  local stderr_file
  stderr_file="$(mktemp)"

  local out
  if ! out=$(FACTORY_API_KEY="$key" "$DROID" exec \
      --output-format json \
      --auto low \
      --model "$MODEL" \
      --cwd "$PROJECT_DIR" \
      --file "$PROMPT_FILE" \
      2>"$stderr_file"); then
    if [[ "$quiet" != "1" ]]; then
      echo "ERROR: droid exec failed (exit != 0). stderr:" >&2
      head -20 "$stderr_file" >&2
    fi
    rm -f "$stderr_file"
    return 1
  fi

  rm -f "$stderr_file"

  local sid
  sid=$(echo "$out" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('session_id',''))" 2>/dev/null || true)
  local is_error
  is_error=$(echo "$out" | python3 -c "import json,sys; d=json.load(sys.stdin); print('1' if d.get('is_error') else '0')" 2>/dev/null || echo "1")

  if [[ -z "$sid" ]]; then
    if [[ "$quiet" != "1" ]]; then
      echo "ERROR: Could not parse JSON output from droid exec." >&2
      echo "Raw output (first 5 lines):" >&2
      echo "$out" | head -5 >&2
    fi
    return 1
  fi

  if [[ "$is_error" == "1" ]]; then
    if [[ "$quiet" != "1" ]]; then
      echo "ERROR: droid exec returned is_error=true for session $sid" >&2
    fi
    return 1
  fi

  echo "$sid"
}

echo "=== Factory Cache Binding Test (definitive) ==="
echo "Model: $MODEL"
echo "Project: $PROJECT_DIR"
echo "Prompt ID: $PROMPT_ID"
echo "Mode: $MODE"
echo "Runs per key: $RUNS_PER_KEY"
echo "Payload lines: $PAYLOAD_LINES"
echo "Follow turns (same-session only): $FOLLOW_TURNS"
echo "Home mode: $ISOLATED_HOME"
echo ""

echo "--- Control 0: invalid key must FAIL (proves FACTORY_API_KEY is used) ---"
set +e
SID_INVALID=$(run_exec "fk-INVALID-TEST" 1 2>/dev/null)
RC=$?
set -e

if [[ $RC -eq 0 && -n "$SID_INVALID" ]]; then
  echo "FAILED CONTROL: droid exec succeeded even with an invalid key." >&2
  echo "This means FACTORY_API_KEY is not being used (likely browser-login auth is taking precedence)." >&2
  echo "Rerun with isolated HOME (default) and ensure no other auth mechanism is active." >&2
  echo "If this still happens, this test cannot determine key-bound caching." >&2
  exit 2
fi

echo "OK (invalid key rejected)"
echo ""

run_and_report() {
  local label="$1"
  local key="$2"

  local sid
  sid=$(run_exec "$key")
  if ! wait_for_settings "$sid"; then
    echo "ERROR: settings file not found for session $sid" >&2
    exit 1
  fi
  read -r inTok outTok cacheRead cacheCreate < <(read_usage "$sid")
  echo "$label session=$sid input=$inTok output=$outTok cacheRead=$cacheRead cacheCreate=$cacheCreate" >&2
  echo "$cacheRead"
}

median() {
  python3 - "$@" << 'PY'
import sys
xs=[int(x) for x in sys.argv[1:] if x.strip()!='']
xs.sort()
if not xs:
  print(0)
  raise SystemExit
n=len(xs)
mid=n//2
if n%2==1:
  print(xs[mid])
else:
  print((xs[mid-1]+xs[mid])//2)
PY
}

declare -a A=()
declare -a B=()

get_settings_path() {
  local session_id="$1"
  echo "$SESSIONS_DIR/$SESSION_DIR_NAME/$session_id.settings.json"
}

read_usage_json() {
  local session_id="$1"
  local settings_file
  settings_file="$(get_settings_path "$session_id")"
  python3 -c "
import json
with open('$settings_file') as f:
  d = json.load(f)
u = d.get('tokenUsage', {})
print(json.dumps({
  'input': int(u.get('inputTokens', 0) or 0),
  'output': int(u.get('outputTokens', 0) or 0),
  'cacheRead': int(u.get('cacheReadTokens', 0) or 0),
  'cacheCreate': int(u.get('cacheCreationTokens', 0) or 0),
}))"
}

usage_field() {
  local json="$1"
  local field="$2"
  echo "$json" | python3 -c "import json,sys; print(json.load(sys.stdin).get('$field', 0))"
}

print_usage_line() {
  local label="$1"
  local session_id="$2"
  local ujson
  ujson=$(read_usage_json "$session_id")
  local inTok outTok cacheRead cacheCreate
  inTok=$(usage_field "$ujson" input)
  outTok=$(usage_field "$ujson" output)
  cacheRead=$(usage_field "$ujson" cacheRead)
  cacheCreate=$(usage_field "$ujson" cacheCreate)
  echo "$label session=$session_id input=$inTok output=$outTok cacheRead=$cacheRead cacheCreate=$cacheCreate" >&2
}

write_seed_and_follow_prompts() {
  local seed_file="$1"
  local follow_file="$2"

  # Seed prompt: large deterministic payload to be cached into the session prefix.
  python3 - << 'PY' > "$seed_file"
import os
pid = os.environ.get('PROMPT_ID', 'missing')
lines = int(os.environ.get('PAYLOAD_LINES', '2500'))

payload_line = "CACHE_PAYLOAD_" + ("0123456789abcdef" * 16)

print(f"CACHE_KEY_BINDING_TEST_SEED id={pid}")
print("Reply with exactly: OK")
print("Do not use tools. Do not add any other text.")
print("\nPAYLOAD_BEGIN")
for i in range(lines):
    print(f"{i:04d} {payload_line}")
print("PAYLOAD_END")
PY

  # Follow prompt: tiny message. If session prompt caching works, this turn should
  # show large cacheRead tokens because the prefix (including the big seed) is reused.
  cat > "$follow_file" << EOF
CACHE_KEY_BINDING_TEST_FOLLOW id=$PROMPT_ID
Reply with exactly: OK
Do not use tools. Do not add any other text.
EOF
}

SEED_PROMPT_FILE="$TMP_DIR/seed-$PROMPT_ID.txt"
FOLLOW_PROMPT_FILE="$TMP_DIR/follow-$PROMPT_ID.txt"
write_seed_and_follow_prompts "$SEED_PROMPT_FILE" "$FOLLOW_PROMPT_FILE"

run_exec_with_file() {
  local key="$1"
  local prompt_file="$2"
  local quiet="${3:-0}"

  local stderr_file
  stderr_file="$(mktemp)"

  local out
  if ! out=$(FACTORY_API_KEY="$key" "$DROID" exec \
      --output-format json \
      --auto low \
      --model "$MODEL" \
      --cwd "$PROJECT_DIR" \
      --file "$prompt_file" \
      2>"$stderr_file"); then
    if [[ "$quiet" != "1" ]]; then
      echo "ERROR: droid exec failed (exit != 0). stderr:" >&2
      head -20 "$stderr_file" >&2
    fi
    rm -f "$stderr_file"
    return 1
  fi
  rm -f "$stderr_file"

  local sid
  sid=$(echo "$out" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('session_id',''))" 2>/dev/null || true)
  local is_error
  is_error=$(echo "$out" | python3 -c "import json,sys; d=json.load(sys.stdin); print('1' if d.get('is_error') else '0')" 2>/dev/null || echo "1")

  if [[ -z "$sid" ]]; then
    if [[ "$quiet" != "1" ]]; then
      echo "ERROR: Could not parse JSON output from droid exec." >&2
      echo "Raw output (first 5 lines):" >&2
      echo "$out" | head -5 >&2
    fi
    return 1
  fi

  if [[ "$is_error" == "1" ]]; then
    if [[ "$quiet" != "1" ]]; then
      echo "ERROR: droid exec returned is_error=true for session $sid" >&2
    fi
    return 1
  fi

  echo "$sid"
}

continue_session_with_file() {
  local key="$1"
  local session_id="$2"
  local prompt_file="$3"

  # droid exec -s may return empty output + exit 0 in some versions. We treat the
  # call as successful ONLY if the session settings tokenUsage changes.
  local before after
  before=$(read_usage_json "$session_id")

  local stderr_file stdout_file rc
  stderr_file="$(mktemp)"
  stdout_file="$(mktemp)"
  set +e
  FACTORY_API_KEY="$key" "$DROID" exec \
    --output-format json \
    --auto low \
    --model "$MODEL" \
    --cwd "$PROJECT_DIR" \
    -s "$session_id" \
    --file "$prompt_file" \
    1>"$stdout_file" 2>"$stderr_file"
  rc=$?
  set -e
  if [[ $rc -ne 0 ]]; then
    echo "CONTINUE_ERROR (exit=$rc):" >&2
    head -20 "$stderr_file" >&2
    if [[ -s "$stdout_file" ]]; then
      echo "CONTINUE_STDOUT (first 5 lines):" >&2
      head -5 "$stdout_file" >&2
    fi
  fi

  rm -f "$stderr_file"
  rm -f "$stdout_file"

  sleep 1
  after=$(read_usage_json "$session_id")

  local b_in b_out b_cr b_cc a_in a_out a_cr a_cc
  b_in=$(usage_field "$before" input)
  b_out=$(usage_field "$before" output)
  b_cr=$(usage_field "$before" cacheRead)
  b_cc=$(usage_field "$before" cacheCreate)
  a_in=$(usage_field "$after" input)
  a_out=$(usage_field "$after" output)
  a_cr=$(usage_field "$after" cacheRead)
  a_cc=$(usage_field "$after" cacheCreate)

  if [[ $a_in -eq $b_in && $a_out -eq $b_out && $a_cr -eq $b_cr && $a_cc -eq $b_cc ]]; then
    echo "NO_CHANGE"
    return 0
  fi

  echo $((a_cr - b_cr))
}

if [[ "$MODE" == "same-session" ]]; then
  echo "--- Same-session test ---" >&2

  echo "Seed: create session with KEY_A" >&2
  SID=$(run_exec_with_file "$KEY_A" "$SEED_PROMPT_FILE")
  if ! wait_for_settings "$SID"; then
    echo "ERROR: settings file not found for session $SID" >&2
    exit 1
  fi
  print_usage_line "SEED" "$SID"

  # Warm with KEY_A
  declare -a ADELTA=()
  for i in $(seq 1 "$FOLLOW_TURNS"); do
    d=$(continue_session_with_file "$KEY_A" "$SID" "$FOLLOW_PROMPT_FILE")
    if [[ "$d" == "NO_CHANGE" ]]; then
      echo "ERROR: droid exec -s did not update the session (no tokenUsage change)." >&2
      echo "This droid version likely has broken session continuation; cannot do same-session test." >&2
      echo "Tip: rerun with TEST_MODE=multi-session instead." >&2
      exit 3
    fi
    ADELTA+=("$d")
    print_usage_line "A_follow_$i" "$SID"
  done

  # Switch to KEY_B
  declare -a BDELTA=()
  for i in $(seq 1 "$FOLLOW_TURNS"); do
    d=$(continue_session_with_file "$KEY_B" "$SID" "$FOLLOW_PROMPT_FILE")
    if [[ "$d" == "NO_CHANGE" ]]; then
      echo ""
      echo "=== Conclusion (same-session) ==="
      echo "KEY_A can continue session $SID, but KEY_B cannot (no tokenUsage change / exec error)." 
      echo "This means session continuation is scoped to the authentication context (API key / account)." 
      echo "So the answer to 'session vs session+key' is: it is NOT session-only; it is at least session+key (or session+identity)."
      exit 0
    fi
    BDELTA+=("$d")
    print_usage_line "B_follow_$i" "$SID"
  done

  # Switch back to KEY_A (sanity check)
  declare -a A2DELTA=()
  for i in $(seq 1 "$FOLLOW_TURNS"); do
    d=$(continue_session_with_file "$KEY_A" "$SID" "$FOLLOW_PROMPT_FILE")
    if [[ "$d" == "NO_CHANGE" ]]; then
      echo "ERROR: droid exec -s did not update the session after switching back (no tokenUsage change)." >&2
      exit 3
    fi
    A2DELTA+=("$d")
    print_usage_line "A_back_$i" "$SID"
  done

  AMED=$(median "${ADELTA[@]}")
  BMED=$(median "${BDELTA[@]}")
  A2MED=$(median "${A2DELTA[@]}")

  echo ""
  echo "=== Summary (same-session cacheRead delta per follow turn) ==="
  echo "A_median=$AMED (KEY_A)"
  echo "B_median=$BMED (KEY_B)"
  echo "A_back_median=$A2MED (KEY_A again)"
  echo ""

  if [[ $BMED -lt $((AMED / 3)) && $A2MED -gt $((AMED * 2 / 3)) ]]; then
    echo "CONCLUSION: Cache is SESSION+KEY bound (switching key breaks cache within the same session)."
  elif [[ $BMED -gt $((AMED * 2 / 3)) ]]; then
    echo "CONCLUSION: Cache is SESSION-bound (switching key does NOT break cache within the same session)."
  else
    echo "CONCLUSION: Inconclusive."
    echo "Try increasing TEST_FOLLOW_TURNS or TEST_PAYLOAD_LINES."
  fi

  exit 0
fi

if [[ "$MODE" != "multi-session" ]]; then
  echo "ERROR: Unknown TEST_MODE=$MODE (use same-session or multi-session)" >&2
  exit 1
fi

echo "--- Multi-session test (details printed to stderr) ---" >&2

for i in $(seq 1 "$RUNS_PER_KEY"); do
  A+=("$(run_and_report "A$i" "$KEY_A")")
done

for i in $(seq 1 "$RUNS_PER_KEY"); do
  B+=("$(run_and_report "B$i" "$KEY_B")")
done

A1=${A[0]}
B1=${B[0]}

AWARM_MED=$(median "${A[@]:1}")
BWARM_MED=$(median "${B[@]:1}")

echo ""
echo "=== Summary (cacheReadTokens) ==="
echo "A1=$A1"
echo "A_warm_median=$AWARM_MED (median of A2..A$RUNS_PER_KEY)"
echo "B1=$B1"
echo "B_warm_median=$BWARM_MED (median of B2..B$RUNS_PER_KEY)"
echo ""

abs_diff() {
  local a="$1"
  local b="$2"
  if [[ $a -ge $b ]]; then
    echo $((a - b))
  else
    echo $((b - a))
  fi
}

rel_close() {
  local a="$1"
  local b="$2"
  local d
  d=$(abs_diff "$a" "$b")
  local m=$((a > b ? a : b))
  if [[ $m -eq 0 ]]; then
    echo "1"
    return
  fi
  # within 10%
  if [[ $((d * 10)) -le $m ]]; then
    echo "1"
  else
    echo "0"
  fi
}

WARM_EFFECT_A=$((AWARM_MED > A1 ? AWARM_MED - A1 : A1 - AWARM_MED))
WARM_EFFECT_B=$((BWARM_MED > B1 ? BWARM_MED - B1 : B1 - BWARM_MED))

echo "Analysis:"
echo "- Independent sessions are used for every run (so any cache hit is NOT session-only)."
echo "- Warm deltas (median-based): |A_warm-A1|=$WARM_EFFECT_A, |B_warm-B1|=$WARM_EFFECT_B"
echo ""

if [[ $A1 -eq 0 && $AWARM_MED -eq 0 && $B1 -eq 0 && $BWARM_MED -eq 0 ]]; then
  echo "CONCLUSION: No prompt cache observed (cacheReadTokens are all 0)."
  echo "Cannot determine key binding."
  exit 0
fi

# Key-bound vs shared logic:
# - If key-bound: after warming A, B1 should look like A1 (cold), and B2 should jump (warm B).
# - If shared: after warming A, B1 should look like A2 (already warm), and B2 should be similar to B1.

IS_B1_CLOSE_AWARM=$(rel_close "$B1" "$AWARM_MED")
IS_B1_CLOSE_A1=$(rel_close "$B1" "$A1")
IS_BWARM_CLOSE_B1=$(rel_close "$BWARM_MED" "$B1")

if [[ "$IS_B1_CLOSE_AWARM" == "1" && "$IS_BWARM_CLOSE_B1" == "1" ]]; then
  echo "CONCLUSION: Cache is shared across API keys (NOT key-bound), and persists across sessions."
  echo "Evidence: B1≈A_warm_median and B_warm_median≈B1."
elif [[ "$IS_B1_CLOSE_A1" == "1" && "$IS_BWARM_CLOSE_B1" == "0" ]]; then
  echo "CONCLUSION: Cache appears key-bound (each key warms its own cache), but persists across sessions per key."
  echo "Evidence: B1≈A1 (cold) and B_warm_median differs from B1 (warm-up effect)."
else
  echo "CONCLUSION: Inconclusive / noisy signal."
  echo "Observed pattern doesn't cleanly match shared vs key-bound expectations."
  echo "Try rerunning once, or increase TEST_PAYLOAD_LINES (e.g. 6000), or increase TEST_RUNS_PER_KEY (e.g. 6)."
fi
