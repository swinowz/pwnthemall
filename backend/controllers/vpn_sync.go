package controllers

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/pwnthemall/pwnthemall/backend/config"
	"github.com/pwnthemall/pwnthemall/backend/debug"
	"github.com/pwnthemall/pwnthemall/backend/models"
)

// SyncVPNServerKey syncs the VPN server's public key to the database
// Called by WireGuard container on startup to ensure key consistency
func SyncVPNServerKey(c *gin.Context) {
	// Verify internal secret
	secret := c.GetHeader("X-Internal-Secret")
	expectedSecret := os.Getenv("PTA_VPN_INTERNAL_SECRET")
	if expectedSecret == "" {
		expectedSecret = "vpn-internal-secret"
	}
	
	if secret != expectedSecret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		ServerPublicKey string `json:"serverPublicKey" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update server public key in database
	var vpnConfig models.VPNConfig
	if err := config.DB.First(&vpnConfig, 1).Error; err != nil {
		debug.Log("VPN config not found, creating new one")
		endpoint := os.Getenv("PTA_VPN_ENDPOINT")
		if endpoint == "" {
			endpoint = "localhost:51820"
		}
		vpnConfig = models.VPNConfig{
			Enabled:             true,
			ServerPublicKey:     req.ServerPublicKey,
			ServerEndpoint:      endpoint,
			ServerListenPort:    51820,
			ServerAddress:       "10.13.0.1/16",
			DNSServers:          "1.1.1.1, 8.8.8.8",
			MTU:                 1420,
			PersistentKeepalive: 25,
			RequiredForInstances: true,
		}
		if err := config.DB.Create(&vpnConfig).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create VPN config"})
			return
		}
	} else {
		vpnConfig.ServerPublicKey = req.ServerPublicKey
		if err := config.DB.Save(&vpnConfig).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update server key"})
			return
		}
	}

	debug.Log("VPN server public key synced: %s", req.ServerPublicKey)
	c.JSON(http.StatusOK, gin.H{"message": "key synced"})
}

// GetVPNPeers returns all VPN peers for WireGuard configuration
// Called by WireGuard container on startup to restore peer list
func GetVPNPeers(c *gin.Context) {
	// Verify internal secret
	secret := c.GetHeader("X-Internal-Secret")
	expectedSecret := os.Getenv("PTA_VPN_INTERNAL_SECRET")
	if expectedSecret == "" {
		expectedSecret = "vpn-internal-secret"
	}
	
	if secret != expectedSecret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var peers []models.VPNPeer
	if err := config.DB.Where("enabled = ?", true).Find(&peers).Error; err != nil {
		debug.Log("Failed to fetch VPN peers: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch peers"})
		return
	}

	// Format peers for WireGuard config
	type PeerConfig struct {
		PublicKey     string `json:"publicKey"`
		PresharedKey  string `json:"presharedKey"`
		AllowedIp     string `json:"allowedIp"`
	}

	var peerConfigs []PeerConfig
	for _, peer := range peers {
		peerConfigs = append(peerConfigs, PeerConfig{
			PublicKey:    peer.PublicKey,
			PresharedKey: peer.PresharedKey,
			AllowedIp:    peer.AllowedIP,
		})
	}

	c.JSON(http.StatusOK, peerConfigs)
}
