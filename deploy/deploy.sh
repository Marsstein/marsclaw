#!/usr/bin/env bash
# MarsClaw GCP Deploy — creates a VM and deploys MarsClaw in one command.
# Usage: ./deploy.sh
#
# Prerequisites:
#   - gcloud CLI installed and authenticated (gcloud auth login)
#   - A GCP project set (gcloud config set project YOUR_PROJECT)
#
# What this does:
#   1. Creates a small GCP VM in Europe (Frankfurt)
#   2. Opens firewall for HTTP (port 8080)
#   3. Copies setup script to the VM
#   4. Runs setup (installs Go, MarsClaw, CLIs, systemd service)
#   5. Prints access URLs
#
# Cost: ~$15/month on e2-small, paid from GCP credits.

set -euo pipefail

# --- Config (edit these) ---
VM_NAME="${VM_NAME:-marsclaw-worker}"
ZONE="${ZONE:-europe-west3-a}"
MACHINE_TYPE="${MACHINE_TYPE:-e2-small}"
IMAGE_FAMILY="ubuntu-2404-lts-amd64"
IMAGE_PROJECT="ubuntu-os-cloud"
DISK_SIZE="20GB"
TAGS="marsclaw,http-server"

# Colors.
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()  { echo -e "${CYAN}▸${NC} $*"; }
ok()    { echo -e "${GREEN}✓${NC} $*"; }
err()   { echo -e "${RED}✗${NC} $*" >&2; }

# --- Preflight checks ---
if ! command -v gcloud &>/dev/null; then
    err "gcloud CLI not found. Install: https://cloud.google.com/sdk/docs/install"
    exit 1
fi

PROJECT=$(gcloud config get-value project 2>/dev/null)
if [ -z "$PROJECT" ] || [ "$PROJECT" = "(unset)" ]; then
    err "No GCP project set. Run: gcloud config set project YOUR_PROJECT"
    exit 1
fi

info "Project: ${BOLD}$PROJECT${NC}"
info "VM: ${BOLD}$VM_NAME${NC} ($MACHINE_TYPE) in ${BOLD}$ZONE${NC}"
echo ""
read -p "Continue? [Y/n] " -r REPLY
REPLY=${REPLY:-Y}
if [[ ! "$REPLY" =~ ^[Yy]$ ]]; then
    echo "Aborted."
    exit 0
fi

# --- Step 1: Create firewall rule (idempotent) ---
info "Creating firewall rule for port 8080..."
if ! gcloud compute firewall-rules describe allow-marsclaw --project="$PROJECT" &>/dev/null; then
    gcloud compute firewall-rules create allow-marsclaw \
        --project="$PROJECT" \
        --direction=INGRESS \
        --action=ALLOW \
        --rules=tcp:8080 \
        --source-ranges=0.0.0.0/0 \
        --target-tags=marsclaw \
        --description="Allow MarsClaw Web UI"
    ok "Firewall rule created"
else
    ok "Firewall rule already exists"
fi

# --- Step 2: Create VM ---
info "Creating VM..."
if gcloud compute instances describe "$VM_NAME" --zone="$ZONE" --project="$PROJECT" &>/dev/null; then
    ok "VM $VM_NAME already exists"
else
    gcloud compute instances create "$VM_NAME" \
        --project="$PROJECT" \
        --zone="$ZONE" \
        --machine-type="$MACHINE_TYPE" \
        --image-family="$IMAGE_FAMILY" \
        --image-project="$IMAGE_PROJECT" \
        --boot-disk-size="$DISK_SIZE" \
        --tags="$TAGS" \
        --metadata=enable-oslogin=true
    ok "VM created"
fi

# Wait for SSH to be ready.
info "Waiting for SSH..."
for i in $(seq 1 30); do
    if gcloud compute ssh "$VM_NAME" --zone="$ZONE" --project="$PROJECT" --command="echo ready" &>/dev/null; then
        break
    fi
    sleep 2
done
ok "SSH ready"

# --- Step 3: Copy setup script ---
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
info "Copying setup script to VM..."
gcloud compute scp "$SCRIPT_DIR/setup.sh" "$VM_NAME":~/setup.sh \
    --zone="$ZONE" --project="$PROJECT"
ok "Setup script copied"

# --- Step 4: Copy config template ---
if [ -f "$SCRIPT_DIR/config.production.yaml" ]; then
    info "Copying production config..."
    gcloud compute scp "$SCRIPT_DIR/config.production.yaml" "$VM_NAME":~/config.production.yaml \
        --zone="$ZONE" --project="$PROJECT"
    ok "Config copied"
fi

# --- Step 5: Run setup on the VM ---
info "Running setup on VM (this takes 2-3 minutes)..."
gcloud compute ssh "$VM_NAME" --zone="$ZONE" --project="$PROJECT" -- \
    "chmod +x ~/setup.sh && sudo ~/setup.sh"
ok "Setup complete"

# --- Step 6: Print access info ---
EXTERNAL_IP=$(gcloud compute instances describe "$VM_NAME" \
    --zone="$ZONE" --project="$PROJECT" \
    --format='get(networkInterfaces[0].accessConfigs[0].natIP)')

echo ""
echo -e "${GREEN}${BOLD}MarsClaw is running!${NC}"
echo ""
echo -e "  Web UI:     ${CYAN}http://${EXTERNAL_IP}:8080${NC}"
echo -e "  SSH:        ${CYAN}gcloud compute ssh ${VM_NAME} --zone=${ZONE}${NC}"
echo -e "  Logs:       ${CYAN}gcloud compute ssh ${VM_NAME} --zone=${ZONE} -- journalctl -u marsclaw -f${NC}"
echo -e "  WhatsApp:   ${CYAN}http://${EXTERNAL_IP}:8080/webhook/whatsapp${NC}"
echo ""
echo -e "${BOLD}Next steps:${NC}"
echo "  1. SSH in and set your API keys in /etc/marsclaw/env"
echo "  2. Edit /etc/marsclaw/config.yaml for your scheduler tasks"
echo "  3. Restart: sudo systemctl restart marsclaw"
echo ""
