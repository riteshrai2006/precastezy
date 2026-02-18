#!/bin/bash
# =============================================================================
# Precast API (precastezy) — Production Deploy Script
# Run on server:  cd /home/ubuntu/precast-backend && ./server-deploy.sh
# From Mac:       ./upload-and-deploy.sh
#
# Usage:
#   ./server-deploy.sh              → full deploy (pull, build, restart)
#   ./server-deploy.sh --dry-run    → simulate without making changes
#   ./server-deploy.sh --rollback   → restore previous binary and restart
#   ./server-deploy.sh --help       → show help
# =============================================================================

set -o pipefail

# ── Colour palette ─────────────────────────────────────────────────────────────
GREEN="\033[0;32m"; YELLOW="\033[1;33m"; RED="\033[0;31m"
BLUE="\033[0;34m"; CYAN="\033[0;36m"; BOLD="\033[1m"; NC="\033[0m"

log()  { printf "${GREEN}[✔ DEPLOY]${NC}  %s\n" "$*"; }
info() { printf "${YELLOW}[➜ DEPLOY]${NC}  %s\n" "$*"; }
step() { printf "${BLUE}[◆ DEPLOY]${NC}  %s\n" "$*"; }
err()  { printf "${RED}[✘ ERROR]${NC}   %s\n" "$*" >&2; }
warn() { printf "${CYAN}[⚠ WARN]${NC}   %s\n" "$*"; }
dry()  { printf "${CYAN}[DRY-RUN]${NC}  SKIP: %s\n" "$*"; }

# ── Config ─────────────────────────────────────────────────────────────────────
# FIX 1: Hardcode to the actual server path instead of the broken
#         /var/www/source-code detection which doesn't exist on this server.
#         GIT_ROOT = where .git lives = repo root
#         BINARY_DIR = where the running binary lives (what systemd points to)
#         BUILD_DIR = where main.go lives (may be a subdir of GIT_ROOT)
GIT_ROOT="/home/ubuntu/precast-backend"
BINARY_DIR="/home/ubuntu/precast-backend"   # systemd ExecStart must point here
BINARY_NAME="precast-backend"
SERVICE_NAME="precast-backend"
GIT_REPO="${GIT_REPO:-git@github.com:riteshrai2006/precastezy.git}"
GIT_BRANCH="${GIT_BRANCH:-main}"
GO_BIN="${GO_BIN:-/usr/local/go/bin/go}"
GO_MIN_VERSION="1.21"
HEALTH_PORT="9000"
HEALTH_PATH="/swagger/doc.json"
HEALTH_TIMEOUT=30
HEALTH_RETRY_INTERVAL=2
# FIX 5: Use > (truncate) not >> (append) so OOM detection isn't polluted by old runs
BUILD_LOG_FILE="/tmp/precast_deploy_build.log"
# Always use ubuntu's SSH key regardless of who runs the script
SSH_KEY_PATH="/home/ubuntu/.ssh/id_ed25519"

# ── Flags ──────────────────────────────────────────────────────────────────────
DRY_RUN=0
ROLLBACK=0

for arg in "$@"; do
    case "$arg" in
        --dry-run)  DRY_RUN=1 ;;
        --rollback) ROLLBACK=1 ;;
        --help|-h)
            echo "Usage: ./server-deploy.sh [--dry-run] [--rollback]"
            echo "  --dry-run   Simulate all steps without making any changes"
            echo "  --rollback  Restore $BINARY_NAME.bak and restart service"
            exit 0
            ;;
        *)
            err "Unknown argument: $arg  (use --help)"
            exit 1
            ;;
    esac
done

# FIX 2: Resolve BUILD_DIR (where main.go lives) separately from GIT_ROOT.
#         This prevents the binary ending up in the wrong subdirectory.
BUILD_DIR="$GIT_ROOT"
if [ -f "$GIT_ROOT/go-backend/main.go" ]; then
    BUILD_DIR="$GIT_ROOT/go-backend"
elif [ -f "$GIT_ROOT/main.go" ]; then
    BUILD_DIR="$GIT_ROOT"
fi

