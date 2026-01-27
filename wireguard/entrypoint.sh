#!/bin/bash
set -e

# Load kernel modules if needed (won't work in unprivileged containers)
modprobe wireguard 2>/dev/null || true

# Create WireGuard config directory
mkdir -p /etc/wireguard

# Generate server keys if they don't exist
if [ ! -f /etc/wireguard/server_private.key ]; then
    echo "Generating WireGuard server keys..."
    wg genkey > /etc/wireguard/server_private.key
    wg pubkey < /etc/wireguard/server_private.key > /etc/wireguard/server_public.key
    chmod 600 /etc/wireguard/server_private.key
fi

SERVER_PRIVATE_KEY=$(cat /etc/wireguard/server_private.key)
SERVER_PUBLIC_KEY=$(cat /etc/wireguard/server_public.key)

echo "Server Public Key: $SERVER_PUBLIC_KEY"

# Create initial WireGuard config
cat > /etc/wireguard/${WG_INTERFACE}.conf << EOF
[Interface]
Address = ${WG_ADDRESS}
ListenPort = ${WG_PORT}
PrivateKey = ${SERVER_PRIVATE_KEY}
SaveConfig = false

# Note: IP forwarding is enabled via docker-compose sysctls

# NAT for VPN clients to reach challenge networks
PostUp = iptables -t nat -A POSTROUTING -s 10.13.0.0/16 -o eth0 -j MASQUERADE
PostUp = iptables -A FORWARD -i %i -j ACCEPT
PostUp = iptables -A FORWARD -o %i -j ACCEPT

PostDown = iptables -t nat -D POSTROUTING -s 10.13.0.0/16 -o eth0 -j MASQUERADE
PostDown = iptables -D FORWARD -i %i -j ACCEPT
PostDown = iptables -D FORWARD -o %i -j ACCEPT
EOF

# Add any pre-configured peers from environment
if [ -n "$WG_PEERS_JSON" ]; then
    echo "Loading pre-configured peers..."
    echo "$WG_PEERS_JSON" | jq -r '.[] | "[Peer]\nPublicKey = \(.publicKey)\nPresharedKey = \(.presharedKey)\nAllowedIPs = \(.allowedIp)\n"' >> /etc/wireguard/${WG_INTERFACE}.conf
fi

# Start WireGuard interface
echo "Starting WireGuard interface ${WG_INTERFACE}..."
wg-quick up ${WG_INTERFACE}

# Show initial status
wg show ${WG_INTERFACE}

# Start the management API
echo "Starting WireGuard management API on port ${API_PORT}..."
exec /usr/local/bin/wg-manager
