#!/bin/bash

set -euo pipefail

# Set variables
SSH_KEY_NAME="docker-droplet-key"
SSH_KEY_PATH="$HOME/.ssh/$SSH_KEY_NAME"
DROPLET_NAME="docker"
DROPLET_SIZE="s-1vcpu-1gb"
DROPLET_IMAGE="docker-20-04"
# Get current external IP address
IP_WHITELIST=$(curl -s https://api.ipify.org || curl -s https://ifconfig.me || curl -s https://icanhazip.com)
echo "Detected external IP: $IP_WHITELIST"

# Function to clean up droplet
cleanup() {
  if [[ -n "${DROPLET_ID:-}" ]]; then
    echo "Cleaning up droplet with ID: $DROPLET_ID"
    doctl compute droplet delete "$DROPLET_ID" --force
  fi
}

# Set trap to ensure cleanup runs on exit
trap cleanup EXIT

# Check if SSH key exists locally
if [ ! -f "$SSH_KEY_PATH" ]; then
  echo "Generating SSH key..."
  ssh-keygen -t rsa -b 4096 -f "$SSH_KEY_PATH" -N "" -C "docker-droplet-access"
else
  echo "Using existing SSH key at $SSH_KEY_PATH"
fi

# Check if SSH key exists in Digital Ocean
EXISTING_KEY_ID=$(doctl compute ssh-key list --format ID,Name --no-header | grep "$SSH_KEY_NAME" | awk '{print $1}' || echo "")

if [ -z "$EXISTING_KEY_ID" ]; then
  echo "Adding SSH key to Digital Ocean..."
  DO_SSH_KEY_ID=$(doctl compute ssh-key import "$SSH_KEY_NAME" --public-key-file "$SSH_KEY_PATH.pub" --format ID --no-header)
else
  echo "Using existing Digital Ocean SSH key with ID: $EXISTING_KEY_ID"
  DO_SSH_KEY_ID=$EXISTING_KEY_ID
fi

# Create user-data script
USER_DATA=$(cat <<EOF
#!/bin/bash
sudo ufw insert 1 allow from ${IP_WHITELIST} to any port 22
sudo ufw reload
EOF
)

echo "Creating Digital Ocean droplet..."
DROPLET_DATA=$(doctl compute droplet create "$DROPLET_NAME" \
  --image "$DROPLET_IMAGE" \
  --size "$DROPLET_SIZE" \
  --ssh-keys "$DO_SSH_KEY_ID" \
  --user-data "$USER_DATA" \
  --format ID,PublicIPv4 \
  --no-header \
  --wait)

DROPLET_ID=$(echo "$DROPLET_DATA" | awk '{print $1}')
DROPLET_IP=$(echo "$DROPLET_DATA" | awk '{print $2}')

echo "Droplet created with IP: $DROPLET_IP"

echo "Adding SSH key to agent..."
ssh-add "$SSH_KEY_PATH"

sleep 10

echo "Waiting for SSH to be available..."
until ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 "root@$DROPLET_IP" echo "SSH is up"; do
  echo "Waiting for SSH connection..."
  sleep 10
done

echo "Setting up Docker host environment..."
export DOCKER_HOST="ssh://root@$DROPLET_IP"
echo "Docker host set to: $DOCKER_HOST"

echo "Running Docker commands..."
until docker ps -a > /dev/null; do
  echo "Waiting for Docker to be available..."
  sleep 10
done

echo "Running tests..."
task