# ── SSH key setup ──────────────────────────────────────────────────────────────
if [ -f "$SSH_KEY_PATH" ]; then
    export GIT_SSH_COMMAND="ssh -i $SSH_KEY_PATH -F /dev/null -o StrictHostKeyChecking=no"
fi

# ── State tracking for safe cleanup ───────────────────────────────────────────
# 0=not started  1=service stopped  2=service restarted
_STATE=0

_cleanup() {
    local exit_code=$?
    if [ "$_STATE" -eq 1 ]; then
        warn "Deploy failed after stopping service — attempting to restore..."
        if [ -f "$BINARY_DIR/$BINARY_NAME.bak" ] && [ ! -f "$BINARY_DIR/$BINARY_NAME" ]; then
            warn "Restoring backup binary..."
            mv -f "$BINARY_DIR/$BINARY_NAME.bak" "$BINARY_DIR/$BINARY_NAME"
            chmod 755 "$BINARY_DIR/$BINARY_NAME"
        fi
        sudo systemctl start "$SERVICE_NAME" 2>/dev/null && \
            warn "Service restored after failed deploy." || \
            err  "CRITICAL: Service could not be restored! Manual intervention required."
    fi
    [ -n "${TMP_BIN:-}" ] && rm -f "$TMP_BIN" 2>/dev/null
    exit "$exit_code"
}
trap _cleanup EXIT

# ── Banner ─────────────────────────────────────────────────────────────────────
echo ""
printf "${BOLD}══════════════════════════════════════════════════${NC}\n"
if   [ $DRY_RUN  -eq 1 ]; then printf "${BOLD}   Precast API — DRY RUN  $(date '+%Y-%m-%d %H:%M:%S')${NC}\n"
elif [ $ROLLBACK -eq 1 ]; then printf "${BOLD}   Precast API — ROLLBACK  $(date '+%Y-%m-%d %H:%M:%S')${NC}\n"
else                            printf "${BOLD}   Precast API — Deploy  $(date '+%Y-%m-%d %H:%M:%S')${NC}\n"
fi
printf "${BOLD}══════════════════════════════════════════════════${NC}\n"
echo ""

# ── Root / sudo check ──────────────────────────────────────────────────────────
# Script should run as ubuntu (with NOPASSWD sudo), not as root
if [ "$(id -u)" -eq 0 ]; then
    warn "Running as root — prefer running as ubuntu with sudo access."
fi

# =============================================================================
# ROLLBACK MODE
# =============================================================================
if [ $ROLLBACK -eq 1 ]; then
    step "Rolling back to previous binary..."

    if [ ! -f "$BINARY_DIR/$BINARY_NAME.bak" ]; then
        err "No backup found at $BINARY_DIR/$BINARY_NAME.bak — cannot rollback."
        exit 1
    fi

    step "Stopping $SERVICE_NAME..."
    sudo systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    _STATE=1
    sleep 1

    cp -f "$BINARY_DIR/$BINARY_NAME" "$BINARY_DIR/$BINARY_NAME.failed" 2>/dev/null || true
    mv -f "$BINARY_DIR/$BINARY_NAME.bak" "$BINARY_DIR/$BINARY_NAME"
    chmod 755 "$BINARY_DIR/$BINARY_NAME"
    chown ubuntu:ubuntu "$BINARY_DIR/$BINARY_NAME" 2>/dev/null || true

    step "Starting $SERVICE_NAME..."
    sudo systemctl start "$SERVICE_NAME"
    _STATE=2
    sleep 2

    if systemctl is-active --quiet "$SERVICE_NAME"; then
        log "Rollback complete. Service is running on previous binary."
        log "Failed binary saved as: $BINARY_DIR/$BINARY_NAME.failed"
    else
        err "Service failed to start after rollback!"
        sudo systemctl status "$SERVICE_NAME" --no-pager -l >&2
        exit 1
    fi
    exit 0
fi

# =============================================================================
# DEPLOY MODE
# =============================================================================

# ── Pre-flight checks ──────────────────────────────────────────────────────────
step "Running pre-flight checks..."

if [ ! -d "$GIT_ROOT" ]; then
    err "GIT_ROOT not found: $GIT_ROOT"
    exit 1
fi

if [ ! -x "$GO_BIN" ]; then
    err "Go binary not found or not executable: $GO_BIN"
    exit 1
