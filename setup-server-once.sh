#!/bin/bash
################################################################################
# One-time server setup – run FROM YOUR MAC (not on the server).
# Prepares the server so that after login you only need to run:
#   ~/precast-backend/server-deploy.sh
#
# Usage:
#   ./setup-server-once.sh
#
# What it does:
#   1. SSH to server and clone your Git repo to ~/precast-backend (if needed)
#   2. Copy server-deploy.sh to server and make it executable
#   3. Create .env on server if missing
#   After this, on server you run: ~/precast-backend/server-deploy.sh
################################################################################

set -e

SERVER="${1:-ubuntu@18.140.23.205}"
SSH_KEY="${SSH_KEY:-$HOME/Documents/jaildev.pem}"
GIT_REPO="git@github.com:riteshrai2006/precastezy.git"
GIT_BRANCH="main"
APP_DIR="/home/ubuntu/precast-backend"
LOCAL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "[Setup] Server: $SERVER"
echo "[Setup] This will clone repo on server and install server-deploy.sh so you can run one command after login."
echo ""

# Copy server-deploy.sh to server (so it's there even before clone)
echo "[Setup] Copying server-deploy.sh to server..."
scp -i "$SSH_KEY" -o StrictHostKeyChecking=no "$LOCAL_DIR/server-deploy.sh" "$SERVER:/tmp/server-deploy.sh"

# On server: clone repo if no .git, install script, ensure .env
ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "$SERVER" "
set -e
echo '[Server] Checking app directory...'
if [ ! -d '$APP_DIR' ] || [ ! -d '$APP_DIR/.git' ]; then
    echo '[Server] Cloning repo (first time)...'
    sudo rm -rf '$APP_DIR'
    git clone --branch $GIT_BRANCH '$GIT_REPO' '$APP_DIR' 2>/dev/null || git clone --branch master '$GIT_REPO' '$APP_DIR'
    sudo chown -R ubuntu:ubuntu '$APP_DIR'
else
    echo '[Server] Repo already present at $APP_DIR'
fi
echo '[Server] Installing server-deploy.sh...'
sudo mv /tmp/server-deploy.sh '$APP_DIR/server-deploy.sh'
sudo chown ubuntu:ubuntu '$APP_DIR/server-deploy.sh'
chmod +x '$APP_DIR/server-deploy.sh'
# .env: create if missing (so app can start)
if [ ! -f '$APP_DIR/.env' ]; then
    echo '[Server] Creating .env from template...'
    sudo tee '$APP_DIR/.env' > /dev/null <<'ENVEOF'
DB_USER=bluedev
DB_PASSWORD=B!nT3@2024V
DB_NAME=precast
DB_HOST=18.140.23.205
DB_PORT=5432
FCM_CREDENTIALS_PATH=firebase-credentials.json
ENVEOF
    sudo chown ubuntu:ubuntu '$APP_DIR/.env'
fi
echo '[Server] Done.'
"

echo ""
echo "Setup complete. From now on:"
echo "  1. Login:  ssh -i $SSH_KEY $SERVER"
echo "  2. Run:   ~/precast-backend/server-deploy.sh"
echo ""
echo "That one command will: pull latest code → build → restart backend → reload nginx."
echo ""
