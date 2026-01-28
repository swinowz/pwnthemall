package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var (
	wgInterface = getEnv("WG_INTERFACE", "wg0")
	apiPort     = getEnv("API_PORT", "8080")
	configMutex sync.Mutex
)

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

type PeerRequest struct {
	PublicKey    string `json:"publicKey" binding:"required"`
	PresharedKey string `json:"presharedKey"`
	AllowedIP    string `json:"allowedIp" binding:"required"`
	TeamID       uint   `json:"teamId"`
}

type NetworkRequest struct {
	TeamID  uint   `json:"teamId" binding:"required"`
	Subnet  string `json:"subnet" binding:"required"`
	Gateway string `json:"gateway" binding:"required"`
}

type PeerStatus struct {
	PublicKey       string     `json:"publicKey"`
	AllowedIP       string     `json:"allowedIp"`
	Endpoint        string     `json:"endpoint"`
	LastHandshake   *time.Time `json:"lastHandshake"`
	BytesReceived   int64      `json:"bytesReceived"`
	BytesSent       int64      `json:"bytesSent"`
	Connected       bool       `json:"connected"`
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	// API routes
	api := r.Group("/api")
	{
	// Peer management
	api.POST("/peers", addPeer)
	api.DELETE("/peers/:publicKey", removePeer)
	api.GET("/peers", listPeers)
	api.GET("/peer/status", getPeerStatus)		// Network management
		api.POST("/networks", addNetwork)
		api.POST("/networks/connect", connectToNetwork)
		api.DELETE("/networks/:teamId", removeNetwork)

		// Server info
		api.GET("/server/info", getServerInfo)
		api.GET("/server/status", getServerStatus)
	}

	log.Printf("Starting WireGuard management API on port %s", apiPort)
	if err := r.Run(":" + apiPort); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func addPeer(c *gin.Context) {
	var req PeerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	configMutex.Lock()
	defer configMutex.Unlock()

	// Parse the public key
	pubKey, err := wgtypes.ParseKey(req.PublicKey)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid public key"})
		return
	}

	// Parse allowed IPs
	_, ipNet, err := net.ParseCIDR(req.AllowedIP)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid allowed IP"})
		return
	}

	// Build peer config
	peerConfig := wgtypes.PeerConfig{
		PublicKey:         pubKey,
		ReplaceAllowedIPs: true,
		AllowedIPs:        []net.IPNet{*ipNet},
	}

	// Add preshared key if provided
	if req.PresharedKey != "" {
		psk, err := wgtypes.ParseKey(req.PresharedKey)
		if err != nil {
			c.JSON(400, gin.H{"error": "invalid preshared key"})
			return
		}
		peerConfig.PresharedKey = &psk
	}

	// Apply configuration using wgctrl
	client, err := wgctrl.New()
	if err != nil {
		log.Printf("Failed to create wgctrl client: %v", err)
		c.JSON(500, gin.H{"error": "failed to configure WireGuard"})
		return
	}
	defer client.Close()

	err = client.ConfigureDevice(wgInterface, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peerConfig},
	})
	if err != nil {
		log.Printf("Failed to add peer: %v", err)
		c.JSON(500, gin.H{"error": "failed to add peer"})
		return
	}

	// Also update the config file for persistence
	if err := appendPeerToConfig(req); err != nil {
		log.Printf("Warning: Failed to persist peer to config: %v", err)
	}

	log.Printf("Added peer: %s with IP %s", req.PublicKey[:16]+"...", req.AllowedIP)
	c.JSON(201, gin.H{"message": "peer added"})
}

func removePeer(c *gin.Context) {
	publicKeyStr := c.Param("publicKey")

	configMutex.Lock()
	defer configMutex.Unlock()

	// URL decode and parse the public key
	decodedKey, err := base64.URLEncoding.DecodeString(publicKeyStr)
	if err != nil {
		// Try without URL encoding
		decodedKey, err = base64.StdEncoding.DecodeString(publicKeyStr)
		if err != nil {
			c.JSON(400, gin.H{"error": "invalid public key encoding"})
			return
		}
	}

	pubKey, err := wgtypes.NewKey(decodedKey)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid public key"})
		return
	}

	// Remove peer using wgctrl
	client, err := wgctrl.New()
	if err != nil {
		log.Printf("Failed to create wgctrl client: %v", err)
		c.JSON(500, gin.H{"error": "failed to configure WireGuard"})
		return
	}
	defer client.Close()

	err = client.ConfigureDevice(wgInterface, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey: pubKey,
				Remove:    true,
			},
		},
	})
	if err != nil {
		log.Printf("Failed to remove peer: %v", err)
		c.JSON(500, gin.H{"error": "failed to remove peer"})
		return
	}

	// Also update the config file
	if err := removePeerFromConfig(publicKeyStr); err != nil {
		log.Printf("Warning: Failed to remove peer from config: %v", err)
	}

	log.Printf("Removed peer: %s", publicKeyStr[:min(16, len(publicKeyStr))]+"...")
	c.JSON(200, gin.H{"message": "peer removed"})
}