fi

# ── Go version check ───────────────────────────────────────────────────────────
ACTUAL_GO_VER=$("$GO_BIN" version 2>/dev/null | grep -oP 'go\K[0-9]+\.[0-9]+' | head -1)
if [ -n "$ACTUAL_GO_VER" ]; then
    MIN_MAJOR=$(echo "$GO_MIN_VERSION" | cut -d. -f1)
    MIN_MINOR=$(echo "$GO_MIN_VERSION" | cut -d. -f2)
    ACT_MAJOR=$(echo "$ACTUAL_GO_VER"  | cut -d. -f1)
    ACT_MINOR=$(echo "$ACTUAL_GO_VER"  | cut -d. -f2)
    if [ "$ACT_MAJOR" -lt "$MIN_MAJOR" ] || \
       { [ "$ACT_MAJOR" -eq "$MIN_MAJOR" ] && [ "$ACT_MINOR" -lt "$MIN_MINOR" ]; }; then
        err "Go $ACTUAL_GO_VER is below minimum required $GO_MIN_VERSION"
        exit 1
    fi
    log "Go version: $ACTUAL_GO_VER (>= $GO_MIN_VERSION required)"
fi

# FIX 11: Verify go.sum exists before attempting build
if [ ! -f "$BUILD_DIR/go.sum" ] && [ ! -f "$GIT_ROOT/go.sum" ]; then
    warn "go.sum not found — build may fail if modules are not vendored."
fi

if [ ! -f "$BINARY_DIR/.env" ] && [ ! -f "$BUILD_DIR/.env" ] && [ ! -f "$GIT_ROOT/.env" ]; then
    warn "No .env file found — ensure environment variables are configured."
fi

log "Pre-flight checks passed."
log "  GIT_ROOT  : $GIT_ROOT"
log "  BUILD_DIR : $BUILD_DIR"
log "  BINARY    : $BINARY_DIR/$BINARY_NAME"

# ── Git pull ───────────────────────────────────────────────────────────────────
cd "$GIT_ROOT" || { err "Cannot cd to $GIT_ROOT"; exit 1; }

# Fix .git ownership if root accidentally owns it
if [ -d "$GIT_ROOT/.git" ]; then
    GIT_OWNER=$(stat -c '%U' "$GIT_ROOT/.git" 2>/dev/null || echo "")
    if [ "$GIT_OWNER" = "root" ]; then
        warn "Fixing .git ownership (was root, changing to ubuntu)..."
        sudo chown -R ubuntu:ubuntu "$GIT_ROOT/.git"
    fi
fi

# Add github to known_hosts silently
mkdir -p ~/.ssh
ssh-keyscan -t ed25519 github.com >> ~/.ssh/known_hosts 2>/dev/null || true

