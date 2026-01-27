package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pwnthemall/pwnthemall/backend/config"
	"github.com/pwnthemall/pwnthemall/backend/debug"
	"github.com/pwnthemall/pwnthemall/backend/models"
	"github.com/pwnthemall/pwnthemall/backend/utils"
)

// GetVPNStatus returns the current VPN status for the user
func GetVPNStatus(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedError(c, "unauthorized")
		return
	}

	var user models.User
	if err := config.DB.Preload("Team").First(&user, userID).Error; err != nil {
		utils.NotFoundError(c, "user_not_found")
		return
	}

	// Get VPN config
	vpnConfig, err := utils.GetVPNConfig()
	if err != nil {
		utils.OKResponse(c, gin.H{
			"enabled":       false,
			"configured":    false,
			"hasPeer":       false,
			"connected":     false,
			"message":       "vpn_not_configured",
		})
		return
	}

	if !vpnConfig.Enabled {
		utils.OKResponse(c, gin.H{
			"enabled":       false,
			"configured":    true,
			"hasPeer":       false,
			"connected":     false,
			"message":       "vpn_disabled",
		})
		return
	}

	// Check if user has a team
	if user.TeamID == nil {
		utils.OKResponse(c, gin.H{
			"enabled":       true,
			"configured":    true,
			"hasPeer":       false,
			"connected":     false,
			"requiresTeam":  true,
			"message":       "team_required_for_vpn",
		})
		return
	}

	// Check if user has a VPN peer
	var peer models.VPNPeer
	hasPeer := config.DB.Where("user_id = ?", userID).First(&peer).Error == nil

	// Get connection status
	var status *models.VPNPeerStatus
	connected := false
	if hasPeer {
		status, err = utils.GetVPNPeerStatus(peer.PublicKey)
		if err != nil {
			debug.Log("Warning: Failed to get VPN peer status: %v", err)
		} else {
			connected = status.Connected
		}
	}

	// Get team VPN info
	var teamVPN models.TeamVPN
	config.DB.Where("team_id = ?", *user.TeamID).First(&teamVPN)

	response := gin.H{
		"enabled":             vpnConfig.Enabled,
		"configured":          true,
		"hasPeer":             hasPeer,
		"connected":           connected,
		"requiredForInstances": vpnConfig.RequiredForInstances,
		"serverEndpoint":      vpnConfig.ServerEndpoint,
	}

	if hasPeer {
		response["assignedIp"] = peer.AssignedIP
		response["publicKey"] = peer.PublicKey
		response["teamSubnet"] = teamVPN.Subnet
	}

	if status != nil && connected {
		response["lastHandshake"] = status.LastHandshake
		response["bytesReceived"] = status.BytesReceived
		response["bytesSent"] = status.BytesSent
	}

	utils.OKResponse(c, response)
}

// GetVPNConfig returns the VPN configuration for download
func GetVPNConfig(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedError(c, "unauthorized")
		return
	}

	var user models.User
	if err := config.DB.Preload("Team").First(&user, userID).Error; err != nil {
		utils.NotFoundError(c, "user_not_found")
		return
	}

	// Check if user has a team
	if user.TeamID == nil {
		utils.BadRequestError(c, "team_required_for_vpn")
		return
	}

	// Get VPN config
	vpnConfig, err := utils.GetVPNConfig()
	if err != nil || !vpnConfig.Enabled {
		utils.BadRequestError(c, "vpn_not_available")
		return
	}

	// Ensure user has a VPN peer
	peer, err := utils.EnsureUserVPNPeer(user.ID, *user.TeamID)
	if err != nil {
		debug.Log("Failed to ensure VPN peer: %v", err)
		utils.InternalServerError(c, "failed_to_create_vpn_config")
		return
	}

	// Get team VPN
	var teamVPN models.TeamVPN
	if err := config.DB.First(&teamVPN, peer.TeamVPNID).Error; err != nil {
		utils.InternalServerError(c, "failed_to_get_team_vpn")
		return
	}

	// Generate config
	configContent, err := utils.GenerateWireGuardClientConfig(peer, vpnConfig, &teamVPN)
	if err != nil {
		debug.Log("Failed to generate VPN config: %v", err)
		utils.InternalServerError(c, "failed_to_generate_config")
		return
	}

	// Return as downloadable file
	c.Header("Content-Disposition", "attachment; filename=wg-pwnthemall.conf")
	c.Header("Content-Type", "text/plain")
	c.String(http.StatusOK, configContent)
}

