#!/bin/bash
set -euo pipefail

pnpm install --frozen-lockfile 2>/dev/null || pnpm install
pnpm -C tests exec playwright install chromium
