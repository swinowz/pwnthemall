package models

import (
	"time"
)

// VPNConfig stores global VPN configuration
type VPNConfig struct {
	ID                 uint   `gorm:"primaryKey" json:"id"`
	Enabled            bool   `json:"enabled" gorm:"default:false"`
	ServerPublicKey    string `json:"serverPublicKey"`
	ServerPrivateKey   string `json:"-"` // Never expose private key in JSON
	ServerEndpoint     string `json:"serverEndpoint"`     // e.g., "vpn.example.com:51820"
	ServerListenPort   int    `json:"serverListenPort" gorm:"default:51820"`
	ServerAddress      string `json:"serverAddress"`      // e.g., "10.13.0.1/16"
	DNSServers         string `json:"dnsServers"`         // e.g., "1.1.1.1,8.8.8.8"
	AllowedIPsTemplate string `json:"allowedIpsTemplate"` // e.g., "10.13.0.0/16,172.70.0.0/16"
	MTU                int    `json:"mtu" gorm:"default:1420"`
	PersistentKeepalive int    `json:"persistentKeepalive" gorm:"default:25"`
	RequiredForInstances bool  `json:"requiredForInstances" gorm:"default:true"` // If true, VPN is required to start instances
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

// TeamVPN stores VPN configuration for each team
type TeamVPN struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	TeamID        uint      `gorm:"uniqueIndex" json:"teamId"`
	Team          Team      `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"team,omitempty"`
	Subnet        string    `json:"subnet"`        // e.g., "10.13.5.0/24" for team 5
	Gateway       string    `json:"gateway"`       // e.g., "10.13.5.1"
	NextPeerIP    int       `json:"nextPeerIp" gorm:"default:2"` // Next available IP in subnet (starts at 2)
	Enabled       bool      `json:"enabled" gorm:"default:true"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// VPNPeer stores individual user VPN peer configuration
type VPNPeer struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	UserID        uint      `gorm:"index" json:"userId"`
	User          User      `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"user,omitempty"`
	TeamVPNID     uint      `gorm:"index" json:"teamVpnId"`
	TeamVPN       TeamVPN   `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"teamVpn,omitempty"`
	PublicKey     string    `json:"publicKey"`
	PrivateKey    string    `json:"-"` // Never expose private key in JSON
	PresharedKey  string    `json:"-"` // Optional additional security
	AllowedIP     string    `json:"allowedIp"`     // e.g., "10.13.5.2/32"
	AssignedIP    string    `json:"assignedIp"`    // e.g., "10.13.5.2"
	LastHandshake *time.Time `json:"lastHandshake"` // Last successful handshake
	BytesReceived int64     `json:"bytesReceived"`
	BytesSent     int64     `json:"bytesSent"`
	Enabled       bool      `json:"enabled" gorm:"default:true"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// VPNPeerStatus represents the real-time status of a VPN peer
type VPNPeerStatus struct {
	PublicKey       string     `json:"publicKey"`
	AllowedIP       string     `json:"allowedIp"`
	Endpoint        string     `json:"endpoint"`
	LastHandshake   *time.Time `json:"lastHandshake"`
	BytesReceived   int64      `json:"bytesReceived"`
	BytesSent       int64      `json:"bytesSent"`
	Connected       bool       `json:"connected"`
}
