package utils

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pwnthemall/pwnthemall/backend/config"
	"github.com/pwnthemall/pwnthemall/backend/debug"
	"github.com/pwnthemall/pwnthemall/backend/models"
	"golang.org/x/crypto/curve25519"
)

const (
	VPNBaseSubnet    = "10.13.0.0/16"   // Base VPN subnet
	VPNServerAddress = "10.13.0.1/16"   // Server address
	VPNTeamSubnetBase = "10.13"          // Team subnets: 10.13.X.0/24
)

// GenerateWireGuardKeyPair generates a WireGuard private/public key pair
func GenerateWireGuardKeyPair() (privateKey, publicKey string, err error) {
	// Generate private key (32 random bytes)
	var private [32]byte
	if _, err := rand.Read(private[:]); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Clamp the private key per WireGuard spec
	private[0] &= 248
	private[31] &= 127
	private[31] |= 64

	// Derive public key
	var public [32]byte
	curve25519.ScalarBaseMult(&public, &private)

	privateKey = base64.StdEncoding.EncodeToString(private[:])
	publicKey = base64.StdEncoding.EncodeToString(public[:])

	return privateKey, publicKey, nil
}

// GeneratePresharedKey generates a WireGuard preshared key
func GeneratePresharedKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("failed to generate preshared key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// GetTeamVPNSubnet returns the VPN subnet for a team
// Team 1 gets 10.13.1.0/24, Team 2 gets 10.13.2.0/24, etc.
func GetTeamVPNSubnet(teamID uint) (subnet, gateway string, err error) {
	if teamID < 1 || teamID > 254 {
		return "", "", fmt.Errorf("team ID must be between 1 and 254, got %d", teamID)
	}

	subnet = fmt.Sprintf("%s.%d.0/24", VPNTeamSubnetBase, teamID)
	gateway = fmt.Sprintf("%s.%d.1", VPNTeamSubnetBase, teamID)

	return subnet, gateway, nil
}

// GetNextPeerIP returns the next available IP in the team's VPN subnet
func GetNextPeerIP(teamVPN *models.TeamVPN) (string, error) {
	if teamVPN.NextPeerIP > 254 {
		return "", fmt.Errorf("no more IP addresses available in team subnet")
	}

	// Parse the subnet to get the base IP (e.g., "10.13.1.0/24" -> "10.13.1.0")
	baseParts := strings.Split(teamVPN.Subnet, "/")[0]
	ipParts := strings.Split(baseParts, ".")
	if len(ipParts) < 3 {
		return "", fmt.Errorf("invalid subnet format: %s", teamVPN.Subnet)
	}

	ip := fmt.Sprintf("%s.%s.%s.%d", ipParts[0], ipParts[1], ipParts[2], teamVPN.NextPeerIP)
	return ip, nil
}

// GenerateWireGuardClientConfig generates a WireGuard client configuration file
func GenerateWireGuardClientConfig(peer *models.VPNPeer, vpnConfig *models.VPNConfig, teamVPN *models.TeamVPN) (string, error) {
	// Get the challenge network subnet for routing
	challengeSubnet := os.Getenv("PTA_DOCKER_CHALL_BASE_CIDR")
	if challengeSubnet == "" {
		challengeSubnet = "172.70.0.0/16"
	}

	// Build allowed IPs - include VPN gateway, team subnet, and challenge network
	allowedIPs := []string{
		"10.13.0.0/24",     // VPN gateway subnet (for connectivity/status checks)
		teamVPN.Subnet,     // Team's VPN subnet
		challengeSubnet,    // Challenge containers network
	}

	// Add any custom allowed IPs from config
	if vpnConfig.AllowedIPsTemplate != "" {
		customIPs := strings.Split(vpnConfig.AllowedIPsTemplate, ",")
		for _, ip := range customIPs {
			ip = strings.TrimSpace(ip)
			if ip != "" && !contains(allowedIPs, ip) {
				allowedIPs = append(allowedIPs, ip)
			}
		}
	}

	dns := vpnConfig.DNSServers
	if dns == "" {
		dns = "1.1.1.1, 8.8.8.8"
	}

	mtu := vpnConfig.MTU
	if mtu == 0 {
		mtu = 1420
	}

	keepalive := vpnConfig.PersistentKeepalive
	if keepalive == 0 {
		keepalive = 25
	}

	config := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32
DNS = %s
MTU = %d

[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = %s
Endpoint = %s
PersistentKeepalive = %d
`,
		peer.PrivateKey,
		peer.AssignedIP,
		dns,
		mtu,
		vpnConfig.ServerPublicKey,
		peer.PresharedKey,
		strings.Join(allowedIPs, ", "),
		vpnConfig.ServerEndpoint,
		keepalive,
	)

	return config, nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// EnsureTeamVPN creates or returns existing TeamVPN configuration for a team
func EnsureTeamVPN(teamID uint) (*models.TeamVPN, error) {
	var teamVPN models.TeamVPN

	// Try to find existing
	if err := config.DB.Where("team_id = ?", teamID).First(&teamVPN).Error; err == nil {
		return &teamVPN, nil
	}

	// Create new TeamVPN
	subnet, gateway, err := GetTeamVPNSubnet(teamID)
	if err != nil {
		return nil, err
	}

	teamVPN = models.TeamVPN{
		TeamID:     teamID,
		Subnet:     subnet,
		Gateway:    gateway,
		NextPeerIP: 2, // Start from .2 (.1 is gateway)
		Enabled:    true,
	}

	if err := config.DB.Create(&teamVPN).Error; err != nil {
		return nil, fmt.Errorf("failed to create team VPN: %w", err)
	}

	// Notify VPN container to create the network
	if err := NotifyVPNAddTeamNetwork(teamID, subnet, gateway); err != nil {
		debug.Log("Warning: Failed to notify VPN container about new team network: %v", err)
	}

	return &teamVPN, nil
}

// EnsureUserVPNPeer creates or returns existing VPN peer for a user
func EnsureUserVPNPeer(userID, teamID uint) (*models.VPNPeer, error) {
	var peer models.VPNPeer

	// Try to find existing peer for this user
	if err := config.DB.Where("user_id = ?", userID).First(&peer).Error; err == nil {
		// Check if the team matches
		var teamVPN models.TeamVPN
		if err := config.DB.First(&teamVPN, peer.TeamVPNID).Error; err == nil {
			if teamVPN.TeamID == teamID {
				return &peer, nil
			}
			// User changed teams - delete old peer and create new one
			if err := DeleteUserVPNPeer(userID); err != nil {
				debug.Log("Warning: Failed to delete old VPN peer: %v", err)
			}
		}
	}

	// Ensure team VPN exists
	teamVPN, err := EnsureTeamVPN(teamID)
	if err != nil {
		return nil, err
	}

	// Generate keys
	privateKey, publicKey, err := GenerateWireGuardKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	presharedKey, err := GeneratePresharedKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate preshared key: %w", err)
	}

	// Get next available IP
	assignedIP, err := GetNextPeerIP(teamVPN)
	if err != nil {
		return nil, err
	}

	peer = models.VPNPeer{
		UserID:       userID,
		TeamVPNID:    teamVPN.ID,
		PublicKey:    publicKey,
		PrivateKey:   privateKey,
		PresharedKey: presharedKey,
		AllowedIP:    assignedIP + "/32",
		AssignedIP:   assignedIP,
		Enabled:      true,
	}

	if err := config.DB.Create(&peer).Error; err != nil {
		return nil, fmt.Errorf("failed to create VPN peer: %w", err)
	}

	// Update next peer IP
	teamVPN.NextPeerIP++
	if err := config.DB.Save(teamVPN).Error; err != nil {
		debug.Log("Warning: Failed to update next peer IP: %v", err)
	}

	// Notify VPN container to add the peer
	if err := NotifyVPNAddPeer(&peer); err != nil {
		debug.Log("Warning: Failed to notify VPN container about new peer: %v", err)
	}

	return &peer, nil
}

// DeleteUserVPNPeer deletes a user's VPN peer configuration
func DeleteUserVPNPeer(userID uint) error {
	var peer models.VPNPeer
	if err := config.DB.Where("user_id = ?", userID).First(&peer).Error; err != nil {
		return nil // No peer to delete
	}

	// Notify VPN container to remove the peer
	if err := NotifyVPNRemovePeer(&peer); err != nil {
		debug.Log("Warning: Failed to notify VPN container about peer removal: %v", err)
	}

	return config.DB.Delete(&peer).Error
}

// RegenerateUserVPNKeys regenerates VPN keys for a user
func RegenerateUserVPNKeys(userID uint) (*models.VPNPeer, error) {
	var peer models.VPNPeer
	if err := config.DB.Where("user_id = ?", userID).First(&peer).Error; err != nil {
		return nil, fmt.Errorf("VPN peer not found")
	}

	// Remove old peer from VPN container
	if err := NotifyVPNRemovePeer(&peer); err != nil {
		debug.Log("Warning: Failed to notify VPN container about peer removal: %v", err)
	}

	// Generate new keys
	privateKey, publicKey, err := GenerateWireGuardKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	presharedKey, err := GeneratePresharedKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate preshared key: %w", err)
	}

	peer.PublicKey = publicKey
	peer.PrivateKey = privateKey
	peer.PresharedKey = presharedKey

	if err := config.DB.Save(&peer).Error; err != nil {
		return nil, fmt.Errorf("failed to update VPN peer: %w", err)
	}

	// Add updated peer to VPN container
	if err := NotifyVPNAddPeer(&peer); err != nil {
		debug.Log("Warning: Failed to notify VPN container about updated peer: %v", err)
	}

	return &peer, nil
}

// VPN Container Communication

type vpnPeerRequest struct {
	PublicKey    string `json:"publicKey"`
	PresharedKey string `json:"presharedKey"`
	AllowedIP    string `json:"allowedIp"`
	TeamID       uint   `json:"teamId"`
}

type vpnTeamNetworkRequest struct {
	TeamID  uint   `json:"teamId"`
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway"`
}

func getVPNContainerURL() string {
	url := os.Getenv("PTA_VPN_CONTAINER_URL")
	if url == "" {
		url = "http://wireguard:8080"
	}
	return url
}

// NotifyVPNAddPeer notifies the VPN container to add a new peer
func NotifyVPNAddPeer(peer *models.VPNPeer) error {
	// Get team ID from TeamVPN
	var teamVPN models.TeamVPN
	if err := config.DB.First(&teamVPN, peer.TeamVPNID).Error; err != nil {
		return fmt.Errorf("failed to get team VPN: %w", err)
	}

	req := vpnPeerRequest{
		PublicKey:    peer.PublicKey,
		PresharedKey: peer.PresharedKey,
		AllowedIP:    peer.AllowedIP,
		TeamID:       teamVPN.TeamID,
	}

	data, _ := json.Marshal(req)
	resp, err := http.Post(getVPNContainerURL()+"/api/peers", "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to contact VPN container: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("VPN container returned status %d", resp.StatusCode)
	}

	return nil
}

// NotifyVPNRemovePeer notifies the VPN container to remove a peer
func NotifyVPNRemovePeer(peer *models.VPNPeer) error {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("DELETE", getVPNContainerURL()+"/api/peers/"+peer.PublicKey, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to contact VPN container: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("VPN container returned status %d", resp.StatusCode)
	}

	return nil
}

// NotifyVPNAddTeamNetwork notifies the VPN container to add routing for a team network
func NotifyVPNAddTeamNetwork(teamID uint, subnet, gateway string) error {
	req := vpnTeamNetworkRequest{
		TeamID:  teamID,
		Subnet:  subnet,
		Gateway: gateway,
	}

	data, _ := json.Marshal(req)
	resp, err := http.Post(getVPNContainerURL()+"/api/networks", "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to contact VPN container: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("VPN container returned status %d", resp.StatusCode)
	}

	return nil
}

// GetVPNPeerStatus gets the real-time status of a VPN peer from the VPN container
func GetVPNPeerStatus(publicKey string) (*models.VPNPeerStatus, error) {
	resp, err := http.Get(getVPNContainerURL() + "/api/peers/" + publicKey + "/status")
	if err != nil {
		return nil, fmt.Errorf("failed to contact VPN container: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &models.VPNPeerStatus{Connected: false}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("VPN container returned status %d", resp.StatusCode)
	}

	var status models.VPNPeerStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &status, nil
}

// IsUserConnectedToVPN checks if a user is currently connected to the VPN
func IsUserConnectedToVPN(userID uint) (bool, error) {
	var peer models.VPNPeer
	if err := config.DB.Where("user_id = ?", userID).First(&peer).Error; err != nil {
		return false, nil // No peer means not connected
	}

	status, err := GetVPNPeerStatus(peer.PublicKey)
	if err != nil {
		debug.Log("Warning: Failed to get VPN peer status: %v", err)
		return false, nil
	}

	return status.Connected, nil
}

// IsTeamMemberConnectedToVPN checks if any member of a team is connected to VPN
func IsTeamMemberConnectedToVPN(teamID uint) (bool, error) {
	var teamVPN models.TeamVPN
	if err := config.DB.Where("team_id = ?", teamID).First(&teamVPN).Error; err != nil {
		return false, nil
	}

	var peers []models.VPNPeer
	if err := config.DB.Where("team_vpn_id = ?", teamVPN.ID).Find(&peers).Error; err != nil {
		return false, nil
	}

	for _, peer := range peers {
		status, err := GetVPNPeerStatus(peer.PublicKey)
		if err != nil {
			continue
		}
		if status.Connected {
			return true, nil
		}
	}

	return false, nil
}

// SyncAllPeersToVPN syncs all VPN peers to the VPN container (useful on startup)
func SyncAllPeersToVPN() error {
	var peers []models.VPNPeer
	if err := config.DB.Find(&peers).Error; err != nil {
		return fmt.Errorf("failed to fetch peers: %w", err)
	}

	for _, peer := range peers {
		if err := NotifyVPNAddPeer(&peer); err != nil {
			debug.Log("Warning: Failed to sync peer %d to VPN: %v", peer.ID, err)
		}
	}

	return nil
}

// ValidateVPNIP checks if an IP address is within a valid VPN range
func ValidateVPNIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	_, vpnNet, _ := net.ParseCIDR(VPNBaseSubnet)
	return vpnNet.Contains(parsedIP)
}

// GetVPNConfig gets the current VPN configuration from the database
func GetVPNConfig() (*models.VPNConfig, error) {
	var vpnConfig models.VPNConfig
	if err := config.DB.First(&vpnConfig).Error; err != nil {
		return nil, err
	}
	return &vpnConfig, nil
}

// EnsureVPNConfig ensures VPN config exists, creates default if not
func EnsureVPNConfig() (*models.VPNConfig, error) {
	var vpnConfig models.VPNConfig
	if err := config.DB.First(&vpnConfig).Error; err == nil {
		return &vpnConfig, nil
	}

	// Generate server keys
	privateKey, publicKey, err := GenerateWireGuardKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate server keys: %w", err)
	}

	// Get endpoint from environment or use default
	endpoint := os.Getenv("PTA_VPN_ENDPOINT")
	if endpoint == "" {
		domain := os.Getenv("PTA_PUBLIC_DOMAIN")
		if domain != "" {
			endpoint = domain + ":51820"
		} else {
			endpoint = "localhost:51820"
		}
	}

	vpnConfig = models.VPNConfig{
		Enabled:             false, // Disabled by default
		ServerPrivateKey:    privateKey,
		ServerPublicKey:     publicKey,
		ServerEndpoint:      endpoint,
		ServerListenPort:    51820,
		ServerAddress:       VPNServerAddress,
		DNSServers:          "1.1.1.1, 8.8.8.8",
		AllowedIPsTemplate:  "",
		MTU:                 1420,
		PersistentKeepalive: 25,
		RequiredForInstances: true,
	}

	if err := config.DB.Create(&vpnConfig).Error; err != nil {
		return nil, fmt.Errorf("failed to create VPN config: %w", err)
	}

	return &vpnConfig, nil
}