func listPeers(c *gin.Context) {
	client, err := wgctrl.New()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to get WireGuard info"})
		return
	}
	defer client.Close()

	device, err := client.Device(wgInterface)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to get device info"})
		return
	}

	var peers []PeerStatus
	for _, peer := range device.Peers {
		allowedIPs := make([]string, len(peer.AllowedIPs))
		for i, ip := range peer.AllowedIPs {
			allowedIPs[i] = ip.String()
		}

		var lastHandshake *time.Time
		if !peer.LastHandshakeTime.IsZero() {
			lastHandshake = &peer.LastHandshakeTime
		}

		// Consider connected if handshake was within last 3 minutes
		connected := lastHandshake != nil && time.Since(*lastHandshake) < 3*time.Minute

		endpoint := ""
		if peer.Endpoint != nil {
			endpoint = peer.Endpoint.String()
		}

		peers = append(peers, PeerStatus{
			PublicKey:     peer.PublicKey.String(),
			AllowedIP:     strings.Join(allowedIPs, ", "),
			Endpoint:      endpoint,
			LastHandshake: lastHandshake,
			BytesReceived: peer.ReceiveBytes,
			BytesSent:     peer.TransmitBytes,
			Connected:     connected,
		})
	}

	c.JSON(200, peers)
}

func getPeerStatus(c *gin.Context) {
	publicKeyStr := c.Query("publicKey")
	if publicKeyStr == "" {
		c.JSON(400, gin.H{"error": "publicKey query parameter required"})
		return
	}

	client, err := wgctrl.New()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to get WireGuard info"})
		return
	}
	defer client.Close()

	device, err := client.Device(wgInterface)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to get device info"})
		return
	}

	for _, peer := range device.Peers {
		if peer.PublicKey.String() == publicKeyStr {
			allowedIPs := make([]string, len(peer.AllowedIPs))
			for i, ip := range peer.AllowedIPs {
				allowedIPs[i] = ip.String()
			}

		var lastHandshake *time.Time
		if !peer.LastHandshakeTime.IsZero() {
			lastHandshake = &peer.LastHandshakeTime
		}

		// Consider connected if handshake within last 30 seconds
		// WireGuard keepalive is typically 25s, so this gives a small buffer
		connected := lastHandshake != nil && time.Since(*lastHandshake) < 30*time.Second

		endpoint := ""
		if peer.Endpoint != nil {
			endpoint = peer.Endpoint.String()
		}

			c.JSON(200, PeerStatus{
				PublicKey:     peer.PublicKey.String(),
				AllowedIP:     strings.Join(allowedIPs, ", "),
				Endpoint:      endpoint,
				LastHandshake: lastHandshake,
				BytesReceived: peer.ReceiveBytes,
				BytesSent:     peer.TransmitBytes,
				Connected:     connected,
			})
			return
		}
	}

	c.JSON(404, gin.H{"error": "peer not found"})
}

func addNetwork(c *gin.Context) {
	var req NetworkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Add routing for the team's challenge network
	// This allows VPN clients in the team's subnet to reach challenge containers
	challengeBaseCIDR := os.Getenv("PTA_DOCKER_CHALL_BASE_CIDR")
	if challengeBaseCIDR == "" {
		challengeBaseCIDR = "172.70.0.0/16"
	}

	// The team's challenge network subnet (e.g., 172.70.X.0/24 for team X)
	teamChallengeSubnet := fmt.Sprintf("172.70.%d.0/24", req.TeamID)

	// Add iptables rule to allow forwarding from VPN subnet to challenge subnet
	cmd := exec.Command("iptables", "-A", "FORWARD",
		"-s", req.Subnet,
		"-d", teamChallengeSubnet,
		"-j", "ACCEPT")
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: Failed to add forward rule: %v", err)
	}

	// Add reverse rule
	cmd = exec.Command("iptables", "-A", "FORWARD",
		"-s", teamChallengeSubnet,
		"-d", req.Subnet,
		"-j", "ACCEPT")
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: Failed to add reverse forward rule: %v", err)
	}

	log.Printf("Added network rules for team %d: VPN %s <-> Challenge %s", req.TeamID, req.Subnet, teamChallengeSubnet)
	c.JSON(201, gin.H{"message": "network added"})
}

