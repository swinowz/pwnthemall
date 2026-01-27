import { useState, useEffect, useCallback } from "react";
import { useRouter } from "next/router";
import { useAuth } from "@/context/AuthContext";
import { useSiteConfig } from "@/context/SiteConfigContext";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import axios from "@/lib/axios";
import { useLanguage } from "@/context/LanguageContext";
import { toast } from "sonner";
import Head from "next/head";
import {
  RefreshCw,
  Shield,
  Users,
  Network,
  Settings,
  Trash2,
  Key,
  Activity,
  Server,
  Wifi,
  WifiOff,
} from "lucide-react";

interface VPNConfig {
  id: number;
  enabled: boolean;
  serverPublicKey: string;
  serverEndpoint: string;
  serverListenPort: number;
  serverAddress: string;
  dnsServers: string;
  allowedIpsTemplate: string;
  mtu: number;
  persistentKeepalive: number;
  requiredForInstances: boolean;
}

interface VPNPeer {
  id: number;
  userId: number;
  publicKey: string;
  assignedIp: string;
  enabled: boolean;
  createdAt: string;
  username: string;
  teamName: string;
  status?: {
    connected: boolean;
    lastHandshake?: string;
    bytesReceived?: number;
    bytesSent?: number;
  };
}

interface TeamVPN {
  id: number;
  teamId: number;
  subnet: string;
  gateway: string;
  enabled: boolean;
  teamName: string;
  peerCount: number;
}

interface VPNStats {
  totalPeers: number;
  enabledPeers: number;
  connectedPeers: number;
  teamNetworks: number;
}

