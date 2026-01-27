import { useState, useEffect, useCallback } from "react";
import { useRouter } from "next/router";
import { useAuth } from "@/context/AuthContext";
import { useSiteConfig } from "@/context/SiteConfigContext";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import axios from "@/lib/axios";
import { useLanguage } from "@/context/LanguageContext";
import { toast } from "sonner";
import Head from "next/head";
import {
  Download,
  RefreshCw,
  Shield,
  ShieldCheck,
  ShieldX,
  Wifi,
  WifiOff,
  Server,
  Network,
  Clock,
  ArrowUpDown,
  Copy,
  CheckCircle2,
  AlertCircle,
  Info,
} from "lucide-react";

interface VPNStatus {
  enabled: boolean;
  configured: boolean;
  hasPeer: boolean;
  connected: boolean;
  requiredForInstances: boolean;
  serverEndpoint?: string;
  assignedIp?: string;
  publicKey?: string;
  teamSubnet?: string;
  lastHandshake?: string;
  bytesReceived?: number;
  bytesSent?: number;
  requiresTeam?: boolean;
  message?: string;
}

export default function VPNPage() {
  const { t } = useLanguage();
  const { getSiteName } = useSiteConfig();
  const [vpnStatus, setVpnStatus] = useState<VPNStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [downloading, setDownloading] = useState(false);
  const [regenerating, setRegenerating] = useState(false);
  const [copied, setCopied] = useState(false);
  const router = useRouter();
  const { loggedIn, authChecked } = useAuth();

  const fetchVPNStatus = useCallback(async () => {
    try {
      const res = await axios.get("/api/vpn/status");
      setVpnStatus(res.data);
    } catch (err: any) {
      console.error("Failed to fetch VPN status:", err);
      setVpnStatus(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (authChecked && !loggedIn) {
      router.replace("/login");
    } else if (authChecked && loggedIn) {
      fetchVPNStatus();
    }
  }, [authChecked, loggedIn, router, fetchVPNStatus]);

  // Auto-refresh status every 30 seconds
  useEffect(() => {
    if (!loggedIn) return;
    const interval = setInterval(fetchVPNStatus, 30000);
    return () => clearInterval(interval);
  }, [loggedIn, fetchVPNStatus]);

  const handleDownloadConfig = async () => {
    setDownloading(true);
    try {
      const res = await axios.get("/api/vpn/config", {
        responseType: "blob",
      });

      const url = window.URL.createObjectURL(new Blob([res.data]));
      const link = document.createElement("a");
      link.href = url;
      link.setAttribute("download", "wg-pwnthemall.conf");
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.URL.revokeObjectURL(url);

      toast.success(t("vpn.config_downloaded") || "VPN configuration downloaded");
      fetchVPNStatus();
    } catch (err: any) {
      const errorMsg = err.response?.data?.error || "Failed to download config";
      toast.error(t(errorMsg) || errorMsg, { className: "bg-red-600 text-white" });
    } finally {
      setDownloading(false);
    }
  };

  const handleRegenerateKeys = async () => {
    if (!confirm(t("vpn.regenerate_confirm") || "Are you sure? This will invalidate your current VPN configuration.")) {
      return;
    }

    setRegenerating(true);
    try {
      await axios.post("/api/vpn/regenerate");
      toast.success(t("vpn.keys_regenerated") || "VPN keys regenerated successfully");
      fetchVPNStatus();
    } catch (err: any) {
      const errorMsg = err.response?.data?.error || "Failed to regenerate keys";
      toast.error(t(errorMsg) || errorMsg, { className: "bg-red-600 text-white" });
    } finally {
      setRegenerating(false);
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
    toast.success(t("copied") || "Copied to clipboard");
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  };

  const formatTimestamp = (timestamp: string) => {
    const date = new Date(timestamp);
    return date.toLocaleString();
  };

  if (!authChecked || !loggedIn) return null;

  return (
    <>
      <Head>
        <title>{t("vpn.title") || "VPN"} - {getSiteName()}</title>
      </Head>
      <div className="container mx-auto py-8 px-4 max-w-4xl">
        <div className="mb-8">
          <h1 className="text-3xl font-bold flex items-center gap-2">
            <Shield className="h-8 w-8" />
            {t("vpn.title") || "VPN Access"}
          </h1>
          <p className="text-muted-foreground mt-2">
            {t("vpn.description") || "Connect to the VPN to access challenge instances"}
          </p>
        </div>

        {loading ? (
          <Card>
            <CardContent className="py-8 flex items-center justify-center">
              <RefreshCw className="h-6 w-6 animate-spin" />
            </CardContent>
          </Card>
        ) : !vpnStatus?.enabled ? (
          <Alert>
            <ShieldX className="h-4 w-4" />
            <AlertTitle>{t("vpn.disabled_title") || "VPN Not Available"}</AlertTitle>
            <AlertDescription>
              {t("vpn.disabled_description") || "VPN access is currently disabled. Contact an administrator for more information."}
            </AlertDescription>
          </Alert>
        ) : vpnStatus.requiresTeam ? (
          <Alert>
            <AlertCircle className="h-4 w-4" />
            <AlertTitle>{t("vpn.team_required_title") || "Team Required"}</AlertTitle>
            <AlertDescription>
              {t("vpn.team_required_description") || "You need to join or create a team before you can access the VPN."}
              <Button variant="link" className="p-0 h-auto" onClick={() => router.push("/team")}>
                {t("team.go_to_team") || "Go to Team page"}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <div className="space-y-6">
            {/* Status Card */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center justify-between">
                  <span className="flex items-center gap-2">
                    {vpnStatus.connected ? (
                      <Wifi className="h-5 w-5 text-green-500" />
                    ) : (
                      <WifiOff className="h-5 w-5 text-muted-foreground" />
                    )}
                    {t("vpn.connection_status") || "Connection Status"}
                  </span>
                  <Badge variant={vpnStatus.connected ? "default" : "secondary"}>
                    {vpnStatus.connected ? (t("vpn.connected") || "Connected") : (t("vpn.disconnected") || "Disconnected")}
                  </Badge>
                </CardTitle>
                <CardDescription>
                  {vpnStatus.requiredForInstances && (
                    <span className="text-amber-500 flex items-center gap-1">
                      <Info className="h-4 w-4" />
                      {t("vpn.required_for_instances") || "VPN connection is required to start challenge instances"}
                    </span>
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="grid gap-4 md:grid-cols-2">
                  <div className="space-y-2">
                    <div className="flex items-center gap-2 text-sm">
                      <Server className="h-4 w-4 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("vpn.server") || "Server"}:</span>
                      <span className="font-mono">{vpnStatus.serverEndpoint || "N/A"}</span>
                    </div>
                    {vpnStatus.assignedIp && (
                      <div className="flex items-center gap-2 text-sm">
                        <Network className="h-4 w-4 text-muted-foreground" />
                        <span className="text-muted-foreground">{t("vpn.your_ip") || "Your IP"}:</span>
                        <span className="font-mono">{vpnStatus.assignedIp}</span>
                      </div>
                    )}
                    {vpnStatus.teamSubnet && (
                      <div className="flex items-center gap-2 text-sm">
                        <Network className="h-4 w-4 text-muted-foreground" />
                        <span className="text-muted-foreground">{t("vpn.team_subnet") || "Team Subnet"}:</span>
                        <span className="font-mono">{vpnStatus.teamSubnet}</span>
                      </div>
                    )}
                  </div>
                  {vpnStatus.connected && (
                    <div className="space-y-2">
                      {vpnStatus.lastHandshake && (
                        <div className="flex items-center gap-2 text-sm">
                          <Clock className="h-4 w-4 text-muted-foreground" />
                          <span className="text-muted-foreground">{t("vpn.last_handshake") || "Last Handshake"}:</span>
                          <span>{formatTimestamp(vpnStatus.lastHandshake)}</span>
                        </div>
                      )}
                      <div className="flex items-center gap-2 text-sm">
                        <ArrowUpDown className="h-4 w-4 text-muted-foreground" />
                        <span className="text-muted-foreground">{t("vpn.data_transferred") || "Data"}:</span>
                        <span>
                          {formatBytes(vpnStatus.bytesReceived || 0)} / {formatBytes(vpnStatus.bytesSent || 0)}
                        </span>
                      </div>
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>

            {/* Actions Card */}
            <Card>
              <CardHeader>
                <CardTitle>{t("vpn.configuration") || "Configuration"}</CardTitle>
                <CardDescription>
                  {t("vpn.configuration_description") || "Download your WireGuard configuration file"}
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex flex-wrap gap-4">
                  <Button onClick={handleDownloadConfig} disabled={downloading}>
                    {downloading ? (
                      <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
                    ) : (
                      <Download className="mr-2 h-4 w-4" />
                    )}
                    {t("vpn.download_config") || "Download Config"}
                  </Button>
                  <Button variant="outline" onClick={handleRegenerateKeys} disabled={regenerating}>
                    {regenerating ? (
                      <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
                    ) : (
                      <RefreshCw className="mr-2 h-4 w-4" />
                    )}
                    {t("vpn.regenerate_keys") || "Regenerate Keys"}
                  </Button>
                  <Button variant="outline" onClick={fetchVPNStatus}>
                    <RefreshCw className="mr-2 h-4 w-4" />
                    {t("vpn.refresh_status") || "Refresh Status"}
                  </Button>
                </div>

                {vpnStatus.publicKey && (
                  <div className="mt-4 p-4 bg-muted rounded-lg">
                    <div className="flex items-center justify-between">
                      <span className="text-sm text-muted-foreground">{t("vpn.your_public_key") || "Your Public Key"}:</span>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => copyToClipboard(vpnStatus.publicKey!)}
                      >
                        {copied ? <CheckCircle2 className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                      </Button>
                    </div>
                    <code className="text-xs break-all">{vpnStatus.publicKey}</code>
                  </div>
                )}
              </CardContent>
            </Card>

            {/* Setup Instructions */}
            <Card>
              <CardHeader>
                <CardTitle>{t("vpn.setup_instructions") || "Setup Instructions"}</CardTitle>
              </CardHeader>
              <CardContent>
                <Tabs defaultValue="linux" className="w-full">
                  <TabsList className="grid w-full grid-cols-4">
                    <TabsTrigger value="linux">Linux</TabsTrigger>
                    <TabsTrigger value="windows">Windows</TabsTrigger>
                    <TabsTrigger value="macos">macOS</TabsTrigger>
                    <TabsTrigger value="mobile">Mobile</TabsTrigger>
                  </TabsList>
                  <TabsContent value="linux" className="mt-4 space-y-4">
                    <div className="space-y-2">
                      <p className="text-sm">1. {t("vpn.install_wireguard") || "Install WireGuard"}:</p>
                      <pre className="bg-muted p-2 rounded text-sm overflow-x-auto">
                        sudo apt install wireguard
                      </pre>
                    </div>
                    <div className="space-y-2">
                      <p className="text-sm">2. {t("vpn.save_config") || "Save the configuration file"}:</p>
                      <pre className="bg-muted p-2 rounded text-sm overflow-x-auto">
                        sudo mv ~/Downloads/wg-pwnthemall.conf /etc/wireguard/
                      </pre>
                    </div>
                    <div className="space-y-2">
                      <p className="text-sm">3. {t("vpn.start_vpn") || "Start the VPN"}:</p>
                      <pre className="bg-muted p-2 rounded text-sm overflow-x-auto">
                        sudo wg-quick up wg-pwnthemall
                      </pre>
                    </div>
                    <div className="space-y-2">
                      <p className="text-sm">4. {t("vpn.stop_vpn") || "To disconnect"}:</p>
                      <pre className="bg-muted p-2 rounded text-sm overflow-x-auto">
                        sudo wg-quick down wg-pwnthemall
                      </pre>
                    </div>
                  </TabsContent>
                  <TabsContent value="windows" className="mt-4 space-y-4">
                    <div className="space-y-2">
                      <p className="text-sm">1. {t("vpn.download_wireguard_windows") || "Download WireGuard from"}:</p>
                      <a
                        href="https://www.wireguard.com/install/"
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-primary hover:underline"
                      >
                        https://www.wireguard.com/install/
                      </a>
                    </div>
                    <div className="space-y-2">
                      <p className="text-sm">2. {t("vpn.import_tunnel") || "Open WireGuard and click 'Import tunnel(s) from file'"}.</p>
                    </div>
                    <div className="space-y-2">
                      <p className="text-sm">3. {t("vpn.select_config") || "Select the downloaded configuration file"}.</p>
                    </div>
                    <div className="space-y-2">
                      <p className="text-sm">4. {t("vpn.click_activate") || "Click 'Activate' to connect"}.</p>
                    </div>
                  </TabsContent>
                  <TabsContent value="macos" className="mt-4 space-y-4">
                    <div className="space-y-2">
                      <p className="text-sm">1. {t("vpn.install_wireguard_macos") || "Install WireGuard from the App Store or"}:</p>
                      <pre className="bg-muted p-2 rounded text-sm overflow-x-auto">
                        brew install wireguard-tools
                      </pre>
                    </div>
                    <div className="space-y-2">
                      <p className="text-sm">2. {t("vpn.macos_gui") || "If using the GUI app, import the configuration file"}.</p>
                    </div>
                    <div className="space-y-2">
                      <p className="text-sm">3. {t("vpn.macos_cli") || "If using CLI"}:</p>
                      <pre className="bg-muted p-2 rounded text-sm overflow-x-auto">
                        sudo wg-quick up ~/Downloads/wg-pwnthemall.conf
                      </pre>
                    </div>
                  </TabsContent>
                  <TabsContent value="mobile" className="mt-4 space-y-4">
                    <div className="space-y-2">
                      <p className="text-sm">1. {t("vpn.install_app") || "Install the WireGuard app from your app store"}:</p>
                      <ul className="list-disc list-inside text-sm ml-4">
                        <li>
                          <a
                            href="https://apps.apple.com/app/wireguard/id1441195209"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-primary hover:underline"
                          >
                            iOS App Store
                          </a>
                        </li>
                        <li>
                          <a
                            href="https://play.google.com/store/apps/details?id=com.wireguard.android"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-primary hover:underline"
                          >
                            Google Play Store
                          </a>
                        </li>
                      </ul>
                    </div>
                    <div className="space-y-2">
                      <p className="text-sm">2. {t("vpn.scan_qr") || "Download the config file and import it, or use the QR code feature (available in admin panel)"}.</p>
                    </div>
                  </TabsContent>
                </Tabs>
              </CardContent>
            </Card>
          </div>
        )}
      </div>
    </>
  );
}
