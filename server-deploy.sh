#!/bin/bash
################################################################################
# Server-side deploy script – run this ON THE SERVER after SSH login.
# One command does: pull latest code, build, restart backend, reload nginx.
#
# Usage (on server):
#   ~/precast-backend/server-deploy.sh
#   or:  cd ~/precast-backend && ./server-deploy.sh
#
# First-time setup: see SETUP_SERVER.md or run the one-time setup from your Mac.
################################################################################

set -e

APP_DIR="${APP_DIR:-/home/ubuntu/precast-backend}"
APP_NAME="precast-backend"
PORT=9000
GIT_REPO="${GIT_REPO:-git@github.com:riteshrai2006/precastezy.git}"
GIT_BRANCH="${GIT_BRANCH:-main}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${BLUE}[INFO]${NC} $1"; }
log_ok()    { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_err()   { echo -e "${RED}[ERR]${NC} $1"; }

# If backend lives in a subdir (e.g. precastezy/go-backend), use it
if [ -f "$APP_DIR/go-backend/main.go" ]; then
    APP_DIR="$APP_DIR/go-backend"
fi
cd "$APP_DIR" || { log_err "Directory $APP_DIR not found"; exit 1; }

log_info "Pull → Build → Restart (in $APP_DIR)"

# 1. Git pull (if this is a git repo)
if [ -d .git ]; then
    log_info "Pulling latest from $GIT_REPO ($GIT_BRANCH)..."
    git remote add origin "$GIT_REPO" 2>/dev/null || true
    git fetch origin 2>/dev/null || true
    if git pull origin "$GIT_BRANCH" --no-edit 2>/dev/null; then
        log_ok "Pulled latest code"
    else
        git pull origin master --no-edit 2>/dev/null && log_ok "Pulled (master)" || log_warn "Git pull failed, using existing code"
    fi
else
    log_warn "No .git – skipping pull (run one-time setup to clone repo)"
fi

# 2. Build
log_info "Building..."
export PATH="$PATH:/usr/local/go/bin"
export GOPATH="${GOPATH:-/home/ubuntu/go}"
if ! go build -o precast-backend -ldflags='-s -w' . 2>&1; then
    log_err "Build failed"
    exit 1
fi
chmod +x precast-backend
# If we built in a subdir (e.g. go-backend), copy binary where systemd expects it
if [ "$(pwd)" != "/home/ubuntu/precast-backend" ] && [ -d /home/ubuntu/precast-backend ]; then
    cp -f precast-backend /home/ubuntu/precast-backend/precast-backend
    chmod +x /home/ubuntu/precast-backend/precast-backend
fi
log_ok "Build done"

# 3. Restart backend
log_info "Restarting $APP_NAME..."
sudo systemctl restart "$APP_NAME"
sleep 5
if sudo systemctl is-active --quiet "$APP_NAME"; then
    log_ok "Service is running"
else
    log_err "Service failed to start"
    sudo journalctl -u "$APP_NAME" -n 15 --no-pager
    exit 1
fi

# 4. Wait for HTTP then reload nginx
log_info "Waiting for backend to respond..."
for i in $(seq 1 20); do
    if curl -s -o /dev/null -w '%{http_code}' --connect-timeout 2 http://localhost:$PORT/swagger/doc.json 2>/dev/null | grep -q 200; then
        log_ok "Backend responding on port $PORT"
        sudo systemctl reload nginx 2>/dev/null || sudo systemctl restart nginx 2>/dev/null || true
        log_ok "Nginx reloaded"
        break
    fi
    sleep 1
done

echo ""
log_ok "Deploy done. API: https://precastezy.blueinvent.com"
log_info "Logs: sudo journalctl -u $APP_NAME -f"
