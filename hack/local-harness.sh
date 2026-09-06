#!/usr/bin/env bash
# Stand up a throwaway SSH deploy target and exercise helmforge against it.
#
# The target is a container running sshd with the Docker CLI and compose
# plugin, talking to the host daemon over the mounted socket. Containers
# helmforge deploys therefore appear on the host, which is close enough to a
# real remote host to exercise the full lifecycle without touching anything
# that matters.
#
#   ./hack/local-harness.sh up        build the image, start the target
#   ./hack/local-harness.sh demo      run init/plan/apply/status/logs/drift/rollback/destroy
#   ./hack/local-harness.sh down      remove the container, image and ssh entry
#
# Requires: docker, ssh, go. Nothing here touches a real deployment.
set -euo pipefail

HARNESS_DIR="${HARNESS_DIR:-${TMPDIR:-/tmp}/helmforge-harness}"
CONTAINER=helmforge-target
IMAGE=helmforge-target:latest
SSH_HOST=helmforge-target
SSH_PORT=2222
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HF="${HF:-$REPO_ROOT/helmforge}"
SSH_CONFIG="$HOME/.ssh/config"

log() { printf '\n\033[1m== %s\033[0m\n' "$*"; }

build_image() {
  mkdir -p "$HARNESS_DIR"
  if [ ! -f "$HARNESS_DIR/id_hf" ]; then
    ssh-keygen -t ed25519 -N "" -f "$HARNESS_DIR/id_hf" -C helmforge-local-harness -q
  fi

  cat > "$HARNESS_DIR/Dockerfile" <<'DOCKERFILE'
FROM docker:28-cli
RUN apk add --no-cache openssh-server curl && ssh-keygen -A
# adduser -D leaves the account locked ("!" in /etc/shadow) and sshd refuses a
# locked account even for key-only auth. "*" = no password login, not locked.
RUN adduser -D -s /bin/sh deploy && sed -i 's/^deploy:!/deploy:*/' /etc/shadow
RUN mkdir -p /home/deploy/.ssh && chmod 700 /home/deploy/.ssh
COPY id_hf.pub /home/deploy/.ssh/authorized_keys
RUN chmod 600 /home/deploy/.ssh/authorized_keys && chown -R deploy:deploy /home/deploy/.ssh
RUN mkdir -p /opt/apps && chown deploy:deploy /opt/apps
RUN sed -i 's/^#*PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config \
 && sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
# Throwaway harness container: widen the mounted socket rather than guessing
# the host GID. Never do this on a real host.
RUN printf '%s\n' '#!/bin/sh' 'set -e' \
    'if [ -S /var/run/docker.sock ]; then chmod 666 /var/run/docker.sock || true; fi' \
    'exec /usr/sbin/sshd -D -e' > /entrypoint.sh && chmod +x /entrypoint.sh
CMD ["/entrypoint.sh"]
DOCKERFILE

  docker build -q -t "$IMAGE" "$HARNESS_DIR" >/dev/null
}

write_ssh_entry() {
  mkdir -p "$(dirname "$SSH_CONFIG")"
  touch "$SSH_CONFIG"
  if grep -q "Host $SSH_HOST\$" "$SSH_CONFIG" 2>/dev/null; then return; fi
  cat >> "$SSH_CONFIG" <<EOF

# --- helmforge local test harness (safe to delete) ---
Host $SSH_HOST
    HostName 127.0.0.1
    Port $SSH_PORT
    User deploy
    IdentityFile $HARNESS_DIR/id_hf
    IdentitiesOnly yes
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    LogLevel ERROR
# --- end helmforge harness ---
EOF
}

