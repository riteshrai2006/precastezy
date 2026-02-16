#!/bin/bash

################################################################################
# Comprehensive Deployment Script for Precast Backend
# 
# PURPOSE:
#   This script performs a complete deployment of the Go backend application
#   to the remote server. It includes pre-flight checks, server verification,
#   code deployment, build, service management, and health verification.
#
# USAGE:
#   ./deploy.sh
#
# WHAT IT DOES:
#   1. Pre-flight checks (local): Verifies required tools, SSH key, project structure
#   2. Server connectivity: Tests SSH connection to remote server
#   3. Server prerequisites: Checks Go, PostgreSQL, port availability
#   4. Full deployment: Uploads code, builds application, creates systemd service
#   5. Post-deployment verification: Health checks with automatic fixes
#
# FEATURES:
#   - Automatic service restart if backend is down
#   - Port conflict detection and resolution
#   - Health check with automatic recovery
#   - Comprehensive error reporting
#   - Backup of existing deployment
#   - Swagger documentation verification
#
# ALWAYS performs FULL deployment (complete rebuild)
#
# DEPLOY ORDER (when updating): Update code on server first (sync + build + start),
# then update this script file; the script is part of the codebase and is synced
# with the rest of the project.
################################################################################

# Strict error handling: exit on error, undefined vars, pipe failures
set -euo pipefail

################################################################################
# Color Codes for Terminal Output
################################################################################
RED='\033[0;31m'      # Error messages
GREEN='\033[0;32m'   # Success messages
YELLOW='\033[1;33m'  # Warning messages
BLUE='\033[0;34m'    # Info messages
NC='\033[0m'         # No Color (reset)

################################################################################
# Configuration Variables
################################################################################
DEFAULT_SERVER="ubuntu@18.140.23.205"           # Remote server SSH address
DEFAULT_SSH_KEY="$HOME/Documents/jaildev.pem"   # SSH private key path
APP_NAME="precast-backend"                      # Systemd service name
APP_DIR="/home/ubuntu/$APP_NAME"                # Remote application directory
LOCAL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"  # Local project directory
PORT=9000                                        # Backend application port

# Error tracking counters
ERRORS=0      # Total number of errors encountered
WARNINGS=0   # Total number of warnings encountered

################################################################################
# Helper Functions
# These functions provide consistent logging and command checking
################################################################################

# Log an informational message (blue)
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

# Log a success message (green)
log_success() {
    echo -e "${GREEN}[✓]${NC} $1"
}

# Log a warning message (yellow) and increment warning counter
log_warning() {
    echo -e "${YELLOW}[⚠]${NC} $1"
    ((WARNINGS++)) || true
}

# Log an error message (red) and increment error counter
log_error() {
    echo -e "${RED}[✗]${NC} $1"
    ((ERRORS++)) || true
}

# Check if a command exists in PATH
# Returns 0 if found, 1 if not found
check_command() {
    if ! command -v "$1" &> /dev/null; then
        log_error "$1 is not installed or not in PATH"
        return 1
    fi
    return 0
}

################################################################################
# Step 1: Pre-flight Checks (Local Environment)
# 
# PURPOSE:
#   Verify that the local environment has everything needed for deployment
#   before attempting to connect to the remote server.
#
# CHECKS PERFORMED:
#   - Required commands (ssh, rsync, scp, curl)
#   - SSH key existence and permissions
#   - Local project directory structure
#   - Required Go files (main.go, go.mod, go.sum)
#   - Swagger documentation files
################################################################################

echo -e "\n${GREEN}========================================${NC}"
echo -e "${GREEN}Precast Backend Deployment Script${NC}"
echo -e "${GREEN}========================================${NC}\n"

log_info "Step 1: Pre-flight checks (local environment)..."

# Check if required commands are available in PATH
# These commands are essential for deployment operations
log_info "  Checking required commands..."
for cmd in ssh rsync scp curl; do
    if check_command "$cmd"; then
        log_success "  $cmd found"
    else
        log_error "  $cmd not found - please install it"
        exit 1
    fi
done

# Locate and verify SSH private key
# SSH key is required for secure connection to remote server
log_info "  Checking SSH key..."
SSH_KEY=""
if [ -f "$DEFAULT_SSH_KEY" ]; then
    SSH_KEY="$DEFAULT_SSH_KEY"
    log_success "  SSH key found: $SSH_KEY"
elif [ -f "$HOME/.ssh/id_rsa" ]; then
    SSH_KEY="$HOME/.ssh/id_rsa"
    log_success "  Using default SSH key: $SSH_KEY"
else
    log_error "  SSH key not found at $DEFAULT_SSH_KEY or ~/.ssh/id_rsa"
    exit 1
fi

