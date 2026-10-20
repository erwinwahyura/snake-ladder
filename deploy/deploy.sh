#!/usr/bin/env bash
# Ships bin/snake-ladder-linux-amd64 to the server and restarts it.
# The server side (user, systemd unit, env file, Caddy site) is managed by
# the homelab repo's snake_ladder role; this only swaps the binary.
#
#   deploy/deploy.sh deploy@46.224.127.221
#
# A restart ends every game in progress, so it first waits for them to
# finish (healthz "playing" == 0), for up to DRAIN_MINUTES.
set -euo pipefail

target=${1:?usage: deploy/deploy.sh user@host}
bin=bin/snake-ladder-linux-amd64
port=${PORT:-8091}
drain_minutes=${DRAIN_MINUTES:-10}

[ -f "$bin" ] || { echo "missing $bin, run: make build-linux" >&2; exit 1; }

scp -q "$bin" "$target:/tmp/snake-ladder.new"

ssh "$target" PORT="$port" DRAIN_MINUTES="$drain_minutes" bash -s <<'REMOTE'
set -euo pipefail
health="http://127.0.0.1:$PORT/healthz"

if systemctl is-active --quiet snake-ladder; then
  deadline=$(( $(date +%s) + DRAIN_MINUTES * 60 ))
  while :; do
    playing=$(curl -fsS "$health" | grep -o '"playing":[0-9]*' | cut -d: -f2 || echo 0)
    [ "${playing:-0}" -eq 0 ] && break
    if [ "$(date +%s)" -ge "$deadline" ]; then
      echo "Still $playing game(s) running; deploying anyway."
      break
    fi
    echo "Waiting for $playing game(s) to finish..."
    sleep 10
  done
fi

sudo install -m 0755 -o root -g root /tmp/snake-ladder.new /usr/local/bin/snake-ladder
rm -f /tmp/snake-ladder.new
sudo systemctl enable --quiet snake-ladder
sudo systemctl restart snake-ladder

for _ in $(seq 1 15); do
  curl -fsS "$health" >/dev/null 2>&1 && { echo "snake-ladder is up: $(curl -fsS "$health")"; exit 0; }
  sleep 1
done
echo "snake-ladder did not become healthy" >&2
sudo journalctl -u snake-ladder -n 30 --no-pager >&2
exit 1
REMOTE