// RegenerateVPNKeys regenerates the user's VPN keys
func RegenerateVPNKeys(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedError(c, "unauthorized")
		return
	}

	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		utils.NotFoundError(c, "user_not_found")
		return
	}

	if user.TeamID == nil {
		utils.BadRequestError(c, "team_required_for_vpn")
		return
	}

	// Regenerate keys
	peer, err := utils.RegenerateUserVPNKeys(user.ID)
	if err != nil {
		debug.Log("Failed to regenerate VPN keys: %v", err)
		utils.InternalServerError(c, "failed_to_regenerate_keys")
		return
	}

	utils.OKResponse(c, gin.H{
		"message":    "keys_regenerated",
		"assignedIp": peer.AssignedIP,
		"publicKey":  peer.PublicKey,
	})
}

// Admin Controllers

// GetVPNConfigAdmin returns VPN config for admin
func GetVPNConfigAdmin(c *gin.Context) {
	vpnConfig, err := utils.EnsureVPNConfig()
	if err != nil {
		utils.InternalServerError(c, "failed_to_get_vpn_config")
		return
	}

	utils.OKResponse(c, gin.H{
		"id":                   vpnConfig.ID,
		"enabled":              vpnConfig.Enabled,
		"serverPublicKey":      vpnConfig.ServerPublicKey,
		"serverEndpoint":       vpnConfig.ServerEndpoint,
		"serverListenPort":     vpnConfig.ServerListenPort,
		"serverAddress":        vpnConfig.ServerAddress,
		"dnsServers":           vpnConfig.DNSServers,
		"allowedIpsTemplate":   vpnConfig.AllowedIPsTemplate,
		"mtu":                  vpnConfig.MTU,
		"persistentKeepalive":  vpnConfig.PersistentKeepalive,
		"requiredForInstances": vpnConfig.RequiredForInstances,
		"createdAt":            vpnConfig.CreatedAt,
		"updatedAt":            vpnConfig.UpdatedAt,
	})
}

