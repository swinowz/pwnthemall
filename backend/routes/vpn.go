package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/pwnthemall/pwnthemall/backend/controllers"
	"github.com/pwnthemall/pwnthemall/backend/middleware"
)

func RegisterVPNRoutes(router *gin.Engine) {
	// Public VPN routes (authenticated users)
	// Note: Caddy strips /api prefix, so routes are /vpn/...
	vpn := router.Group("/vpn", middleware.AuthRequired(false), middleware.CSRFProtection())
	{
		vpn.GET("/status", middleware.CheckPolicy("/vpn/status", "read"), controllers.GetVPNStatus)
		vpn.GET("/config", middleware.CheckPolicy("/vpn/config", "read"), controllers.GetVPNConfig)
		vpn.POST("/regenerate", middleware.CheckPolicy("/vpn/regenerate", "write"), middleware.RateLimit(5), controllers.RegenerateVPNKeys)
	}

	// Admin VPN routes
	adminVPN := router.Group("/admin/vpn", middleware.AuthRequired(false), middleware.CSRFProtection())
	{
		adminVPN.GET("/config", middleware.CheckPolicy("/admin/vpn/config", "read"), controllers.GetVPNConfigAdmin)
		adminVPN.PUT("/config", middleware.CheckPolicy("/admin/vpn/config", "write"), controllers.UpdateVPNConfigAdmin)
		adminVPN.POST("/regenerate-keys", middleware.CheckPolicy("/admin/vpn/regenerate-keys", "write"), controllers.RegenerateServerKeysAdmin)
		adminVPN.GET("/peers", middleware.CheckPolicy("/admin/vpn/peers", "read"), controllers.GetAllVPNPeersAdmin)
		adminVPN.DELETE("/peers/:id", middleware.CheckPolicy("/admin/vpn/peers", "write"), controllers.DeleteVPNPeerAdmin)
		adminVPN.GET("/teams", middleware.CheckPolicy("/admin/vpn/teams", "read"), controllers.GetTeamVPNsAdmin)
		adminVPN.POST("/sync", middleware.CheckPolicy("/admin/vpn/sync", "write"), controllers.SyncVPNPeersAdmin)
		adminVPN.GET("/stats", middleware.CheckPolicy("/admin/vpn/stats", "read"), controllers.GetVPNStatsAdmin)
	}
}
