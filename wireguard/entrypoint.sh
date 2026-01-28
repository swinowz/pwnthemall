#!/bin/bash
set -e

# Load kernel modules if needed (won't work in unprivileged containers)
modprobe wireguard 2>/dev/null || true

# Create WireGuard config directory
mkdir -p /etc/wireguard

# Generate server keys if they don't exist (persisted in volume)
if [ ! -f /etc/wireguard/server_private.key ]; then
    echo "Generating WireGuard server keys..."
    wg genkey > /etc/wireguard/server_private.key
    wg pubkey < /etc/wireguard/server_private.key > /etc/wireguard/server_public.key
    chmod 600 /etc/wireguard/server_private.key
fi

SERVER_PRIVATE_KEY=$(cat /etc/wireguard/server_private.key)
SERVER_PUBLIC_KEY=$(cat /etc/wireguard/server_public.key)

echo "Server Public Key: $SERVER_PUBLIC_KEY"

# Sync server public key to database via backend API
if [ -n "$BACKEND_API_URL" ]; then
    echo "Syncing server public key to database..."
    for i in {1..30}; do
        if curl -f -X POST "${BACKEND_API_URL}/internal/vpn/sync-server-key" \
            -H "Content-Type: application/json" \
            -H "X-Internal-Secret: ${PTA_VPN_INTERNAL_SECRET:-vpn-internal-secret}" \
            -d "{\"serverPublicKey\":\"${SERVER_PUBLIC_KEY}\"}" 2>/dev/null; then
            echo "✓ Server public key synced successfully"
            break
        else
            echo "Waiting for backend... ($i/30)"
            sleep 2
        fi
    done
else
    echo "⚠ BACKEND_API_URL not set, skipping key sync"
fi

# Fetch peers from backend and build config
echo "Fetching VPN peers from database..."
PEERS_JSON=""
if [ -n "$BACKEND_API_URL" ]; then
    for i in {1..10}; do
        PEERS_JSON=$(curl -sf -X GET "${BACKEND_API_URL}/internal/vpn/peers" \
            -H "X-Internal-Secret: ${PTA_VPN_INTERNAL_SECRET:-vpn-internal-secret}" 2>/dev/null || echo "")
        if [ -n "$PEERS_JSON" ]; then
            echo "✓ Retrieved peers from database"
            break
        fi
        echo "Waiting for backend peers... ($i/10)"
        sleep 2
    done
fi

# Create WireGuard config
cat > /etc/wireguard/${WG_INTERFACE}.conf << EOF
[Interface]
Address = ${WG_ADDRESS}
ListenPort = ${WG_PORT}
PrivateKey = ${SERVER_PRIVATE_KEY}
SaveConfig = false

# Note: IP forwarding is enabled via docker-compose sysctls

# NAT and forwarding for VPN clients to reach challenge networks
PostUp = iptables -t nat -A POSTROUTING -s 10.13.0.0/16 -o eth0 -j MASQUERADE
PostUp = iptables -A FORWARD -i %i -j ACCEPT
PostUp = iptables -A FORWARD -o %i -j ACCEPT
PostUp = iptables -A FORWARD -s 10.13.0.0/16 -d 172.80.0.0/16 -j ACCEPT
PostUp = iptables -A FORWARD -s 172.80.0.0/16 -d 10.13.0.0/16 -j ACCEPT

PostDown = iptables -t nat -D POSTROUTING -s 10.13.0.0/16 -o eth0 -j MASQUERADE
PostDown = iptables -D FORWARD -i %i -j ACCEPT
PostDown = iptables -D FORWARD -o %i -j ACCEPT
PostDown = iptables -D FORWARD -s 10.13.0.0/16 -d 172.80.0.0/16 -j ACCEPT
PostDown = iptables -D FORWARD -s 172.80.0.0/16 -d 10.13.0.0/16 -j ACCEPT
EOF

# Add peers from database to config
if [ -n "$PEERS_JSON" ] && [ "$PEERS_JSON" != "null" ]; then
    echo "$PEERS_JSON" | jq -r '.[] | "[Peer]\nPublicKey = \(.publicKey)\nPresharedKey = \(.presharedKey)\nAllowedIPs = \(.allowedIp)\n"' >> /etc/wireguard/${WG_INTERFACE}.conf
    echo "✓ Added $(echo "$PEERS_JSON" | jq '. | length') peers to config"
fi

# Start WireGuard interface
echo "Starting WireGuard interface ${WG_INTERFACE}..."
wg-quick up ${WG_INTERFACE}

# Show initial status
wg show ${WG_INTERFACE}

# Connect to team challenge networks and setup NAT
# We need to wait a bit for Docker to be ready and networks to exist
sleep 3
echo "Setting up challenge network access..."

# Find all team networks using the Docker socket
TEAM_NETWORKS=$(curl -sf --unix-socket /var/run/docker.sock \
    http://localhost/networks 2>/dev/null | \
    jq -r '.[] | select(.Name | startswith("team_")) | .Name' || echo "")

if [ -n "$TEAM_NETWORKS" ]; then
    for network in $TEAM_NETWORKS; do
        echo "Connecting to $network..."
        # Connect to network via Docker API
        NETWORK_ID=$(curl -sf --unix-socket /var/run/docker.sock \
            http://localhost/networks 2>/dev/null | \
            jq -r ".[] | select(.Name == \"$network\") | .Id" || echo "")
        
        if [ -n "$NETWORK_ID" ]; then
            curl -sf --unix-socket /var/run/docker.sock \
                -X POST \
                -H "Content-Type: application/json" \
                -d "{\"Container\":\"wireguard\",\"EndpointConfig\":{}}" \
                "http://localhost/networks/$NETWORK_ID/connect" 2>/dev/null || true
        fi
    done
    
    # Add NAT rules for all team network interfaces (eth1, eth2, etc.)
    sleep 2
    for iface in $(ip link show | grep -E "^[0-9]+: eth[1-9]" | awk -F': ' '{print $2}' | cut -d'@' -f1); do
        echo "Adding NAT rule for $iface"
        iptables -t nat -I POSTROUTING 1 -s 10.13.0.0/16 -o "$iface" -j MASQUERADE 2>/dev/null || true
    done
    echo "✓ Challenge network access configured"
else
    echo "⚠ No team networks found yet"
fi

echo "✓ VPN setup complete"

# Start the management API
echo "Starting WireGuard management API on port ${API_PORT}..."
exec /usr/local/bin/wg-manager

# Connect to all existing team networks for challenge access
echo "Connecting to team networks..."
for network in $(docker network ls --filter "name=team_" --format "{{.Name}}"); do
    echo "Connecting to $network"
    docker network connect "$network" wireguard 2>/dev/null || echo "Already connected or failed: $network"
done

# Add MASQUERADE rules for each team network interface
for iface in $(ip link show | grep "^[0-9]" | grep -v "lo\|wg0\|eth0" | awk -F': ' '{print $2}' | cut -d'@' -f1); do
    echo "Adding NAT rule for interface $iface"
    iptables -t nat -I POSTROUTING 1 -s 10.13.0.0/16 -o "$iface" -j MASQUERADE 2>/dev/null || true
done

echo "✓ VPN setup complete"

# Start the management API
echo "Starting WireGuard management API on port ${API_PORT}..."
exec /usr/local/bin/wg-manager