seed_repo() {
  local repo="$HARNESS_DIR/gitops"
  rm -rf "$repo"
  mkdir -p "$repo/environments/staging/apps/demo"

  cat > "$repo/environments/staging/apps/demo/app.yaml" <<EOF
app: demo
env: staging
targets:
  - host: $SSH_HOST
    user: deploy
    port: $SSH_PORT
    path: /opt/apps/demo
source:
  type: compose
  # Deliberately not one of Docker's auto-discovered names, so the -f handling
  # is actually exercised.
  composeFile: compose.prod.yml
deploy:
  strategy: rolling
  health:
    type: http
    url: http://127.0.0.1:8099/
    timeoutSeconds: 60
policy:
  allowedBranches: [main]
  requireCleanWorktree: false
EOF

  cat > "$repo/environments/staging/apps/demo/compose.prod.yml" <<'EOF'
services:
  demo-web:
    image: nginx:1.27-alpine
    container_name: helmforge-demo-web
    restart: unless-stopped
    ports:
      - "8099:80"
EOF

  git -C "$repo" init -q -b main
  git -C "$repo" config user.email harness@local
  git -C "$repo" config user.name harness
  git -C "$repo" add -A
  git -C "$repo" commit -qm "demo app for helmforge harness"
}

up() {
  log "building target image"
  build_image
  write_ssh_entry
  seed_repo

  log "starting target"
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$CONTAINER" -p "$SSH_PORT:22" \
    -v /var/run/docker.sock:/var/run/docker.sock "$IMAGE" >/dev/null

  for _ in $(seq 1 20); do
    if ssh -o ConnectTimeout=3 "$SSH_HOST" true 2>/dev/null; then break; fi
    sleep 1
  done

  ssh "$SSH_HOST" 'echo "target ready as $(whoami); compose $(docker compose version --short)"'
  echo "gitops repo: $HARNESS_DIR/gitops"
}

demo() {
  local repo="$HARNESS_DIR/gitops"
  [ -x "$HF" ] || { log "building helmforge"; (cd "$REPO_ROOT" && go build -o "$HF" ./cmd/helmforge); }

  log "plan (read-only, never touches the host)"
  "$HF" plan --env staging --app demo --repo "$repo"

  log "apply"
  "$HF" apply --env staging --app demo --repo "$repo"

  log "the deployed service"
  docker ps --filter name=helmforge-demo-web --format '{{.Names}}  {{.Image}}  {{.Ports}}'
  curl -s -o /dev/null -w 'HTTP %{http_code}\n' http://127.0.0.1:8099/

  log "status"
  "$HF" status --env staging --app demo --repo "$repo"

  log "logs"
  "$HF" logs --env staging --app demo --repo "$repo" --tail 5

  log "introduce drift by moving the repo ahead"
  sed -i 's|nginx:1.27-alpine|nginx:1.29-alpine|' "$repo/environments/staging/apps/demo/compose.prod.yml"
  git -C "$repo" commit -aqm "bump nginx"
  "$HF" drift --env staging --repo "$repo" --all

  log "apply the bump, then confirm drift clears"
  "$HF" apply --env staging --app demo --repo "$repo" >/dev/null
  docker ps --filter name=helmforge-demo-web --format 'now running {{.Image}}'
  "$HF" drift --env staging --repo "$repo" --all

  log "rollback to the first release"
  local first
  first="$("$HF" status --env staging --app demo 2>/dev/null | awk '/^Release:/{print $2}')"
  echo "latest release is $first - use 'helmforge rollback --to <id>' with an earlier one"

  log "destroy"
  "$HF" destroy --env staging --app demo --repo "$repo" --yes
}

down() {
  log "removing target"
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  docker rmi -f "$IMAGE" >/dev/null 2>&1 || true
  docker rm -f helmforge-demo-web >/dev/null 2>&1 || true

  if [ -f "$SSH_CONFIG" ] && grep -q "helmforge local test harness" "$SSH_CONFIG"; then
    # Drop the block between the harness markers.
    sed -i '/# --- helmforge local test harness/,/# --- end helmforge harness ---/d' "$SSH_CONFIG"
    echo "removed ssh config entry"
  fi

  rm -rf "$HARNESS_DIR"
  echo "harness removed"
}

case "${1:-up}" in
  up)   up ;;
  demo) demo ;;
  down) down ;;
  *)    echo "usage: $0 {up|demo|down}" >&2; exit 2 ;;
esac
