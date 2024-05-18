# helmforge

A GitOps-style deployment engine that deploys apps to remote Linux hosts via SSH using Docker Compose. Uses a Git repository as the source of truth.

## Features

- **plan** — Preview deployment actions without executing anything
- **apply** — Deploy with rolling strategy, health checks, and automatic rollback on failure
- **status** — View latest release and per-host deployment status
- **drift** — Detect when remote state diverges from desired Git state
- **rollback** — Re-deploy a previous release by ID

## Install

```bash
# Build from source (requires Go 1.21+)
go build -o helmforge ./cmd/helmforge

# Or install directly
go install github.com/7irelo/helmforge/cmd/helmforge@latest
```

**Prerequisites on your local machine:**
- `git` CLI
- `ssh` / `scp` CLI (with key-based auth configured)

**Prerequisites on remote hosts:**
- Docker + `docker compose` (v2 plugin)
- SSH access for the deploy user

## Repository Layout

helmforge reads config from a Git repository with this structure:

```
repo/
  environments/
    staging/
      apps/
        reporting-service/
          app.yaml              # App config (required)
          docker-compose.yaml   # Compose file (required)
          .env.example          # Optional reference
    prod/
      apps/
        reporting-service/
          app.yaml
          docker-compose.yaml
```

## app.yaml Schema

```yaml
app: reporting-service          # Application name (required)
env: staging                    # Environment (required)
targets:                        # Deploy targets (at least one required)
  - host: 10.0.1.10            # Hostname or IP (required)
    user: deploy                # SSH user (required)
    port: 22                    # SSH port (default: 22)
    path: /opt/apps/reporting   # Remote path for compose files (required)
  - host: 10.0.1.11
    user: deploy
    path: /opt/apps/reporting
source:
  type: compose                 # Must be "compose" (required)
  composeFile: docker-compose.yaml  # Compose file name (required)
deploy:
  strategy: rolling             # Deploy strategy (default: rolling)
  health:
    type: http                  # Health check type: "http" or "none"
    url: http://10.0.1.10:8080/health
    timeoutSeconds: 30          # Default: 30
policy:
  allowedBranches:              # Optional: restrict deployable branches
    - main
    - staging
  requireCleanWorktree: true    # Optional (default: false)
  requireSignedCommits: false   # Optional (default: false)
```

## Usage

### Plan

Preview what will happen during deployment:

```bash
helmforge plan -e staging -a reporting-service --repo git@github.com:org/infra.git
```

Sample output:
```
Deployment Plan
===============
  App:    reporting-service
  Env:    staging
  Repo:   git@github.com:org/infra.git
  Ref:    main
  Commit: a1b2c3d4e5f6

Host: deploy@10.0.1.10:22
  --------------------------------------------------
  + [ensure_dir] Create remote directory /opt/apps/reporting
      cmd: mkdir -p /opt/apps/reporting
  ~ [copy_files] Copy docker-compose.yaml to 10.0.1.10:/opt/apps/reporting
  > [docker_pull] Pull latest images
      cmd: cd /opt/apps/reporting && docker compose pull
  > [docker_up] Start/update services
      cmd: cd /opt/apps/reporting && docker compose up -d --remove-orphans
  ? [health_check] HTTP health check http://10.0.1.10:8080/health (timeout 30s)
  * [write_marker] Write release marker file

Host: deploy@10.0.1.11:22
  --------------------------------------------------
  ...
```

JSON output for CI:
```bash
helmforge plan -e staging -a reporting-service --repo git@github.com:org/infra.git --json
```

```json
{
  "env": "staging",
  "app": "reporting-service",
  "repo": "git@github.com:org/infra.git",
  "ref": "main",
  "commit_sha": "a1b2c3d4e5f6789...",
  "actions": [
    {
      "host": "deploy@10.0.1.10:22",
      "step": "ensure_dir",
      "description": "Create remote directory /opt/apps/reporting",
      "command": "mkdir -p /opt/apps/reporting"
    },
    ...
  ]
}
```

### Apply

Execute the deployment:

```bash
# Serial deployment (default)
helmforge apply -e staging -a reporting-service --repo git@github.com:org/infra.git

# Deploy up to 3 hosts in parallel
helmforge apply -e staging -a reporting-service --repo git@github.com:org/infra.git --max-parallel 3

# Deploy a specific branch/tag/commit
helmforge apply -e staging -a reporting-service --repo git@github.com:org/infra.git --ref v1.2.3
```

Sample output:
```
Deployment Plan
===============
  App:    reporting-service
  Env:    staging
  ...

Executing deployment...

Release: rel-1708617234567890
  Status:    success
  App:       reporting-service
  Env:       staging
  Commit:    a1b2c3d4
  Started:   2025-02-22 15:00:00 UTC
  Finished:  2025-02-22 15:00:45 UTC
  Duration:  45s

  Host Results:
    HOST        STATUS   ERROR
    10.0.1.10   success
    10.0.1.11   success
```

### Status

Check the current deployment status:

```bash
helmforge status -e staging -a reporting-service

# With drift detection (compares remote marker to current Git HEAD)
helmforge status -e staging -a reporting-service --repo git@github.com:org/infra.git
```

### Drift

Check for configuration drift across all apps in an environment:

```bash
# Check all apps
helmforge drift -e staging --repo git@github.com:org/infra.git --all

# Check specific apps
helmforge drift -e staging --repo git@github.com:org/infra.git reporting-service
```

Sample output:
```
Drift Report for staging (desired: a1b2c3d4)
===========================================
  reporting-service/10.0.1.10  InSync      a1b2c3d4 -> a1b2c3d4
  reporting-service/10.0.1.11  OutOfSync   a1b2c3d4 -> 9f8e7d6c
```

### Rollback

Roll back to a previous release:

```bash
helmforge rollback -e staging -a reporting-service --to rel-1708617234567890
```

### Global Flags

```
-v, --verbose    Enable verbose/debug logging
    --log-json   Use structured JSON logging (for CI/log aggregation)
```

## Architecture

```
cmd/helmforge/main.go           Entry point
internal/
  core/
    model/                      Domain types (AppConfig, Release, Plan, etc.)
    validate/                   YAML config parsing + validation
    plan/                       Plan generation (read-only)
    reconcile/                  Apply engine (deploy, drift check)
    release/                    Output formatting (text, JSON)
  adapters/
    git/                        Git operations (shells out to git CLI)
    remote/                     SSH execution (shells out to ssh/scp)
    health/                     HTTP health checking
    store/                      SQLite release storage
  cli/
    commands/                   Cobra CLI commands
  util/
    log/                        Structured logging (zerolog)
    lock/                       File-based deploy locking
```

All adapters are behind interfaces for testability. Core logic depends only on interfaces, not concrete implementations.

## Safety

- **Ctrl+C handling**: Cancellation stops further host deploys and marks release as cancelled
- **Deploy locking**: File-based lock prevents concurrent deploys for the same env/app
- **Rolling strategy**: Deploy host-by-host, stop on first failure
- **Release tracking**: Every deploy is recorded in local SQLite with per-host results
- **Drift detection**: Remote marker file tracks deployed commit SHA

## Secrets

For v1, helmforge does NOT manage secrets directly. Recommended approaches:
- Pre-provision `.env` files or Docker secrets on the host
- Use an external decrypt command hook
- Use a secrets manager that injects env vars at runtime

## Running Tests

```bash
go test ./...
```

## License

MIT