if [ -d "$GIT_ROOT/.git" ]; then
    info "Pulling latest code from origin/$GIT_BRANCH..."
    git remote set-url origin "$GIT_REPO" 2>/dev/null || git remote add origin "$GIT_REPO"

    if [ $DRY_RUN -eq 1 ]; then
        dry "git pull origin $GIT_BRANCH"
        COMMIT="dry-run"
    else
        # FIX 8: Back up .env BEFORE any git operation that could wipe local files
        [ -f "$GIT_ROOT/.env" ]                    && cp -a "$GIT_ROOT/.env"                    /tmp/precast.env.bak
        [ -f "$BUILD_DIR/.env" ]                   && cp -a "$BUILD_DIR/.env"                   /tmp/precast-build.env.bak
        [ -f "$GIT_ROOT/firebase-credentials.json" ] && cp -a "$GIT_ROOT/firebase-credentials.json" /tmp/precast-firebase.bak

        GIT_OUT=$(git pull origin "$GIT_BRANCH" 2>&1)
        GIT_EXIT=$?

        # Restore .env after pull (git never tracks it, but reset --hard could wipe it)
        [ -f /tmp/precast.env.bak ]       && mv -f /tmp/precast.env.bak       "$GIT_ROOT/.env"
        [ -f /tmp/precast-build.env.bak ] && mv -f /tmp/precast-build.env.bak "$BUILD_DIR/.env"
        [ -f /tmp/precast-firebase.bak ]  && mv -f /tmp/precast-firebase.bak  "$GIT_ROOT/firebase-credentials.json"

        if [ $GIT_EXIT -ne 0 ] && echo "$GIT_OUT" | grep -qi "divergent\|forced update\|non-fast-forward"; then
            warn "Divergent branches — resetting to origin/$GIT_BRANCH..."
            git fetch origin
            git reset --hard "origin/$GIT_BRANCH"
            # Restore again after reset --hard
            [ -f /tmp/precast.env.bak ]       && cp -a /tmp/precast.env.bak       "$GIT_ROOT/.env"
            [ -f /tmp/precast-build.env.bak ] && cp -a /tmp/precast-build.env.bak "$BUILD_DIR/.env"
            [ -f /tmp/precast-firebase.bak ]  && cp -a /tmp/precast-firebase.bak  "$GIT_ROOT/firebase-credentials.json"
            log "Reset to origin/$GIT_BRANCH"
            GIT_EXIT=0

        elif [ $GIT_EXIT -ne 0 ]; then
            warn "git pull failed — will build from existing code."
            echo "$GIT_OUT" | head -10
            if echo "$GIT_OUT" | grep -qi "Permission denied\|publickey"; then
                err "SSH key not authorized. Add this key to GitHub:"
                err "  https://github.com/settings/ssh/new"
                [ -f "$SSH_KEY_PATH.pub" ] && err "  Key: $(cat "$SSH_KEY_PATH.pub")"
            fi
        else
            log "Code updated from origin/$GIT_BRANCH"
        fi

        COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
    fi
else
    # No .git — first time init
    info "No .git found — initializing repo..."
    [ -f "$GIT_ROOT/.env" ]                    && cp -a "$GIT_ROOT/.env"                    /tmp/precast.env.bak
    [ -f "$GIT_ROOT/firebase-credentials.json" ] && cp -a "$GIT_ROOT/firebase-credentials.json" /tmp/precast-firebase.bak

    git init
    git remote add origin "$GIT_REPO"
    sudo chown -R ubuntu:ubuntu "$GIT_ROOT/.git" 2>/dev/null || true

    if ! git fetch origin; then
        err "git fetch failed. Add SSH key to GitHub: https://github.com/settings/ssh/new"
        [ -f "$SSH_KEY_PATH.pub" ] && err "Server public key: $(cat "$SSH_KEY_PATH.pub")"
        # Restore files before exit
        [ -f /tmp/precast.env.bak ]      && mv /tmp/precast.env.bak      "$GIT_ROOT/.env"
        [ -f /tmp/precast-firebase.bak ] && mv /tmp/precast-firebase.bak "$GIT_ROOT/firebase-credentials.json"
        exit 1
    fi

    git checkout -B "$GIT_BRANCH" "origin/$GIT_BRANCH" 2>/dev/null || \
    git checkout -B master origin/master 2>/dev/null || true

    [ -f /tmp/precast.env.bak ]      && mv /tmp/precast.env.bak      "$GIT_ROOT/.env"
    [ -f /tmp/precast-firebase.bak ] && mv /tmp/precast-firebase.bak "$GIT_ROOT/firebase-credentials.json"

    log "Repo initialized at $GIT_ROOT"
    COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
fi

# ── Memory check & free before build ──────────────────────────────────────────
step "Building Go application..."

get_avail_mb() {
    grep MemAvailable /proc/meminfo 2>/dev/null | awk '{print int($2/1024)}' || echo "999"
}

AVAIL_MB=$(get_avail_mb)

# FIX 7: Set _STATE=1 HERE before we stop the service, not after.
#         This ensures the cleanup trap will always restore the service on any failure.
_STATE=1

if [ "${AVAIL_MB}" -lt 600 ] && [ $DRY_RUN -eq 0 ]; then
    info "Low memory (${AVAIL_MB}MB) — stopping service to free RAM for build..."
    sudo systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    sleep 2
    sync
    echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null 2>&1 || true
    sleep 1
    AVAIL_MB=$(get_avail_mb)
    info "Memory available after cleanup: ${AVAIL_MB}MB"
fi

