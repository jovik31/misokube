#!/usr/bin/env bash
set -euo pipefail

MAP_PATH=${1:-/sys/fs/bpf/setera/tc/tc_podIDs}
NODE_CONTAINER=${2:-}
BPFCMD=${BPFCMD:-}

if ! command -v docker >/dev/null 2>&1; then
  echo "docker not found" >&2
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 not found" >&2
  exit 1
fi

find_bpftool_cmd() {
  local container=$1
  docker exec "$container" sh -lc '
for c in bpftool /usr/sbin/bpftool /sbin/bpftool /usr/bin/bpftool; do
  if command -v "$c" >/dev/null 2>&1; then
    command -v "$c"
    exit 0
  fi
  if [ -x "$c" ]; then
    echo "$c"
    exit 0
  fi
done
exit 1
' 2>/dev/null
}

if [[ -z "$NODE_CONTAINER" ]]; then
  while IFS= read -r candidate; do
    [[ -z "$candidate" ]] && continue
    if cmd=$(find_bpftool_cmd "$candidate"); then
      NODE_CONTAINER=$candidate
      BPFCMD=$cmd
      break
    fi
  done < <(docker ps --format '{{.Names}}' | grep -E 'control-plane$|worker[0-9]*$' || true)
fi

if [[ -z "$NODE_CONTAINER" ]]; then
  echo "no kind node container found; pass node name as second arg" >&2
  exit 1
fi

if ! docker ps --format '{{.Names}}' | grep -Fxq "$NODE_CONTAINER"; then
  echo "container not running: $NODE_CONTAINER" >&2
  exit 1
fi

if [[ -z "$BPFCMD" ]]; then
  if ! BPFCMD=$(find_bpftool_cmd "$NODE_CONTAINER"); then
    echo "bpftool not found inside container: $NODE_CONTAINER" >&2
    exit 1
  fi
fi

if ! docker exec "$NODE_CONTAINER" sh -lc "test -e '$MAP_PATH'"; then
  echo "map path not found in container $NODE_CONTAINER: $MAP_PATH" >&2
  exit 1
fi

echo "node_container=$NODE_CONTAINER map_path=$MAP_PATH bpftool=$BPFCMD" >&2

docker exec "$NODE_CONTAINER" "$BPFCMD" -j map dump pinned "$MAP_PATH" | python3 -c '
import json
import sys

def b(v):
  if isinstance(v, int):
    return v
  if isinstance(v, str):
    return int(v, 0)
  raise TypeError(f"unsupported byte value type: {type(v)}")

def ip4(a):
  return f"{b(a[0])}.{b(a[1])}.{b(a[2])}.{b(a[3])}"

def le32(a):
  return b(a[0]) + (b(a[1]) << 8) + (b(a[2]) << 16) + (b(a[3]) << 24)

def tenant(v):
  chars = []
  for b in v[:64]:
    bv = b if isinstance(b, int) else int(b, 0)
    if bv == 0:
      break
    chars.append(chr(bv) if 32 <= bv <= 126 else "")
  return "".join(chars)

data = json.load(sys.stdin)
print("POD_IP\tTENANT\tIFINDEX")
for row in data:
  idx = le32(row["value"][64:68])
  ifindex = "remote" if idx == 0xFFFFFFFF else str(idx)
  print("{}\t{}\t{}".format(ip4(row["key"]), tenant(row["value"]), ifindex))
'