func connectToNetwork(c *gin.Context) {
	var req struct {
		NetworkName string `json:"networkName" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Connect to the Docker network using Docker socket
	cmd := exec.Command("sh", "-c", fmt.Sprintf(`
		NETWORK_ID=$(curl -sf --unix-socket /var/run/docker.sock http://localhost/networks 2>/dev/null | \
			jq -r '.[] | select(.Name == "%s") | .Id' || echo "")
		if [ -z "$NETWORK_ID" ]; then
			echo "Network not found"
			exit 1
		fi
		curl -sf --unix-socket /var/run/docker.sock \
			-X POST \
			-H "Content-Type: application/json" \
			-d '{"Container":"wireguard","EndpointConfig":{}}' \
			"http://localhost/networks/$NETWORK_ID/connect" 2>/dev/null
	`, req.NetworkName))
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to connect to network %s: %v, output: %s", req.NetworkName, err, string(output))
		c.JSON(500, gin.H{"error": "failed to connect to network", "details": string(output)})
		return
	}

	// Wait a moment for the interface to be added
	time.Sleep(500 * time.Millisecond)

	// Add NAT rules for the new interface
	cmd = exec.Command("sh", "-c", `
		for iface in $(ip link show | grep -E "^[0-9]+: eth[1-9]" | awk -F': ' '{print $2}' | cut -d'@' -f1); do
			iptables -t nat -C POSTROUTING -s 10.13.0.0/16 -o "$iface" -j MASQUERADE 2>/dev/null || \
			iptables -t nat -I POSTROUTING 1 -s 10.13.0.0/16 -o "$iface" -j MASQUERADE 2>/dev/null
		done
	`)
	
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: Failed to add NAT rules: %v", err)
	}

	log.Printf("Connected to network: %s and added NAT rules", req.NetworkName)
	c.JSON(200, gin.H{"message": "connected to network", "network": req.NetworkName})
}

func removeNetwork(c *gin.Context) {
	teamIDStr := c.Param("teamId")
	var teamID int
	fmt.Sscanf(teamIDStr, "%d", &teamID)

	vpnSubnet := fmt.Sprintf("10.13.%d.0/24", teamID)
	challengeSubnet := fmt.Sprintf("172.70.%d.0/24", teamID)

	// Remove iptables rules
	exec.Command("iptables", "-D", "FORWARD", "-s", vpnSubnet, "-d", challengeSubnet, "-j", "ACCEPT").Run()
	exec.Command("iptables", "-D", "FORWARD", "-s", challengeSubnet, "-d", vpnSubnet, "-j", "ACCEPT").Run()

	log.Printf("Removed network rules for team %d", teamID)
	c.JSON(200, gin.H{"message": "network removed"})
}

func getServerInfo(c *gin.Context) {
	// Read public key from file
	pubKeyBytes, err := os.ReadFile("/etc/wireguard/server_public.key")
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to read server public key"})
		return
	}

	c.JSON(200, gin.H{
		"publicKey":  strings.TrimSpace(string(pubKeyBytes)),
		"interface":  wgInterface,
		"listenPort": getEnv("WG_PORT", "51820"),
		"address":    getEnv("WG_ADDRESS", "10.13.0.1/16"),
	})
}

func getServerStatus(c *gin.Context) {
	client, err := wgctrl.New()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to get WireGuard info"})
		return
	}
	defer client.Close()

	device, err := client.Device(wgInterface)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to get device info"})
		return
	}

	connectedPeers := 0
	for _, peer := range device.Peers {
		if !peer.LastHandshakeTime.IsZero() && time.Since(peer.LastHandshakeTime) < 3*time.Minute {
			connectedPeers++
		}
	}

	c.JSON(200, gin.H{
		"interface":      device.Name,
		"publicKey":      device.PublicKey.String(),
		"listenPort":     device.ListenPort,
		"totalPeers":     len(device.Peers),
		"connectedPeers": connectedPeers,
	})
}

// Helper functions for config file management

func appendPeerToConfig(peer PeerRequest) error {
	configPath := fmt.Sprintf("/etc/wireguard/%s.conf", wgInterface)

	peerSection := fmt.Sprintf("\n[Peer]\nPublicKey = %s\n", peer.PublicKey)
	if peer.PresharedKey != "" {
		peerSection += fmt.Sprintf("PresharedKey = %s\n", peer.PresharedKey)
	}
	peerSection += fmt.Sprintf("AllowedIPs = %s\n", peer.AllowedIP)

	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(peerSection)
	return err
}

func removePeerFromConfig(publicKey string) error {
	configPath := fmt.Sprintf("/etc/wireguard/%s.conf", wgInterface)

	content, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	skipSection := false

	for i, line := range lines {
		if strings.TrimSpace(line) == "[Peer]" {
			// Check if next line has our public key
			if i+1 < len(lines) && strings.Contains(lines[i+1], publicKey) {
				skipSection = true
				continue
			}
		}

		if skipSection {
			if strings.HasPrefix(strings.TrimSpace(line), "[") && strings.TrimSpace(line) != "[Peer]" {
				skipSection = false
			} else if strings.TrimSpace(line) == "[Peer]" {
				skipSection = false
			} else {
				continue
			}
		}

		newLines = append(newLines, line)
	}

	return os.WriteFile(configPath, []byte(strings.Join(newLines, "\n")), 0600)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