// UpdateVPNConfigAdmin updates VPN config
func UpdateVPNConfigAdmin(c *gin.Context) {
	var input struct {
		Enabled              *bool   `json:"enabled"`
		ServerEndpoint       *string `json:"serverEndpoint"`
		ServerListenPort     *int    `json:"serverListenPort"`
		DNSServers           *string `json:"dnsServers"`
		AllowedIPsTemplate   *string `json:"allowedIpsTemplate"`
		MTU                  *int    `json:"mtu"`
		PersistentKeepalive  *int    `json:"persistentKeepalive"`
		RequiredForInstances *bool   `json:"requiredForInstances"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		utils.BadRequestError(c, "invalid_input")
		return
	}

	vpnConfig, err := utils.EnsureVPNConfig()
	if err != nil {
		utils.InternalServerError(c, "failed_to_get_vpn_config")
		return
	}

	if input.Enabled != nil {
		vpnConfig.Enabled = *input.Enabled
	}
	if input.ServerEndpoint != nil {
		vpnConfig.ServerEndpoint = *input.ServerEndpoint
	}
	if input.ServerListenPort != nil {
		vpnConfig.ServerListenPort = *input.ServerListenPort
	}
	if input.DNSServers != nil {
		vpnConfig.DNSServers = *input.DNSServers
	}
	if input.AllowedIPsTemplate != nil {
		vpnConfig.AllowedIPsTemplate = *input.AllowedIPsTemplate
	}
	if input.MTU != nil {
		vpnConfig.MTU = *input.MTU
	}
	if input.PersistentKeepalive != nil {
		vpnConfig.PersistentKeepalive = *input.PersistentKeepalive
	}
	if input.RequiredForInstances != nil {
		vpnConfig.RequiredForInstances = *input.RequiredForInstances
	}

	if err := config.DB.Save(vpnConfig).Error; err != nil {
		utils.InternalServerError(c, "failed_to_update_vpn_config")
		return
	}

	utils.OKResponse(c, gin.H{"message": "vpn_config_updated"})
}

// RegenerateServerKeysAdmin regenerates server VPN keys
func RegenerateServerKeysAdmin(c *gin.Context) {
	vpnConfig, err := utils.EnsureVPNConfig()
	if err != nil {
		utils.InternalServerError(c, "failed_to_get_vpn_config")
		return
	}

	privateKey, publicKey, err := utils.GenerateWireGuardKeyPair()
	if err != nil {
		utils.InternalServerError(c, "failed_to_generate_keys")
		return
	}

	vpnConfig.ServerPrivateKey = privateKey
	vpnConfig.ServerPublicKey = publicKey

	if err := config.DB.Save(vpnConfig).Error; err != nil {
		utils.InternalServerError(c, "failed_to_update_vpn_config")
		return
	}

	// All client configs are now invalid - notify VPN container to update
	// This would require all users to download new configs

	utils.OKResponse(c, gin.H{
		"message":         "server_keys_regenerated",
		"serverPublicKey": publicKey,
		"warning":         "all_client_configs_invalidated",
	})
}

// GetAllVPNPeersAdmin returns all VPN peers for admin
func GetAllVPNPeersAdmin(c *gin.Context) {
	var peers []models.VPNPeer
	if err := config.DB.Preload("User").Preload("TeamVPN.Team").Find(&peers).Error; err != nil {
		utils.InternalServerError(c, "failed_to_fetch_peers")
		return
	}

	// Get status for each peer
	type PeerWithStatus struct {
		models.VPNPeer
		Status    *models.VPNPeerStatus `json:"status"`
		TeamName  string                `json:"teamName"`
		Username  string                `json:"username"`
	}

	result := make([]PeerWithStatus, len(peers))
	for i, peer := range peers {
		status, _ := utils.GetVPNPeerStatus(peer.PublicKey)
		result[i] = PeerWithStatus{
			VPNPeer:  peer,
			Status:   status,
			TeamName: peer.TeamVPN.Team.Name,
			Username: peer.User.Username,
		}
	}

	utils.OKResponse(c, result)
}

// GetTeamVPNsAdmin returns all team VPN configurations
func GetTeamVPNsAdmin(c *gin.Context) {
	var teamVPNs []models.TeamVPN
	if err := config.DB.Preload("Team").Find(&teamVPNs).Error; err != nil {
		utils.InternalServerError(c, "failed_to_fetch_team_vpns")
		return
	}

	type TeamVPNWithDetails struct {
		models.TeamVPN
		TeamName   string `json:"teamName"`
		PeerCount  int64  `json:"peerCount"`
	}

	result := make([]TeamVPNWithDetails, len(teamVPNs))
	for i, tv := range teamVPNs {
		var peerCount int64
		config.DB.Model(&models.VPNPeer{}).Where("team_vpn_id = ?", tv.ID).Count(&peerCount)

		result[i] = TeamVPNWithDetails{
			TeamVPN:   tv,
			TeamName:  tv.Team.Name,
			PeerCount: peerCount,
		}
	}

	utils.OKResponse(c, result)
}

// DeleteVPNPeerAdmin deletes a VPN peer (admin only)
func DeleteVPNPeerAdmin(c *gin.Context) {
	peerID := c.Param("id")

	var peer models.VPNPeer
	if err := config.DB.First(&peer, peerID).Error; err != nil {
		utils.NotFoundError(c, "peer_not_found")
		return
	}

	// Notify VPN container
	if err := utils.NotifyVPNRemovePeer(&peer); err != nil {
		debug.Log("Warning: Failed to notify VPN container: %v", err)
	}

	if err := config.DB.Delete(&peer).Error; err != nil {
		utils.InternalServerError(c, "failed_to_delete_peer")
		return
	}

	utils.OKResponse(c, gin.H{"message": "peer_deleted"})
}

// SyncVPNPeersAdmin syncs all peers to the VPN container
func SyncVPNPeersAdmin(c *gin.Context) {
	if err := utils.SyncAllPeersToVPN(); err != nil {
		utils.InternalServerError(c, "failed_to_sync_peers")
		return
	}

	utils.OKResponse(c, gin.H{"message": "peers_synced"})
}

// GetVPNStatsAdmin returns VPN statistics
func GetVPNStatsAdmin(c *gin.Context) {
	var totalPeers int64
	config.DB.Model(&models.VPNPeer{}).Count(&totalPeers)

	var enabledPeers int64
	config.DB.Model(&models.VPNPeer{}).Where("enabled = ?", true).Count(&enabledPeers)

	var teamVPNCount int64
	config.DB.Model(&models.TeamVPN{}).Count(&teamVPNCount)

	// Get connected count (would require calling VPN container)
	connectedCount := 0
	var peers []models.VPNPeer
	if err := config.DB.Find(&peers).Error; err == nil {
		for _, peer := range peers {
			status, err := utils.GetVPNPeerStatus(peer.PublicKey)
			if err == nil && status.Connected {
				connectedCount++
			}
		}
	}

	utils.OKResponse(c, gin.H{
		"totalPeers":      totalPeers,
		"enabledPeers":    enabledPeers,
		"connectedPeers":  connectedCount,
		"teamNetworks":    teamVPNCount,
	})
}
