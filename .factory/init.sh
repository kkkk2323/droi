#!/bin/bash
set -e
pnpm install --frozen-lockfile 2>/dev/null || pnpm install