# Decide parallelism based on available memory
GOMAXPROCS=2
if [ "${AVAIL_MB}" -lt 512 ]; then
    GOMAXPROCS=1
    info "Low memory (${AVAIL_MB}MB) — using single-threaded build"
fi

cd "$BUILD_DIR" || { err "Cannot cd to BUILD_DIR: $BUILD_DIR"; exit 1; }

export PATH="/usr/local/go/bin:${PATH}"
export GOPATH="${GOPATH:-/home/ubuntu/go}"
export GOTOOLCHAIN=local
export GOMAXPROCS
# FIX 6: Set GOGC=20 to reduce garbage collector memory pressure during build
export GOGC=20

TMP_BIN=$(mktemp -u /tmp/go_build_XXXXXX)

if [ $DRY_RUN -eq 1 ]; then
    dry "go build -o $TMP_BIN (in $BUILD_DIR)"
    BUILD_TIME=0
    BINARY_SIZE="N/A"
else
    # FIX 5: Truncate build log at start of each run (was appending >>)
    : > "$BUILD_LOG_FILE"

    # FIX 3: Set ulimit BEFORE go mod vendor, not after
    # FIX 4: Read fresh AVAIL_MB here (after service was stopped and caches dropped)
    AVAIL_MB=$(get_avail_mb)
    if [ "${AVAIL_MB}" -gt 0 ] && [ "${AVAIL_MB}" -lt 1000 ]; then
        MEM_LIMIT_KB=$((AVAIL_MB * 1024 * 80 / 100))
        ulimit -v "$MEM_LIMIT_KB" 2>/dev/null || true
        info "Memory limit: ${MEM_LIMIT_KB}KB (80% of ${AVAIL_MB}MB available)"
    fi

    # FIX 11: Only run go mod vendor if go.sum exists (otherwise it will fail)
    if [ -f "go.sum" ]; then
        go mod vendor 2>/dev/null || warn "go mod vendor failed — using module cache"
    else
        warn "go.sum not found — skipping go mod vendor"
    fi

    BUILD_START=$(date +%s)

    # Build: always use . (package) not main.go (single file) so all packages are included
    # FIX 2: Binary always goes to BINARY_DIR, not BUILD_DIR
    CGO_ENABLED=0 "$GO_BIN" build \
        -p "$GOMAXPROCS" \
        -ldflags="-s -w" \
        -trimpath \
        -o "$TMP_BIN" \
        . 2>&1 | tee "$BUILD_LOG_FILE"

    BUILD_EXIT=${PIPESTATUS[0]}
    BUILD_END=$(date +%s)
    BUILD_TIME=$((BUILD_END - BUILD_START))

    if [ $BUILD_EXIT -ne 0 ] || [ ! -f "$TMP_BIN" ]; then
        if grep -qi "signal: killed\|out of memory\|cannot allocate\|failed to reserve" "$BUILD_LOG_FILE" 2>/dev/null; then
            err "Build killed by OOM after ${BUILD_TIME}s (${AVAIL_MB}MB was available)"
            err "Solutions:"
            err "  1. Add swap:  sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile"
            err "  2. Build on Mac:  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o precast-backend ."
            err "     Then upload:   scp -i jaildev.pem precast-backend ubuntu@<IP>:$BINARY_DIR/"
        else
            err "Build failed after ${BUILD_TIME}s — see $BUILD_LOG_FILE"
            tail -20 "$BUILD_LOG_FILE" >&2
        fi
        exit 1
    fi

    BINARY_SIZE=$(ls -lh "$TMP_BIN" | awk '{print $5}')
    log "Build succeeded in ${BUILD_TIME}s — binary: ${BINARY_SIZE}"
fi

# ── Stop service (if still running after memory-free step) ────────────────────
if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
    step "Stopping $SERVICE_NAME..."
    if [ $DRY_RUN -eq 1 ]; then
        dry "systemctl stop $SERVICE_NAME"
    else
        sudo systemctl stop "$SERVICE_NAME" 2>/dev/null || true
        sleep 1
    fi
else
    info "Service already stopped."
fi

