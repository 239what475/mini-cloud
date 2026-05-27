#!/usr/bin/env bash
set -euo pipefail

echo "cloud-plane no longer exposes an HTTP teardown endpoint; destroy cleanup must be driven through control-plane or provider resources." >&2
exit 0
