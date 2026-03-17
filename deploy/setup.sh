#!/usr/bin/env bash
# MarsClaw server setup — runs on the GCP VM.
# Called by deploy.sh, or run manually: sudo ./setup.sh
#
# Installs: Go, MarsClaw, gh CLI, systemd service.
# Creates: /etc/marsclaw/ (config + env), deploy user.

set -euo pipefail

GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m'

info() { echo -e "${CYAN}▸${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

export DEBIAN_FRONTEND=noninteractive

# --- System deps ---
info "Installing system packages..."
apt-get update -qq
apt-get install -y -qq git curl jq sqlite3 unzip > /dev/null
ok "System packages"

# --- Go ---
GO_VERSION="1.24.1"
if ! command -v go &>/dev/null; then
    info "Installing Go ${GO_VERSION}..."
    curl -sSfL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xzf -
    ln -sf /usr/local/go/bin/go /usr/local/bin/go
    ok "Go ${GO_VERSION}"
else
    ok "Go already installed: $(go version)"
fi

# --- Deploy user ---
if ! id -u deploy &>/dev/null; then
    info "Creating deploy user..."
    useradd -m -s /bin/bash deploy
    ok "User 'deploy' created"
else
    ok "User 'deploy' exists"
fi

# --- MarsClaw binary ---
info "Building MarsClaw..."
sudo -u deploy bash -c '
    export HOME=/home/deploy
    export GOPATH=$HOME/go
    export PATH=$PATH:/usr/local/go/bin:$GOPATH/bin
    go install github.com/marsstein/marsclaw/cmd/marsclaw@latest
'
ln -sf /home/deploy/go/bin/marsclaw /usr/local/bin/marsclaw
ok "MarsClaw installed: $(marsclaw --version 2>/dev/null || echo 'built')"

# --- GitHub CLI ---
if ! command -v gh &>/dev/null; then
    info "Installing GitHub CLI..."
    curl -sSfL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg 2>/dev/null
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list
    apt-get update -qq
    apt-get install -y -qq gh > /dev/null
    ok "GitHub CLI"
else
    ok "GitHub CLI already installed"
fi

# --- Config directory ---
info "Setting up /etc/marsclaw/..."
mkdir -p /etc/marsclaw
mkdir -p /home/deploy/.marsclaw

# Create env file (user fills in API keys).
if [ ! -f /etc/marsclaw/env ]; then
    cat > /etc/marsclaw/env <<'ENVFILE'
# MarsClaw environment — edit with: sudo nano /etc/marsclaw/env
# Then restart: sudo systemctl restart marsclaw

# LLM Provider (pick one as default)
GEMINI_API_KEY=
ANTHROPIC_API_KEY=
OPENAI_API_KEY=

# GitHub CLI (for PR reviews, issue tracking)
GITHUB_TOKEN=

# Slack notifications (optional)
SLACK_WEBHOOK_URL=

# WhatsApp (optional)
WHATSAPP_ACCESS_TOKEN=
WHATSAPP_VERIFY_TOKEN=

# Telegram (optional)
TELEGRAM_BOT_TOKEN=

# Discord (optional)
DISCORD_BOT_TOKEN=
ENVFILE
    chmod 600 /etc/marsclaw/env
    ok "Env file created at /etc/marsclaw/env"
else
    ok "Env file already exists"
fi

# Copy production config if provided.
if [ -f /home/*/config.production.yaml ]; then
    cp /home/*/config.production.yaml /etc/marsclaw/config.yaml
    ok "Production config copied"
elif [ ! -f /etc/marsclaw/config.yaml ]; then
    cat > /etc/marsclaw/config.yaml <<'CONFIG'
# MarsClaw production config
# Edit: sudo nano /etc/marsclaw/config.yaml
# Restart: sudo systemctl restart marsclaw

providers:
  default: gemini
  gemini:
    api_key_env: GEMINI_API_KEY
    default_model: gemini-2.5-flash
  anthropic:
    api_key_env: ANTHROPIC_API_KEY
    default_model: claude-sonnet-4-20250514
  ollama:
    default_model: llama3.1

agent:
  max_turns: 25
  max_consecutive_tool_calls: 15
  max_input_tokens: 180000
  max_output_tokens: 16384
  enable_streaming: true

cost:
  inline_display: true
  daily_budget: 50.00

security:
  strict_approval: false
  scan_credentials: true
  path_traversal_guard: true
  allowed_dirs:
    - /home/deploy

# Uncomment and customize scheduled tasks:
# scheduler:
#   tasks:
#     - name: morning-summary
#       schedule: "0 9 * * 1-5"
#       prompt: "Summarize yesterday's git activity"
#       channel: log
#       enabled: true
#     - name: security-scan
#       schedule: "0 2 * * *"
#       prompt: "Check for dependency vulnerabilities using gh CLI"
#       channel: log
#       enabled: true
CONFIG
    ok "Default config created"
else
    ok "Config already exists"
fi

# Link config for the deploy user.
ln -sf /etc/marsclaw/config.yaml /home/deploy/.marsclaw/config.yaml
chown -R deploy:deploy /home/deploy/.marsclaw

# --- Working directories ---
mkdir -p /home/deploy/content/weekly
chown -R deploy:deploy /home/deploy/content

# --- systemd service ---
info "Installing systemd service..."
cat > /etc/systemd/system/marsclaw.service <<'SERVICE'
[Unit]
Description=MarsClaw Agent Runtime
Documentation=https://github.com/marsstein/marsclaw
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=deploy
Group=deploy
EnvironmentFile=/etc/marsclaw/env
ExecStart=/usr/local/bin/marsclaw serve --addr :8080 --config /etc/marsclaw/config.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

# Security hardening.
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=/home/deploy/.marsclaw /home/deploy/content /tmp
PrivateTmp=true

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable marsclaw
systemctl start marsclaw
ok "MarsClaw service started"

# --- Final status ---
echo ""
echo -e "${GREEN}Setup complete!${NC}"
echo ""
echo "  Config:  /etc/marsclaw/config.yaml"
echo "  Env:     /etc/marsclaw/env        (add your API keys here)"
echo "  Logs:    journalctl -u marsclaw -f"
echo "  Status:  systemctl status marsclaw"
echo "  Restart: sudo systemctl restart marsclaw"
echo ""
echo "Next: edit /etc/marsclaw/env with your API keys, then restart."
