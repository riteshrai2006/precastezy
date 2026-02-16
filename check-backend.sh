#!/bin/bash

################################################################################
# Quick Backend Status Check Script
# 
# PURPOSE:
#   This script performs a quick health check of the backend service on the
#   remote server. It checks service status, port listening, HTTP response,
#   recent logs, and nginx status. Useful for troubleshooting 502 errors.
#
# USAGE:
#   ./check-backend.sh
#
# WHAT IT CHECKS:
#   1. Systemd service status (active/inactive)
#   2. Port 9000 listening status
#   3. Backend HTTP health endpoint response
#   4. Recent service logs (last 20 lines)
#   5. Nginx service status
#   6. Provides quick fix commands
#
# OUTPUT:
#   Color-coded status messages showing what's working and what's not
################################################################################

# Color codes for terminal output
RED='\033[0;31m'      # Error messages
GREEN='\033[0;32m'    # Success messages
YELLOW='\033[1;33m'   # Warning messages
BLUE='\033[0;34m'     # Info messages
NC='\033[0m'          # No Color (reset)

# Server configuration
SERVER="ubuntu@18.140.23.205"                    # Remote server SSH address
SSH_KEY="$HOME/Documents/jaildev.pem"           # SSH private key path
APP_NAME="precast-backend"                       # Systemd service name
PORT=9000                                         # Backend application port

echo -e "${BLUE}Checking backend status...${NC}\n"

################################################################################
# Check 1: Service Status
# Checks if the systemd service is running, stopped, or failed
################################################################################
echo -e "${BLUE}1. Service Status:${NC}"
ssh -i "$SSH_KEY" "$SERVER" "sudo systemctl status $APP_NAME --no-pager -l" 2>&1 | head -15

################################################################################
# Check 2: Port Listening Status
# Verifies that port 9000 is actually listening for connections
# Uses 'ss' command (preferred) or falls back to 'netstat'
################################################################################
echo -e "\n${BLUE}2. Port Listening Check:${NC}"
PORT_INFO=$(ssh -i "$SSH_KEY" "$SERVER" "
    if command -v ss &>/dev/null; then
        ss -tlnp 2>/dev/null | grep \":$PORT\" || echo 'NOT_LISTENING'
    elif command -v netstat &>/dev/null; then
        netstat -tlnp 2>/dev/null | grep \":$PORT\" || echo 'NOT_LISTENING'
    else
        echo 'CANNOT_CHECK'
    fi
" 2>&1)

# Check if port is listening
if echo "$PORT_INFO" | grep -q ":$PORT"; then
    echo -e "${GREEN}✓ Port $PORT is listening${NC}"
    echo "$PORT_INFO" | grep ":$PORT"
else
    echo -e "${RED}✗ Port $PORT is NOT listening${NC}"
fi

################################################################################
# Check 3: Backend Health Endpoint
# Tests if the backend responds to HTTP requests by checking Swagger endpoint
# Returns HTTP status code (200 = healthy, 000 = connection failed)
################################################################################
echo -e "\n${BLUE}3. Backend Health Check:${NC}"
HEALTH=$(ssh -i "$SSH_KEY" "$SERVER" "
    HTTP_CODE=\$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 5 http://localhost:$PORT/swagger/doc.json 2>/dev/null || echo '000')
    echo \$HTTP_CODE
" 2>&1 | tr -d '\r\n' | xargs)

if [ "$HEALTH" == "200" ]; then
    echo -e "${GREEN}✓ Backend is responding (HTTP 200)${NC}"
else
    echo -e "${RED}✗ Backend is NOT responding (HTTP $HEALTH)${NC}"
fi

################################################################################
# Check 4: Recent Service Logs
# Shows last 20 lines of service logs to help identify errors
################################################################################
echo -e "\n${BLUE}4. Recent Service Logs (last 20 lines):${NC}"
ssh -i "$SSH_KEY" "$SERVER" "sudo journalctl -u $APP_NAME -n 20 --no-pager" 2>&1 | tail -20

################################################################################
# Check 5: Nginx Status
# Verifies nginx is running (needed for reverse proxy to backend)
################################################################################
echo -e "\n${BLUE}5. Nginx Status:${NC}"
NGINX_STATUS=$(ssh -i "$SSH_KEY" "$SERVER" "sudo systemctl is-active nginx 2>/dev/null" 2>/dev/null | tr -d '\r\n' | xargs)
[ -z "$NGINX_STATUS" ] && NGINX_STATUS="UNKNOWN"
if [ "$NGINX_STATUS" == "active" ]; then
    echo -e "${GREEN}✓ Nginx is running${NC}"
else
    echo -e "${YELLOW}⚠ Nginx status: $NGINX_STATUS${NC}"
fi

################################################################################
# Quick Fix Commands
# Provides ready-to-use commands for common fixes
################################################################################
echo -e "\n${BLUE}6. Quick Fix Commands:${NC}"
echo -e "  Restart backend: ${YELLOW}ssh -i $SSH_KEY $SERVER 'sudo systemctl restart $APP_NAME'${NC}"
echo -e "  Restart nginx:   ${YELLOW}ssh -i $SSH_KEY $SERVER 'sudo systemctl restart nginx'${NC}"
echo -e "  View logs:       ${YELLOW}ssh -i $SSH_KEY $SERVER 'sudo journalctl -u $APP_NAME -f'${NC}"