export default function AdminVPNPage() {
  const { t } = useLanguage();
  const { getSiteName } = useSiteConfig();
  const router = useRouter();
  const { loggedIn, authChecked } = useAuth();

  const [config, setConfig] = useState<VPNConfig | null>(null);
  const [peers, setPeers] = useState<VPNPeer[]>([]);
  const [teamVPNs, setTeamVPNs] = useState<TeamVPN[]>([]);
  const [stats, setStats] = useState<VPNStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [regeneratingKeys, setRegeneratingKeys] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [peerToDelete, setPeerToDelete] = useState<VPNPeer | null>(null);

  const fetchData = useCallback(async () => {
    try {
      const [configRes, peersRes, teamsRes, statsRes] = await Promise.all([
        axios.get("/api/admin/vpn/config"),
        axios.get("/api/admin/vpn/peers"),
        axios.get("/api/admin/vpn/teams"),
        axios.get("/api/admin/vpn/stats"),
      ]);
      setConfig(configRes.data);
      setPeers(peersRes.data || []);
      setTeamVPNs(teamsRes.data || []);
      setStats(statsRes.data);
    } catch (err) {
      console.error("Failed to fetch VPN data:", err);
      toast.error("Failed to load VPN data", { className: "bg-red-600 text-white" });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (authChecked && !loggedIn) {
      router.replace("/login");
    } else if (authChecked && loggedIn) {
      fetchData();
    }
  }, [authChecked, loggedIn, router, fetchData]);

  const handleSaveConfig = async () => {
    if (!config) return;
    setSaving(true);
    try {
      await axios.put("/api/admin/vpn/config", config);
      toast.success("VPN configuration saved");
    } catch (err) {
      toast.error("Failed to save configuration", { className: "bg-red-600 text-white" });
    } finally {
      setSaving(false);
    }
  };

  const handleRegenerateServerKeys = async () => {
    if (!confirm("This will invalidate ALL existing client configurations. Are you sure?")) {
      return;
    }
    setRegeneratingKeys(true);
    try {
      const res = await axios.post("/api/admin/vpn/regenerate-keys");
      toast.success("Server keys regenerated. All clients need new configurations.");
      setConfig(prev => prev ? { ...prev, serverPublicKey: res.data.serverPublicKey } : null);
    } catch (err) {
      toast.error("Failed to regenerate keys", { className: "bg-red-600 text-white" });
    } finally {
      setRegeneratingKeys(false);
    }
  };

  const handleSyncPeers = async () => {
    setSyncing(true);
    try {
      await axios.post("/api/admin/vpn/sync");
      toast.success("Peers synced to VPN container");
      fetchData();
    } catch (err) {
      toast.error("Failed to sync peers", { className: "bg-red-600 text-white" });
    } finally {
      setSyncing(false);
    }
  };

  const handleDeletePeer = async () => {
    if (!peerToDelete) return;
    try {
      await axios.delete(`/api/admin/vpn/peers/${peerToDelete.id}`);
      toast.success("Peer deleted");
      setPeers(prev => prev.filter(p => p.id !== peerToDelete.id));
      setDeleteDialogOpen(false);
      setPeerToDelete(null);
    } catch (err) {
      toast.error("Failed to delete peer", { className: "bg-red-600 text-white" });
    }
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  };

  if (!authChecked || !loggedIn) return null;

  return (
    <>
      <Head>
        <title>{t("vpn.management") || "VPN Management"} - {getSiteName()}</title>
      </Head>
      <div className="container mx-auto py-8 px-4">
        <div className="mb-8 flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold flex items-center gap-2">
              <Shield className="h-8 w-8" />
              {t("vpn.management") || "VPN Management"}
            </h1>
            <p className="text-muted-foreground mt-2">
              {t("vpn.admin_description") || "Manage WireGuard VPN configuration and peers"}
            </p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={fetchData} disabled={loading}>
              <RefreshCw className={`mr-2 h-4 w-4 ${loading ? "animate-spin" : ""}`} />
              Refresh
            </Button>
            <Button variant="outline" onClick={handleSyncPeers} disabled={syncing}>
              <Activity className={`mr-2 h-4 w-4 ${syncing ? "animate-spin" : ""}`} />
              Sync Peers
            </Button>
          </div>
        </div>

        {/* Stats Cards */}
        {stats && (
          <div className="grid gap-4 md:grid-cols-4 mb-8">
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Total Peers</CardTitle>
                <Users className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{stats.totalPeers}</div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Connected</CardTitle>
                <Wifi className="h-4 w-4 text-green-500" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{stats.connectedPeers}</div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Team Networks</CardTitle>
                <Network className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{stats.teamNetworks}</div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Status</CardTitle>
                <Server className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <Badge variant={config?.enabled ? "default" : "secondary"}>
                  {config?.enabled ? "Enabled" : "Disabled"}
                </Badge>
              </CardContent>
            </Card>
          </div>
        )}

        {loading ? (
          <Card>
            <CardContent className="py-8 flex items-center justify-center">
              <RefreshCw className="h-6 w-6 animate-spin" />
            </CardContent>
          </Card>
        ) : (
          <Tabs defaultValue="config" className="space-y-4">
            <TabsList>
              <TabsTrigger value="config">
                <Settings className="mr-2 h-4 w-4" />
                Configuration
              </TabsTrigger>
              <TabsTrigger value="peers">
                <Users className="mr-2 h-4 w-4" />
                Peers ({peers.length})
              </TabsTrigger>
              <TabsTrigger value="networks">
                <Network className="mr-2 h-4 w-4" />
                Team Networks ({teamVPNs.length})
              </TabsTrigger>
            </TabsList>

            <TabsContent value="config">
              <Card>
                <CardHeader>
                  <CardTitle>VPN Configuration</CardTitle>
                  <CardDescription>
                    Configure the WireGuard VPN server settings
                  </CardDescription>
                </CardHeader>
                <CardContent className="space-y-6">
                  {config && (
                    <>
                      <div className="flex items-center justify-between">
                        <div className="space-y-0.5">
                          <Label>VPN Enabled</Label>
                          <p className="text-sm text-muted-foreground">
                            Enable or disable VPN access for users
                          </p>
                        </div>
                        <Switch
                          checked={config.enabled}
                          onCheckedChange={(checked) =>
                            setConfig({ ...config, enabled: checked })
                          }
                        />
                      </div>

                      <div className="flex items-center justify-between">
                        <div className="space-y-0.5">
                          <Label>Required for Instances</Label>
                          <p className="text-sm text-muted-foreground">
                            Require VPN connection to start challenge instances
                          </p>
                        </div>
                        <Switch
                          checked={config.requiredForInstances}
                          onCheckedChange={(checked) =>
                            setConfig({ ...config, requiredForInstances: checked })
                          }
                        />
                      </div>

                      <div className="grid gap-4 md:grid-cols-2">
                        <div className="space-y-2">
                          <Label>Server Endpoint</Label>
                          <Input
                            value={config.serverEndpoint}
                            onChange={(e) =>
                              setConfig({ ...config, serverEndpoint: e.target.value })
                            }
                            placeholder="vpn.example.com:51820"
                          />
                        </div>
                        <div className="space-y-2">
                          <Label>Listen Port</Label>
                          <Input
                            type="number"
                            value={config.serverListenPort}
                            onChange={(e) =>
                              setConfig({ ...config, serverListenPort: parseInt(e.target.value) })
                            }
                          />
                        </div>
                        <div className="space-y-2">
                          <Label>Server Address</Label>
                          <Input
                            value={config.serverAddress}
                            onChange={(e) =>
                              setConfig({ ...config, serverAddress: e.target.value })
                            }
                            placeholder="10.13.0.1/16"
                          />
                        </div>
                        <div className="space-y-2">
                          <Label>DNS Servers</Label>
                          <Input
                            value={config.dnsServers}
                            onChange={(e) =>
                              setConfig({ ...config, dnsServers: e.target.value })
                            }
                            placeholder="1.1.1.1, 8.8.8.8"
                          />
                        </div>
                        <div className="space-y-2">
                          <Label>MTU</Label>
                          <Input
                            type="number"
                            value={config.mtu}
                            onChange={(e) =>
                              setConfig({ ...config, mtu: parseInt(e.target.value) })
                            }
                          />
                        </div>
                        <div className="space-y-2">
                          <Label>Persistent Keepalive</Label>
                          <Input
                            type="number"
                            value={config.persistentKeepalive}
                            onChange={(e) =>
                              setConfig({ ...config, persistentKeepalive: parseInt(e.target.value) })
                            }
                          />
                        </div>
                      </div>

                      <div className="space-y-2">
                        <Label>Additional Allowed IPs</Label>
                        <Input
                          value={config.allowedIpsTemplate}
                          onChange={(e) =>
                            setConfig({ ...config, allowedIpsTemplate: e.target.value })
                          }
                          placeholder="Comma-separated CIDRs"
                        />
                        <p className="text-sm text-muted-foreground">
                          Additional networks clients should route through VPN
                        </p>
                      </div>

                      <div className="p-4 bg-muted rounded-lg">
                        <Label className="text-sm text-muted-foreground">Server Public Key</Label>
                        <code className="block mt-1 text-xs break-all">{config.serverPublicKey}</code>
                      </div>

                      <div className="flex gap-4">
                        <Button onClick={handleSaveConfig} disabled={saving}>
                          {saving && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
                          Save Configuration
                        </Button>
                        <Button
                          variant="destructive"
                          onClick={handleRegenerateServerKeys}
                          disabled={regeneratingKeys}
                        >
                          {regeneratingKeys ? (
                            <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
                          ) : (
                            <Key className="mr-2 h-4 w-4" />
                          )}
                          Regenerate Server Keys
                        </Button>
                      </div>
                    </>
                  )}
                </CardContent>
              </Card>
            </TabsContent>

            <TabsContent value="peers">
              <Card>
                <CardHeader>
                  <CardTitle>VPN Peers</CardTitle>
                  <CardDescription>
                    Manage individual user VPN connections
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>User</TableHead>
                        <TableHead>Team</TableHead>
                        <TableHead>Assigned IP</TableHead>
                        <TableHead>Status</TableHead>
                        <TableHead>Data</TableHead>
                        <TableHead>Created</TableHead>
                        <TableHead></TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {peers.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={7} className="text-center text-muted-foreground">
                            No VPN peers configured
                          </TableCell>
                        </TableRow>
                      ) : (
                        peers.map((peer) => (
                          <TableRow key={peer.id}>
                            <TableCell className="font-medium">{peer.username}</TableCell>
                            <TableCell>{peer.teamName}</TableCell>
                            <TableCell className="font-mono text-sm">{peer.assignedIp}</TableCell>
                            <TableCell>
                              {peer.status?.connected ? (
                                <Badge variant="default" className="gap-1">
                                  <Wifi className="h-3 w-3" />
                                  Connected
                                </Badge>
                              ) : (
                                <Badge variant="secondary" className="gap-1">
                                  <WifiOff className="h-3 w-3" />
                                  Disconnected
                                </Badge>
                              )}
                            </TableCell>
                            <TableCell className="text-sm">
                              {peer.status?.bytesReceived !== undefined
                                ? `${formatBytes(peer.status.bytesReceived)} / ${formatBytes(peer.status.bytesSent || 0)}`
                                : "-"}
                            </TableCell>
                            <TableCell className="text-sm text-muted-foreground">
                              {new Date(peer.createdAt).toLocaleDateString()}
                            </TableCell>
                            <TableCell>
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => {
                                  setPeerToDelete(peer);
                                  setDeleteDialogOpen(true);
                                }}
                              >
                                <Trash2 className="h-4 w-4" />
                              </Button>
                            </TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>
            </TabsContent>

            <TabsContent value="networks">
              <Card>
                <CardHeader>
                  <CardTitle>Team Networks</CardTitle>
                  <CardDescription>
                    VPN subnet allocations for each team
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Team</TableHead>
                        <TableHead>Subnet</TableHead>
                        <TableHead>Gateway</TableHead>
                        <TableHead>Peers</TableHead>
                        <TableHead>Status</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {teamVPNs.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={5} className="text-center text-muted-foreground">
                            No team networks configured
                          </TableCell>
                        </TableRow>
                      ) : (
                        teamVPNs.map((tv) => (
                          <TableRow key={tv.id}>
                            <TableCell className="font-medium">{tv.teamName}</TableCell>
                            <TableCell className="font-mono text-sm">{tv.subnet}</TableCell>
                            <TableCell className="font-mono text-sm">{tv.gateway}</TableCell>
                            <TableCell>{tv.peerCount}</TableCell>
                            <TableCell>
                              <Badge variant={tv.enabled ? "default" : "secondary"}>
                                {tv.enabled ? "Active" : "Disabled"}
                              </Badge>
                            </TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>
            </TabsContent>
          </Tabs>
        )}

        {/* Delete Confirmation Dialog */}
        <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Delete VPN Peer</DialogTitle>
              <DialogDescription>
                Are you sure you want to delete the VPN peer for user &quot;{peerToDelete?.username}&quot;?
                They will need to download a new configuration to reconnect.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setDeleteDialogOpen(false)}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={handleDeletePeer}>
                Delete
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </>
  );
}