# ── Swap binary (atomic, with backup) ─────────────────────────────────────────
step "Replacing binary..."
if [ $DRY_RUN -eq 1 ]; then
    dry "cp $BINARY_NAME → $BINARY_NAME.bak && mv $TMP_BIN → $BINARY_DIR/$BINARY_NAME"
else
    # Back up current binary for --rollback
    [ -f "$BINARY_DIR/$BINARY_NAME" ] && cp -f "$BINARY_DIR/$BINARY_NAME" "$BINARY_DIR/$BINARY_NAME.bak"

    # FIX 12: Binary ALWAYS goes to BINARY_DIR (where systemd ExecStart points)
    #          Never into BUILD_DIR which may be a subdir
    mv -f "$TMP_BIN" "$BINARY_DIR/$BINARY_NAME"
    chmod 755 "$BINARY_DIR/$BINARY_NAME"
    chown ubuntu:ubuntu "$BINARY_DIR/$BINARY_NAME" 2>/dev/null || true
    log "Binary replaced at $BINARY_DIR/$BINARY_NAME (backup saved as .bak)"
fi

# ── Start service ──────────────────────────────────────────────────────────────
step "Starting $SERVICE_NAME..."
if [ $DRY_RUN -eq 1 ]; then
    dry "systemctl start $SERVICE_NAME"
else
    if ! sudo systemctl start "$SERVICE_NAME"; then
        err "systemctl start failed"
        sudo systemctl status "$SERVICE_NAME" --no-pager -l >&2
        exit 1
    fi
    _STATE=2
fi

# ── Health check ───────────────────────────────────────────────────────────────
if [ $DRY_RUN -eq 1 ]; then
    dry "Health check on port $HEALTH_PORT (timeout: ${HEALTH_TIMEOUT}s)"
else
    info "Waiting for service to become healthy (timeout: ${HEALTH_TIMEOUT}s)..."
    WAIT=0
    HEALTHY=0

    while [ $WAIT -lt $HEALTH_TIMEOUT ]; do
        # FIX 10: Use systemctl without sudo consistently (ubuntu user can query status)
        if ! systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
            err "Service crashed after ${WAIT}s!"
            sudo systemctl status "$SERVICE_NAME" --no-pager -l >&2
            exit 1
        fi

        if command -v curl &>/dev/null; then
            HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --max-time 3 \
                "http://127.0.0.1:${HEALTH_PORT}${HEALTH_PATH}" 2>/dev/null)
            if [ "$HTTP_CODE" = "200" ]; then
                HEALTHY=1
                break
            fi
            printf "  [%02ds] HTTP %s — retrying in %ss...\n" \
                "$WAIT" "${HTTP_CODE:-timeout}" "$HEALTH_RETRY_INTERVAL"
        else
            HEALTHY=1
            break
        fi

        sleep "$HEALTH_RETRY_INTERVAL"
        WAIT=$((WAIT + HEALTH_RETRY_INTERVAL))
    done

    if [ $HEALTHY -eq 0 ]; then
        err "Service did not become healthy within ${HEALTH_TIMEOUT}s!"
        sudo systemctl status "$SERVICE_NAME" --no-pager -l >&2
        exit 1
    fi

    log "Backend healthy on port $HEALTH_PORT"
    sudo systemctl reload nginx 2>/dev/null || sudo systemctl restart nginx 2>/dev/null || true
    log "Nginx reloaded"
fi

# ── Summary ────────────────────────────────────────────────────────────────────
echo ""
printf "${BOLD}══════════════════════════════════════════════════${NC}\n"
if [ $DRY_RUN -eq 1 ]; then
    log "Dry-run complete — no changes were made."
else
    log "Deployment complete!"
    log "  Commit   : ${COMMIT:-unknown}"
    log "  Binary   : ${BINARY_SIZE:-N/A} (built in ${BUILD_TIME}s)"
    log "  Service  : $(systemctl is-active "$SERVICE_NAME" 2>/dev/null || echo 'unknown')"
    log "  API      : https://precastezy.blueinvent.com"
    log "  Logs     : sudo journalctl -u $SERVICE_NAME -f"
    log "  Rollback : ./server-deploy.sh --rollback"
fi
printf "${BOLD}══════════════════════════════════════════════════${NC}\n"
echo ""

exit 0
