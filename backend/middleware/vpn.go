package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pwnthemall/pwnthemall/backend/config"
	"github.com/pwnthemall/pwnthemall/backend/debug"
	"github.com/pwnthemall/pwnthemall/backend/models"
	"github.com/pwnthemall/pwnthemall/backend/utils"
)

// VPNRequired ensures the user is connected to the VPN before accessing a resource
// This middleware should be placed after AuthRequired
func VPNRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if VPN is enabled and required for instances
		var vpnConfig models.VPNConfig
		if err := config.DB.First(&vpnConfig).Error; err != nil {
			// No VPN config means VPN is not set up, allow access
			c.Next()
			return
		}

		if !vpnConfig.Enabled || !vpnConfig.RequiredForInstances {
			// VPN not required, allow access
			c.Next()
			return
		}

		// Get user from context
		userI, exists := c.Get("user")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		user, ok := userI.(*models.User)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid_user_type"})
			return
		}

		// Admin users bypass VPN requirement
		if user.Role == "admin" {
			c.Next()
			return
		}

		// Check if user is connected to VPN
		connected, err := utils.IsUserConnectedToVPN(user.ID)
		if err != nil {
			debug.Log("Error checking VPN connection for user %d: %v", user.ID, err)
			// On error, we could either allow or deny - being permissive here
			// In production, you might want to deny instead
			c.Next()
			return
		}

		if !connected {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "vpn_required",
				"message": "You must be connected to the VPN to start challenge instances",
			})
			return
		}

		c.Next()
	}
}

// VPNRequiredSoft is a softer version that only warns but doesn't block
// Useful for gradual rollout or testing
func VPNRequiredSoft() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if VPN is enabled
		var vpnConfig models.VPNConfig
		if err := config.DB.First(&vpnConfig).Error; err != nil {
			c.Next()
			return
		}

		if !vpnConfig.Enabled {
			c.Next()
			return
		}

		// Get user from context
		userI, exists := c.Get("user")
		if !exists {
			c.Next()
			return
		}

		user, ok := userI.(*models.User)
		if !ok {
			c.Next()
			return
		}

		// Check if user is connected to VPN
		connected, _ := utils.IsUserConnectedToVPN(user.ID)

		// Set a flag in context for handlers to optionally use
		c.Set("vpn_connected", connected)

		c.Next()
	}
}

// CheckVPNOrTeamIP allows access if user is connected to VPN OR their IP is from the team's VPN subnet
// This is useful for allowing access from within the VPN network itself
func CheckVPNOrTeamIP() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if VPN is enabled and required
		var vpnConfig models.VPNConfig
		if err := config.DB.First(&vpnConfig).Error; err != nil {
			c.Next()
			return
		}

		if !vpnConfig.Enabled || !vpnConfig.RequiredForInstances {
			c.Next()
			return
		}

		// Get user from context
		userI, exists := c.Get("user")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		user, ok := userI.(*models.User)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid_user_type"})
			return
		}

		// Admin bypass
		if user.Role == "admin" {
			c.Next()
			return
		}

		// Check if client IP is from VPN network
		clientIP := getClientIP(c)
		if utils.ValidateVPNIP(clientIP) {
			c.Next()
			return
		}

		// Fall back to checking VPN connection status
		connected, err := utils.IsUserConnectedToVPN(user.ID)
		if err != nil {
			debug.Log("Error checking VPN connection for user %d: %v", user.ID, err)
			c.Next()
			return
		}

		if !connected {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "vpn_required",
				"message": "You must be connected to the VPN to access this resource",
			})
			return
		}

		c.Next()
	}
}