# Verify SSH key has correct permissions (600 or 400)
# SSH requires strict permissions on private keys for security
if [ -f "$SSH_KEY" ]; then
    PERMS=$(stat -f "%OLp" "$SSH_KEY" 2>/dev/null || stat -c "%a" "$SSH_KEY" 2>/dev/null || echo "000")
    if [ "$PERMS" != "600" ] && [ "$PERMS" != "400" ]; then
        log_warning "  SSH key permissions are $PERMS (should be 600 or 400)"
        log_info "  Fixing permissions..."
        chmod 600 "$SSH_KEY"
        log_success "  Permissions fixed"
    fi
fi

# Verify all required project directories exist locally
# These directories contain the application code that will be deployed
log_info "  Checking local project structure..."
REQUIRED_DIRS=("handlers" "models" "services" "storage" "utils" "repository" "docs")
MISSING_DIRS=()
for dir in "${REQUIRED_DIRS[@]}"; do
    if [ ! -d "$LOCAL_DIR/$dir" ]; then
        MISSING_DIRS+=("$dir")
    fi
done

if [ ${#MISSING_DIRS[@]} -gt 0 ]; then
    log_error "  Missing required directories: ${MISSING_DIRS[*]}"
    exit 1
fi
log_success "  All required directories found"

# Verify required Go files exist
# main.go is the entry point, go.mod/go.sum define dependencies
log_info "  Checking required files..."
REQUIRED_FILES=("main.go" "go.mod" "go.sum")
MISSING_FILES=()
for file in "${REQUIRED_FILES[@]}"; do
    if [ ! -f "$LOCAL_DIR/$file" ]; then
        MISSING_FILES+=("$file")
    fi
done

if [ ${#MISSING_FILES[@]} -gt 0 ]; then
    log_error "  Missing required files: ${MISSING_FILES[*]}"
    exit 1
fi
log_success "  All required files found"

# Check if Swagger documentation has been generated
# Swagger docs are needed for API documentation endpoint
log_info "  Checking Swagger documentation..."
if [ ! -d "$LOCAL_DIR/docs" ] || [ ! -f "$LOCAL_DIR/docs/docs.go" ]; then
    log_warning "  Swagger docs not found. Run 'swag init -g main.go' first"
else
    log_success "  Swagger docs found"
fi

log_success "  Deployment mode: FULL (complete rebuild)"

################################################################################
# Step 2: Server Connectivity Check
# 
# PURPOSE:
#   Verify that we can establish an SSH connection to the remote server
#   before proceeding with deployment operations.
#
# CHECKS PERFORMED:
#   - SSH connection test with 10 second timeout
#   - Server reachability
################################################################################

log_info "\nStep 2: Server connectivity check..."

SERVER="$DEFAULT_SERVER"
SERVER_IP=$(echo "$SERVER" | cut -d'@' -f2)  # Extract IP from user@host format

# Test SSH connection with timeout
# If connection fails, deployment cannot proceed
log_info "  Testing SSH connection to $SERVER..."
if ssh -i "$SSH_KEY" -o ConnectTimeout=10 -o StrictHostKeyChecking=no "$SERVER" "echo 'Connection successful'" &>/dev/null; then
    log_success "  SSH connection successful"
else
    log_error "  Cannot connect to server. Check:"
    log_error "    - Server is reachable: $SERVER_IP"
    log_error "    - SSH key is correct: $SSH_KEY"
    log_error "    - Network connectivity"
    exit 1
fi

################################################################################
# Step 3: Server Prerequisites Check
# 
# PURPOSE:
#   Verify that the remote server has all necessary prerequisites installed
#   and configured before deploying the application.
#
# CHECKS PERFORMED:
#   - Go programming language installation
#   - PostgreSQL database service status
#   - Port 9000 availability (backend port)
#   - Application directory existence
################################################################################

log_info "\nStep 3: Server prerequisites check..."

# Check if Go is installed on the server
# Go is required to build the application. If not found, will install during deployment
log_info "  Checking Go installation..."
GO_VERSION=$(ssh -i "$SSH_KEY" "$SERVER" "command -v go >/dev/null 2>&1 && go version || echo 'NOT_INSTALLED'" 2>/dev/null || echo "NOT_INSTALLED")
if [[ "$GO_VERSION" == *"NOT_INSTALLED"* ]]; then
    log_warning "  Go is not installed on server - will install during deployment"
else
    log_success "  $GO_VERSION"
fi

# Check PostgreSQL database service status
# PostgreSQL is required for the application to run. Will attempt to start if stopped
log_info "  Checking PostgreSQL..."
PG_STATUS=$(ssh -i "$SSH_KEY" "$SERVER" "
    # Check for PostgreSQL 14 (common version) or generic postgresql service
    if sudo systemctl is-active --quiet postgresql@14-main 2>/dev/null; then
        echo 'RUNNING'
    elif sudo systemctl is-active --quiet postgresql 2>/dev/null; then
        echo 'RUNNING'
    else
        echo 'STOPPED'
    fi
" 2>/dev/null || echo "UNKNOWN")

if [ "$PG_STATUS" == "RUNNING" ]; then
    log_success "  PostgreSQL is running"
elif [ "$PG_STATUS" == "STOPPED" ]; then
    log_warning "  PostgreSQL is stopped - will attempt to start"
else
    log_warning "  Could not determine PostgreSQL status"
fi

# Check if port 9000 is available or in use
# If port is in use, will free it before starting the service
log_info "  Checking port $PORT availability..."
PORT_IN_USE=$(ssh -i "$SSH_KEY" "$SERVER" "
    # Try lsof first, then fuser as fallback
    if command -v lsof &>/dev/null; then
        sudo lsof -ti:$PORT 2>/dev/null || echo 'FREE'
    elif command -v fuser &>/dev/null; then
        sudo fuser $PORT/tcp 2>/dev/null && echo 'IN_USE' || echo 'FREE'
    else
        echo 'UNKNOWN'
    fi
" 2>/dev/null || echo "UNKNOWN")

if [ "$PORT_IN_USE" == "FREE" ]; then
    log_success "  Port $PORT is available"
elif [ "$PORT_IN_USE" == "IN_USE" ]; then
    log_warning "  Port $PORT is in use - will free it before deployment"
else
    log_warning "  Could not check port status (will attempt cleanup)"
fi

# Check if application directory exists on server
# If exists, will create backup before deployment
log_info "  Checking application directory..."
APP_DIR_EXISTS=$(ssh -i "$SSH_KEY" "$SERVER" "[ -d '$APP_DIR' ] && echo 'EXISTS' || echo 'NOT_EXISTS'" 2>/dev/null || echo "UNKNOWN")
if [ "$APP_DIR_EXISTS" == "EXISTS" ]; then
    log_success "  Application directory exists: $APP_DIR (will be backed up)"
else
    log_info "  Application directory will be created"
fi

################################################################################
# Step 4: Full Deployment Execution
# 
# PURPOSE:
#   Perform the actual deployment: stop old service, backup, upload code,
#   install dependencies, build application, create systemd service, and start.
#
# PROCESS:
#   1. Ensure PostgreSQL is running (required for app startup)
#   2. Stop existing service and free port 9000
#   3. Backup existing deployment directory
#   4. Upload all project files via rsync
#   5. Install/verify Go installation
#   6. Build the Go application
#   7. Create/update systemd service file
#   8. Start the service
#   9. Verify service is running and healthy
################################################################################

log_info "\nStep 4: Full deployment (complete rebuild)..."

# Ensure PostgreSQL database is running before deployment
# Application requires database connection on startup
log_info "  Ensuring PostgreSQL is running..."
ssh -i "$SSH_KEY" "$SERVER" "
    # Check if PostgreSQL is running, start if not
    if ! sudo systemctl is-active --quiet postgresql@14-main 2>/dev/null && ! sudo systemctl is-active --quiet postgresql 2>/dev/null; then
        echo 'Starting PostgreSQL...'
        sudo systemctl start postgresql@14-main 2>/dev/null || sudo systemctl start postgresql 2>/dev/null || true
        sleep 3
    fi
" || true

# Stop existing service and free port 9000
# Prevents port conflicts and ensures clean deployment
# Also stop old 'precast' service if present (legacy path) so it does not grab port 9000
log_info "  Stopping service and freeing port $PORT..."
ssh -i "$SSH_KEY" "$SERVER" "
    # Stop NEW backend service (precast-backend)
    sudo systemctl stop $APP_NAME 2>/dev/null || true
    # Stop OLD service if it exists (was at /var/www/.../main) - prevents 502 after deploy
    sudo systemctl stop precast.service 2>/dev/null || true
    sudo systemctl disable precast.service 2>/dev/null || true
    sleep 2
    # Kill any process using port 9000 (fuser or lsof)
    if command -v fuser &>/dev/null; then
        sudo fuser -k $PORT/tcp 2>/dev/null || true
    elif command -v lsof &>/dev/null; then
        sudo lsof -ti:$PORT | xargs -r sudo kill -9 2>/dev/null || true
    fi
    # Kill any remaining backend processes (both names)
    sudo pkill -f precast-backend 2>/dev/null || true
    sudo pkill -f 'precastezy.blueinvent.com/api/main' 2>/dev/null || true
    sudo pkill -f 'precast.blueinvent.com/api/main' 2>/dev/null || true
    sudo pkill -f '^main ' 2>/dev/null || true
    sleep 2
" || true

# Create backup of existing deployment directory
# Allows rollback if new deployment has issues
log_info "  Creating backup..."
BACKUP_DIR="$APP_DIR-backup-$(date +%Y%m%d-%H%M%S)"
ssh -i "$SSH_KEY" "$SERVER" "
    if [ -d '$APP_DIR' ]; then
        # Move existing directory to timestamped backup
        sudo mv '$APP_DIR' '$BACKUP_DIR' 2>/dev/null || true
        # Keep only last 3 backups, remove older ones to save disk space
        sudo find /home/ubuntu -maxdepth 1 -type d -name '$APP_NAME-backup-*' 2>/dev/null | sort -r | tail -n +4 | while read dir; do
            [ -n \"\$dir\" ] && sudo rm -rf \"\$dir\" 2>/dev/null || true
        done
    fi
    # Create fresh application directory with correct ownership
    sudo mkdir -p '$APP_DIR'
    sudo chown -R ubuntu:ubuntu '$APP_DIR'
" || {
    log_error "  Failed to prepare application directory"
    exit 1
}
log_success "  Backup created (if existed)"

# Upload all project files to server using rsync
# rsync is efficient: only transfers changed files, preserves permissions
log_info "  Uploading project files..."
    if rsync -avz --delete \
        -e "ssh -i $SSH_KEY -o StrictHostKeyChecking=no" \
        "$LOCAL_DIR/" \
        "$SERVER:$APP_DIR/" \
        --exclude='.git' \
        --exclude='node_modules' \
        --exclude='*.log' \
        --exclude='main' \
        --exclude='backend' \
        --exclude='*.swp' \
        --exclude='*.tmp' \
        --exclude='.env' &>/dev/null; then
        log_success "  Files uploaded"
    else
        log_error "  Failed to upload files"
        exit 1
    fi
    
# Install or verify Go installation on server
# Go is required to build the application. Downloads and installs Go 1.23.5 if missing
log_info "  Checking/Installing Go..."
ssh -i "$SSH_KEY" "$SERVER" "
    export PATH=\$PATH:/usr/local/go/bin
    if ! command -v go &>/dev/null; then
        echo 'Installing Go 1.23.5...'
        cd /tmp
        # Download Go binary distribution
        wget -q https://go.dev/dl/go1.23.5.linux-amd64.tar.gz
        # Remove old Go installation if exists
        sudo rm -rf /usr/local/go
        # Extract to /usr/local
        sudo tar -C /usr/local -xzf go1.23.5.linux-amd64.tar.gz
        rm go1.23.5.linux-amd64.tar.gz
        # Add Go to PATH in bashrc for future sessions
        echo 'export PATH=\$PATH:/usr/local/go/bin' >> ~/.bashrc
    fi
    export PATH=\$PATH:/usr/local/go/bin
    go version
" || {
    log_error "  Failed to install/verify Go"
    exit 1
}
log_success "  Go is ready"

# Build the Go application on the server
# Downloads dependencies, compiles the binary, and makes it executable
log_info "  Building application (this may take 1-2 minutes, do not cancel)..."
BUILD_OUTPUT=$(ssh -i "$SSH_KEY" "$SERVER" "
    cd $APP_DIR
    export PATH=\$PATH:/usr/local/go/bin
    export GOPATH=/home/ubuntu/go
    # Download all Go module dependencies
    go mod download 2>&1
    # Build binary with size optimization flags (-s -w strips debug info)
    if go build -o precast-backend -ldflags='-s -w' . 2>&1; then
        if [ -f precast-backend ]; then
            # Make binary executable
            sudo chmod +x precast-backend
            echo 'BUILD_SUCCESS'
        else
            echo 'BUILD_FAILED_NO_BINARY'
        fi
    else
        echo 'BUILD_FAILED'
    fi
" 2>&1)

if echo "$BUILD_OUTPUT" | grep -q "BUILD_SUCCESS"; then
    log_success "  Build successful"
else
    log_error "  Build failed"
    echo "$BUILD_OUTPUT" | grep -v "BUILD_" | tail -10 || true
    exit 1
fi

# Ensure .env file exists with database configuration
# Creates default .env if missing (application reads this for DB connection)
log_info "  Checking .env file..."
    ssh -i "$SSH_KEY" "$SERVER" "
        if [ ! -f '$APP_DIR/.env' ]; then
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
    " || true
    
# Create or update systemd service file
# Systemd manages the application lifecycle (start, stop, restart, auto-start on boot)
log_info "  Creating systemd service..."
SERVICE_CREATE_OUTPUT=$(ssh -i "$SSH_KEY" "$SERVER" "
    # Write systemd service file with proper configuration
    sudo tee /etc/systemd/system/$APP_NAME.service > /dev/null <<EOFSERVICE
[Unit]
Description=Precast Backend Go Application
After=network.target postgresql.service

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/precast-backend
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=$APP_NAME
Environment=\"PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"
Environment=\"HOME=/home/ubuntu\"
Environment=\"PORT=9000\"

[Install]
WantedBy=multi-user.target
EOFSERVICE
    # Reload systemd to recognize new/updated service
    sudo systemctl daemon-reload
    # Enable service to start on boot
    sudo systemctl enable $APP_NAME 2>&1
    echo 'SERVICE_CREATED'
" 2>&1)

if echo "$SERVICE_CREATE_OUTPUT" | grep -q "SERVICE_CREATED"; then
    log_success "  Systemd service created and enabled"
else
    log_error "  Failed to create systemd service"
    echo "$SERVICE_CREATE_OUTPUT" | grep -v "SERVICE_CREATED" || true
    exit 1
fi

# Start the service and verify it's running
# Single SSH: daemon-reload, reset-failed, free port, start, wait 8s, then check is-active in same session.
log_info "  Starting service..."
    set +e
    START_OUTPUT=$(ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=10 -i "$SSH_KEY" "$SERVER" "
        if [ ! -f '$APP_DIR/precast-backend' ]; then echo 'BINARY_MISSING'; exit 0; fi
        [ ! -x '$APP_DIR/precast-backend' ] && sudo chmod +x '$APP_DIR/precast-backend'
        sudo systemctl daemon-reload
        sudo systemctl reset-failed $APP_NAME 2>/dev/null || true
        if command -v fuser &>/dev/null; then sudo fuser -k $PORT/tcp 2>/dev/null || true; else sudo lsof -ti:$PORT | xargs -r sudo kill -9 2>/dev/null || true; fi
        sudo pkill -f precast-backend 2>/dev/null || true
        sudo pkill -f 'precastezy.blueinvent.com/api/main' 2>/dev/null || true
        sudo pkill -f 'precast.blueinvent.com/api/main' 2>/dev/null || true
        sleep 3
        sudo systemctl start $APP_NAME 2>&1 || true
        sleep 8
        if sudo systemctl is-active --quiet $APP_NAME 2>/dev/null; then
            echo 'START_SUCCESS'
        else
            echo 'START_FAILED'
            LOGF=/tmp/precast-start-debug.\$\$
            { sudo systemctl status $APP_NAME --no-pager -l 2>&1; echo '--- JOURNALCTL (last 30) ---'; sudo journalctl -u $APP_NAME -n 30 --no-pager 2>&1; } > \"\$LOGF\"
            cat \"\$LOGF\"
            rm -f \"\$LOGF\"
        fi
        true
    " 2>&1)
    set -e

    if echo "$START_OUTPUT" | grep -q "BINARY_MISSING"; then
        log_error "  Binary not found at $APP_DIR/precast-backend (build may have failed)"
        exit 1
    fi

    if echo "$START_OUTPUT" | grep -q "START_SUCCESS"; then
        log_success "  Service is running"
        # Wait for backend to listen and respond, then reload nginx (fixes 502 Bad Gateway)
        log_info "  Waiting for backend to respond on port $PORT..."
        WAIT_OUTPUT=$(ssh -i "$SSH_KEY" "$SERVER" "
            for i in \$(seq 1 30); do
                if curl -s -o /dev/null -w '%{http_code}' --connect-timeout 2 http://localhost:$PORT/swagger/doc.json 2>/dev/null | grep -q 200; then
                    echo 'BACKEND_READY'
                    sudo systemctl reload nginx 2>/dev/null || sudo systemctl restart nginx 2>/dev/null || true
                    exit 0
                fi
                sleep 1
            done
            echo 'BACKEND_NOT_READY'
        " 2>&1)
        if echo "$WAIT_OUTPUT" | grep -q "BACKEND_READY"; then
            log_success "  Backend is responding; nginx reloaded"
        else
            log_warning "  Backend not yet responding (nginx will be reloaded after health check)"
        fi
    else
        log_error "  Service not active after start (debug output below)"
        if [ -n "$START_OUTPUT" ]; then
            echo "$START_OUTPUT" | head -120
        else
            log_info "  (no output from first start attempt)"
        fi
        # Always fetch current status and logs from server (in case SSH capture missed them)
        log_info "  Fetching service status and logs from server..."
        DIAG_OUTPUT=$(ssh -o ConnectTimeout=15 -i "$SSH_KEY" "$SERVER" "
            echo '=== systemctl status ==='
            sudo systemctl status $APP_NAME --no-pager -l 2>&1 || true
            echo ''
            echo '=== journalctl (last 40 lines) ==='
            sudo journalctl -u $APP_NAME -n 40 --no-pager 2>&1 || true
        " 2>&1)
        echo "$DIAG_OUTPUT" | head -100
        log_info "  Retrying once..."
        set +e
        RETRY_OUTPUT=$(ssh -o ServerAliveInterval=30 -i "$SSH_KEY" "$SERVER" "
            sudo systemctl daemon-reload
            sudo systemctl reset-failed $APP_NAME 2>/dev/null || true
            sudo systemctl stop $APP_NAME 2>/dev/null || true
            sudo fuser -k $PORT/tcp 2>/dev/null || true
            sudo pkill -f precast-backend 2>/dev/null || true
            sleep 3
            sudo systemctl start $APP_NAME 2>&1 || true
            sleep 8
            if sudo systemctl is-active --quiet $APP_NAME 2>/dev/null; then echo 'START_SUCCESS'; else echo 'START_FAILED'; fi
            echo '=== status after retry ==='
            sudo systemctl status $APP_NAME --no-pager -l 2>&1 || true
            echo '=== journalctl (last 35) ==='
            sudo journalctl -u $APP_NAME -n 35 --no-pager 2>&1 || true
            true
        " 2>&1)
        set -e
        echo "$RETRY_OUTPUT" | head -120
        if echo "$RETRY_OUTPUT" | grep -q "START_SUCCESS"; then
            log_success "  Service started on retry"
        else
            log_error "  Service still not running after retry."
            log_info "  Run manually: ssh -i $SSH_KEY $SERVER 'sudo systemctl start precast-backend && sudo systemctl reload nginx'"
            exit 1
        fi
    fi
    
    # Wait and verify
    log_info "  Waiting for service to initialize..."
    sleep 5
    
    SERVICE_STATUS=$(ssh -i "$SSH_KEY" "$SERVER" "
        if sudo systemctl is-active --quiet $APP_NAME; then
            echo 'ACTIVE'
        else
            echo 'INACTIVE'
        fi
    " 2>&1 | tr -d '\r\n' | xargs)
    
    if [ "$SERVICE_STATUS" == "ACTIVE" ]; then
        log_success "  Service is active and running"
    else
        log_warning "  Service is not active, attempting to restart..."
        ssh -i "$SSH_KEY" "$SERVER" "sudo systemctl restart $APP_NAME" || {
            log_error "  Failed to restart service"
            log_info "  Service status:"
            ssh -i "$SSH_KEY" "$SERVER" "sudo systemctl status $APP_NAME --no-pager -l" 2>&1 | head -20 || true
            log_info "  Recent service logs:"
            ssh -i "$SSH_KEY" "$SERVER" "sudo journalctl -u $APP_NAME -n 30 --no-pager" 2>&1 | tail -20 || true
            exit 1
        }
        sleep 3
        SERVICE_STATUS_RETRY=$(ssh -i "$SSH_KEY" "$SERVER" "
            if sudo systemctl is-active --quiet $APP_NAME; then
                echo 'ACTIVE'
            else
                echo 'INACTIVE'
            fi
        " 2>&1 | tr -d '\r\n' | xargs)
        if [ "$SERVICE_STATUS_RETRY" == "ACTIVE" ]; then
            log_success "  Service restarted successfully"
        else
            log_error "  Service failed to start after restart"
            log_info "  Service status:"
            ssh -i "$SSH_KEY" "$SERVER" "sudo systemctl status $APP_NAME --no-pager -l" 2>&1 | head -20 || true
            log_info "  Recent service logs:"
            ssh -i "$SSH_KEY" "$SERVER" "sudo journalctl -u $APP_NAME -n 30 --no-pager" 2>&1 | tail -20 || true
            exit 1
        fi
    fi
    
# Verify port 9000 is actually listening for connections
# If not listening, attempts service restart automatically
log_info "  Verifying port $PORT is listening..."
sleep 3
PORT_CHECK=$(ssh -i "$SSH_KEY" "$SERVER" "
    # Use ss (preferred) or netstat to check if port is listening
    if command -v ss &>/dev/null; then
        ss -tlnp 2>/dev/null | grep \":$PORT\" || echo 'NOT_LISTENING'
    elif command -v netstat &>/dev/null; then
        netstat -tlnp 2>/dev/null | grep \":$PORT\" || echo 'NOT_LISTENING'
    else
        echo 'CANNOT_CHECK'
    fi
" 2>&1)

if echo "$PORT_CHECK" | grep -q ":$PORT"; then
    log_success "  Port $PORT is listening"
    echo "$PORT_CHECK" | grep ":$PORT" | head -1 || true
elif echo "$PORT_CHECK" | grep -q "NOT_LISTENING"; then
    # Automatic restart attempt if port is not listening
    log_warning "  Port $PORT is not listening, attempting to restart service..."
    ssh -i "$SSH_KEY" "$SERVER" "sudo systemctl restart $APP_NAME" || true
    sleep 5
    # Re-check port after restart
    PORT_CHECK_RETRY=$(ssh -i "$SSH_KEY" "$SERVER" "
        if command -v ss &>/dev/null; then
            ss -tlnp 2>/dev/null | grep \":$PORT\" || echo 'NOT_LISTENING'
        elif command -v netstat &>/dev/null; then
            netstat -tlnp 2>/dev/null | grep \":$PORT\" || echo 'NOT_LISTENING'
        else
            echo 'CANNOT_CHECK'
        fi
    " 2>&1)
    if echo "$PORT_CHECK_RETRY" | grep -q ":$PORT"; then
        log_success "  Port $PORT is now listening after restart"
    else
        log_error "  Port $PORT is still not listening"
        log_info "  Checking service logs for errors..."
        ssh -i "$SSH_KEY" "$SERVER" "sudo journalctl -u $APP_NAME -n 50 --no-pager" 2>&1 | grep -i "error\|fail\|panic" | tail -10 || true
    fi
else
    log_warning "  Could not verify port status"
fi

# Verify backend responds to HTTP requests (health check)
# Tests Swagger endpoint to ensure backend is fully operational
# If health check fails, attempts automatic restart
log_info "  Verifying backend health..."
sleep 2
HEALTH_CHECK=$(ssh -i "$SSH_KEY" "$SERVER" "
    # Test Swagger doc.json endpoint with 5 second timeout
    HTTP_CODE=\$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 5 http://localhost:$PORT/swagger/doc.json 2>/dev/null || echo '000')
    echo \$HTTP_CODE
" 2>&1 | tr -d '\r\n' | xargs)

if [ "$HEALTH_CHECK" == "200" ]; then
    log_success "  Backend is responding on port $PORT"
    # Reload nginx so it reconnects to backend (fixes 502 Bad Gateway after deploy)
    log_info "  Reloading nginx to reconnect to backend..."
    ssh -i "$SSH_KEY" "$SERVER" "sudo systemctl reload nginx 2>/dev/null || sudo systemctl restart nginx 2>/dev/null || true" 2>/dev/null && log_success "  Nginx reloaded" || log_warning "  Nginx reload skipped (not installed or failed)"
else
    # Automatic restart attempt if health check fails
    log_warning "  Backend health check failed (HTTP $HEALTH_CHECK), attempting restart..."
    ssh -i "$SSH_KEY" "$SERVER" "sudo systemctl restart $APP_NAME" || true
    sleep 5
    # Re-check health after restart
    HEALTH_CHECK_RETRY=$(ssh -i "$SSH_KEY" "$SERVER" "
        HTTP_CODE=\$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 5 http://localhost:$PORT/swagger/doc.json 2>/dev/null || echo '000')
        echo \$HTTP_CODE
    " 2>&1 | tr -d '\r\n' | xargs)
    if [ "$HEALTH_CHECK_RETRY" == "200" ]; then
        log_success "  Backend is now responding after restart"
    else
        log_error "  Backend health check still failing (HTTP $HEALTH_CHECK_RETRY)"
        log_info "  This may cause 502 Bad Gateway errors"
        log_info "  Quick fix for 502 (run from your machine):"
        echo -e "    ${GREEN}ssh -i $SSH_KEY $SERVER 'sudo systemctl restart $APP_NAME && sleep 10 && sudo systemctl reload nginx'${NC}"
        log_info "  Troubleshooting steps:"
        echo -e "    ${YELLOW}1. Check service logs: ssh -i $SSH_KEY $SERVER 'sudo journalctl -u $APP_NAME -f'${NC}"
        echo -e "    ${YELLOW}2. Check if port is listening: ssh -i $SSH_KEY $SERVER 'sudo ss -tlnp | grep $PORT'${NC}"
        echo -e "    ${YELLOW}3. Verify nginx config points to localhost:$PORT${NC}"
        echo -e "    ${YELLOW}4. Check database connection in logs${NC}"
    fi
fi

# Verify Swagger documentation endpoint is accessible
# Swagger UI provides API documentation interface
log_info "  Verifying Swagger documentation..."
    SWAGGER_CHECK=$(ssh -i "$SSH_KEY" "$SERVER" "
        HTTP_CODE=\$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 5 http://localhost:$PORT/swagger/doc.json 2>/dev/null || echo '000')
        echo \$HTTP_CODE
    " 2>&1 | tr -d '\r\n' | xargs)
    
    if [ "$SWAGGER_CHECK" == "200" ]; then
        log_success "  Swagger is accessible"
        echo -e "${GREEN}    Swagger UI: https://precastezy.blueinvent.com/swagger/index.html${NC}"
    else
        log_warning "  Swagger check returned HTTP $SWAGGER_CHECK"
        echo -e "${YELLOW}    Swagger should be at: https://precastezy.blueinvent.com/swagger/index.html${NC}"
    fi

################################################################################
# Step 5: Post-Deployment Verification
# 
# PURPOSE:
#   Perform comprehensive final health check to ensure deployment was successful.
#   Checks service status, port listening, and HTTP response all at once.
#   Automatically attempts restart if any check fails.
#
# CHECKS PERFORMED:
#   - Service is active (systemd)
#   - Port 9000 is listening
#   - HTTP endpoint responds with 200 OK
################################################################################

log_info "\nStep 5: Post-deployment verification..."

# Comprehensive final health check combining all verification steps
log_info "  Performing final service health check..."
FINAL_STATUS=$(ssh -i "$SSH_KEY" "$SERVER" "
    # Check all three health indicators simultaneously
    SERVICE_ACTIVE=\$(sudo systemctl is-active --quiet $APP_NAME && echo 'YES' || echo 'NO')
    PORT_LISTENING=\$(sudo ss -tlnp 2>/dev/null | grep \":$PORT\" > /dev/null && echo 'YES' || echo 'NO')
    HTTP_RESPONSE=\$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 5 http://localhost:$PORT/swagger/doc.json 2>/dev/null || echo '000')
    echo \"SERVICE:\$SERVICE_ACTIVE|PORT:\$PORT_LISTENING|HTTP:\$HTTP_RESPONSE\"
" 2>&1)

# Parse health check results
SERVICE_OK=$(echo "$FINAL_STATUS" | grep -o "SERVICE:YES" || echo "")
PORT_OK=$(echo "$FINAL_STATUS" | grep -o "PORT:YES" || echo "")
HTTP_OK=$(echo "$FINAL_STATUS" | grep -o "HTTP:200" || echo "")

# If all checks pass, deployment is successful
if [ -n "$SERVICE_OK" ] && [ -n "$PORT_OK" ] && [ -n "$HTTP_OK" ]; then
    log_success "  All health checks passed"
else
    # Automatic fix attempt: restart service and re-check
    log_warning "  Some health checks failed, attempting automatic fix..."
    
    # Final restart attempt
    ssh -i "$SSH_KEY" "$SERVER" "sudo systemctl restart $APP_NAME" || true
    sleep 5
    
    # Re-check all health indicators after restart
    FINAL_STATUS_RETRY=$(ssh -i "$SSH_KEY" "$SERVER" "
        SERVICE_ACTIVE=\$(sudo systemctl is-active --quiet $APP_NAME && echo 'YES' || echo 'NO')
        PORT_LISTENING=\$(sudo ss -tlnp 2>/dev/null | grep \":$PORT\" > /dev/null && echo 'YES' || echo 'NO')
        HTTP_RESPONSE=\$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 5 http://localhost:$PORT/swagger/doc.json 2>/dev/null || echo '000')
        echo \"SERVICE:\$SERVICE_ACTIVE|PORT:\$PORT_LISTENING|HTTP:\$HTTP_RESPONSE\"
    " 2>&1)
    
    # Parse retry results
    SERVICE_OK_RETRY=$(echo "$FINAL_STATUS_RETRY" | grep -o "SERVICE:YES" || echo "")
    PORT_OK_RETRY=$(echo "$FINAL_STATUS_RETRY" | grep -o "PORT:YES" || echo "")
    HTTP_OK_RETRY=$(echo "$FINAL_STATUS_RETRY" | grep -o "HTTP:200" || echo "")
    
    if [ -n "$SERVICE_OK_RETRY" ] && [ -n "$PORT_OK_RETRY" ] && [ -n "$HTTP_OK_RETRY" ]; then
        log_success "  Health checks passed after automatic restart"
    else
        log_warning "  Some issues remain after automatic fix attempt"
        echo "$FINAL_STATUS_RETRY"
    fi
fi

################################################################################
# Final Summary
# 
# PURPOSE:
#   Display deployment summary with service information, useful commands,
#   and troubleshooting tips. Shows error/warning counts.
################################################################################

echo -e "\n${GREEN}========================================${NC}"
echo -e "${GREEN}Deployment Summary${NC}"
echo -e "${GREEN}========================================${NC}"

# Display success message if no errors occurred
if [ $ERRORS -eq 0 ]; then
    log_success "Deployment completed successfully!"
    echo -e "\n${BLUE}Service Information:${NC}"
    echo -e "  Server: $SERVER"
    echo -e "  Application: $APP_NAME"
    echo -e "  Directory: $APP_DIR"
    echo -e "  Swagger UI: https://precastezy.blueinvent.com/swagger/index.html (or http://${SERVER_IP}/swagger/index.html)"
    echo -e "\n${BLUE}Useful Commands:${NC}"
    echo -e "  Status: ssh -i $SSH_KEY $SERVER 'sudo systemctl status $APP_NAME'"
    echo -e "  Logs:   ssh -i $SSH_KEY $SERVER 'sudo journalctl -u $APP_NAME -f'"
    echo -e "  Restart: ssh -i $SSH_KEY $SERVER 'sudo systemctl restart $APP_NAME'"
    echo -e "\n${BLUE}Troubleshooting 502 Bad Gateway:${NC}"
    echo -e "  1. Check backend: ssh -i $SSH_KEY $SERVER 'curl http://localhost:$PORT/swagger/doc.json'"
    echo -e "  2. Check port: ssh -i $SSH_KEY $SERVER 'sudo ss -tlnp | grep $PORT'"
    echo -e "  3. Check nginx: ssh -i $SSH_KEY $SERVER 'sudo nginx -t'"
    echo -e "  4. Restart nginx: ssh -i $SSH_KEY $SERVER 'sudo systemctl restart nginx'"
    echo -e "  5. Quick check: ./check-backend.sh"
else
    # Display error summary if deployment had errors
    log_error "Deployment completed with $ERRORS error(s)"
    if [ $WARNINGS -gt 0 ]; then
        log_warning "  $WARNINGS warning(s) encountered"
    fi
    exit 1
fi

# Display warning summary (non-critical issues)
if [ $WARNINGS -gt 0 ]; then
    log_warning "  $WARNINGS warning(s) encountered (non-critical)"
fi

echo ""
