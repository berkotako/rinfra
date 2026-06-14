// Mock data — port of design/project/data.js, domain-vocabulary aligned per §2
import type {
  ProviderMeta,
  CloudProvider,
  C2Framework,
  Engagement,
  Scenario,
  CanvasNode,
  CanvasEdge,
  NodeTemplate,
  OperatorMode,
  OperatorSession,
} from "./types";

export const PROVIDERS: Record<CloudProvider, ProviderMeta> = {
  aws: { id: "aws", name: "AWS", label: "Amazon Web Services", color: "var(--aws)", short: "AWS" },
  gcp: { id: "gcp", name: "GCP", label: "Google Cloud", color: "var(--gcp)", short: "GCP" },
  azure: { id: "azure", name: "Azure", label: "Microsoft Azure", color: "var(--azure)", short: "AZ" },
  digitalocean: { id: "digitalocean", name: "DO", label: "DigitalOcean", color: "var(--do)", short: "DO" },
};

export const REGIONS: Record<CloudProvider, string[]> = {
  aws: ["us-east-1", "us-west-2", "eu-west-1", "eu-central-1", "ap-southeast-1"],
  gcp: ["us-central1", "us-east4", "europe-west1", "asia-southeast1"],
  azure: ["eastus", "westus2", "westeurope", "northeurope"],
  digitalocean: ["nyc3", "sfo3", "ams3", "fra1", "sgp1"],
};

// 8 C2 frameworks per §2 (SUPPORT_MATRIX.md). Tier 'fronted' replaces prototype's 'manual'.
export const C2_FRAMEWORKS: C2Framework[] = [
  {
    id: "sliver",
    name: "Sliver",
    tier: "orchestrated",
    tierLabel: "Fully orchestrated",
    note: "Cross-platform implants, mTLS/HTTP(S)/DNS, automated emulation supported.",
    gated: false,
    listeners: ["mTLS", "HTTPS", "WireGuard", "DNS"],
    lang: "Go · BishopFox",
  },
  {
    id: "mythic",
    name: "Mythic",
    tier: "orchestrated",
    tierLabel: "Fully orchestrated",
    note: "Modular agents via Docker profiles; full API for scripted scenarios.",
    gated: false,
    listeners: ["HTTP", "HTTPS", "WebSocket", "SMB"],
    lang: "Python · @its_a_feature_",
  },
  {
    id: "metasploit",
    name: "Metasploit",
    tier: "orchestrated",
    tierLabel: "Fully orchestrated",
    note: "msfrpcd RPC drives Meterpreter sessions; open source, no license key required.",
    gated: false,
    listeners: ["TCP", "HTTPS", "HTTP"],
    lang: "Ruby · Rapid7 (open source)",
  },
  {
    id: "custom",
    name: "In-house / Custom",
    tier: "orchestrated",
    tierLabel: "Fully orchestrated",
    note: "Bring your own framework via container image and listener spec. You own the operator surface.",
    gated: false,
    listeners: ["Custom"],
    lang: "Your image",
  },
  {
    id: "havoc",
    name: "Havoc",
    tier: "scripted",
    tierLabel: "Scripted",
    note: "Demon agent with sleep obfuscation; emulation via scripted operator API.",
    gated: false,
    listeners: ["HTTP", "HTTPS", "SMB"],
    lang: "C / ASM · @C5pider",
  },
  {
    id: "poshc2",
    name: "PoshC2",
    tier: "scripted",
    tierLabel: "Scripted",
    note: "PowerShell-native implants with Python server; scriptable but no modern formal API.",
    gated: false,
    listeners: ["HTTP", "HTTPS"],
    lang: "Python/PS · PoshSec (open source)",
  },
  {
    id: "cobalt",
    name: "Cobalt Strike",
    tier: "fronted",
    tierLabel: "Deploy & operate manually",
    note: "Beacon C2. Provision the team server; operate from your own client.",
    gated: true,
    listeners: ["HTTP", "HTTPS", "DNS", "SMB", "TCP"],
    lang: "Fortra · licensed",
  },
  {
    id: "bruteratel",
    name: "Brute Ratel C4",
    tier: "fronted",
    tierLabel: "Deploy & operate manually",
    note: "EDR-evasion-focused C2. License-gated; provision the server and operate manually with your key.",
    gated: true,
    listeners: ["HTTP", "HTTPS", "DNS"],
    lang: "Chetan Nayak · licensed",
  },
];

export const ENGAGEMENTS: Engagement[] = [
  {
    id: "ENG-2411",
    client: "Meridian Financial Group",
    codename: "Northwind",
    scope: "External perimeter + assumed-breach",
    status: "active",
    auth: "authorized",
    authBy: "K. Alvarez (CISO)",
    start: "2026-05-28",
    end: "2026-06-20",
    assets: 6,
    live: 4,
    cost: 12.40,
    lead: "R. Okafor",
    targets: ["*.meridian-fg.com", "203.0.113.0/24"],
    frameworks: ["Sliver", "Mythic"],
  },
  {
    id: "ENG-2409",
    client: "Atlas Health Systems",
    codename: "Cedar",
    scope: "Internal AD adversary emulation",
    status: "active",
    auth: "authorized",
    authBy: "D. Whitfield (VP Sec)",
    start: "2026-06-01",
    end: "2026-06-28",
    assets: 4,
    live: 3,
    cost: 8.10,
    lead: "M. Chen",
    targets: ["10.20.0.0/16", "corp.atlas-health.internal"],
    frameworks: ["Havoc"],
  },
  {
    id: "ENG-2407",
    client: "Pinnacle Retail Co.",
    codename: "Saffron",
    scope: "Web app + phishing infrastructure",
    status: "active",
    auth: "authorized",
    authBy: "L. Romano (Dir IR)",
    start: "2026-06-08",
    end: "2026-07-02",
    assets: 3,
    live: 0,
    cost: 0.00,
    lead: "R. Okafor",
    targets: ["shop.pinnacle-retail.com"],
    frameworks: ["Sliver"],
  },
  {
    id: "ENG-2402",
    client: "Vanguard Defense",
    codename: "Ironwood",
    scope: "Red team — full kill chain",
    status: "draft",
    auth: "pending",
    authBy: "—",
    start: "2026-06-15",
    end: "2026-07-20",
    assets: 0,
    live: 0,
    cost: 0.00,
    lead: "S. Park",
    targets: [],
    frameworks: [],
  },
  {
    id: "ENG-2398",
    client: "Helios Energy",
    codename: "Basalt",
    scope: "OT-adjacent assumed breach",
    status: "completed",
    auth: "expired",
    authBy: "T. Brooks (CISO)",
    start: "2026-04-10",
    end: "2026-05-09",
    assets: 7,
    live: 0,
    cost: 41.85,
    lead: "M. Chen",
    targets: ["172.16.0.0/12"],
    frameworks: ["Cobalt Strike", "Sliver"],
  },
  {
    id: "ENG-2390",
    client: "Crest Logistics",
    codename: "Marlin",
    scope: "External + social engineering",
    status: "completed",
    auth: "expired",
    authBy: "P. Nilsson (CISO)",
    start: "2026-03-02",
    end: "2026-03-30",
    assets: 5,
    live: 0,
    cost: 28.30,
    lead: "S. Park",
    targets: ["*.crest-logistics.com"],
    frameworks: ["Mythic"],
  },
];

export const SCENARIOS: Scenario[] = [
  {
    id: "apt29",
    name: "APT29 — Cozy Bear",
    actor: "Nation-state · espionage",
    desc: "Emulates Midnight Blizzard tradecraft: spearphishing, stealthy persistence, credential theft, and slow exfil over HTTPS.",
    techniques: [
      { id: "T1566.002", name: "Spearphishing Link", tactic: "Initial Access" },
      { id: "T1059.001", name: "PowerShell", tactic: "Execution" },
      { id: "T1547.001", name: "Registry Run Keys", tactic: "Persistence" },
      { id: "T1055", name: "Process Injection", tactic: "Defense Evasion" },
      { id: "T1003.001", name: "LSASS Memory", tactic: "Credential Access" },
      { id: "T1018", name: "Remote System Discovery", tactic: "Discovery" },
      { id: "T1021.001", name: "Remote Desktop Protocol", tactic: "Lateral Movement" },
      { id: "T1567.002", name: "Exfil to Cloud Storage", tactic: "Exfiltration" },
    ],
  },
  {
    id: "fin7",
    name: "FIN7 — Carbanak",
    actor: "Financial · criminal",
    desc: "Point-of-sale and finance-team targeting: malicious documents, in-memory loaders, and lateral movement to payment systems.",
    techniques: [
      { id: "T1566.001", name: "Spearphishing Attachment", tactic: "Initial Access" },
      { id: "T1204.002", name: "Malicious File", tactic: "Execution" },
      { id: "T1053.005", name: "Scheduled Task", tactic: "Persistence" },
      { id: "T1112", name: "Modify Registry", tactic: "Defense Evasion" },
      { id: "T1555.003", name: "Credentials from Browsers", tactic: "Credential Access" },
      { id: "T1057", name: "Process Discovery", tactic: "Discovery" },
      { id: "T1570", name: "Lateral Tool Transfer", tactic: "Lateral Movement" },
    ],
  },
  {
    id: "ransom",
    name: "Ransomware Affiliate",
    actor: "eCrime · big-game hunting",
    desc: "Modern intrusion-to-encryption chain: valid accounts, defense impairment, mass discovery and staged impact (dry-run, no payload).",
    techniques: [
      { id: "T1078", name: "Valid Accounts", tactic: "Initial Access" },
      { id: "T1486", name: "Data Encrypted for Impact", tactic: "Impact" },
      { id: "T1490", name: "Inhibit System Recovery", tactic: "Impact" },
      { id: "T1562.001", name: "Disable Security Tools", tactic: "Defense Evasion" },
    ],
  },
];

// Initial topology for the hero engagement (ENG-2411)
// node types use domain vocabulary: c2_server / payload_host
export const INITIAL_NODES: CanvasNode[] = [
  {
    id: "n1",
    type: "redirector",
    subtype: "HTTPS",
    name: "edge-https-01",
    provider: "aws",
    region: "us-east-1",
    status: "live",
    health: 98,
    ip: "203.0.113.18",
    domain: "cdn-static-assets.net",
    x: 80,
    y: 150,
    cost: 0.42,
  },
  {
    id: "n2",
    type: "redirector",
    subtype: "DNS",
    name: "edge-dns-01",
    provider: "digitalocean",
    region: "nyc3",
    status: "live",
    health: 95,
    ip: "198.51.100.7",
    domain: "ns1.telemetry-sync.com",
    x: 80,
    y: 360,
    cost: 0.31,
  },
  {
    id: "n3",
    type: "c2_server",
    subtype: "Sliver",
    name: "teamserver-01",
    provider: "gcp",
    region: "us-central1",
    status: "live",
    health: 99,
    ip: "10.0.4.12",
    framework: "sliver",
    listener: "mTLS",
    x: 470,
    y: 150,
    cost: 1.85,
  },
  {
    id: "n4",
    type: "c2_server",
    subtype: "Mythic",
    name: "teamserver-02",
    provider: "azure",
    region: "eastus",
    status: "provisioning",
    health: 0,
    ip: "—",
    framework: "mythic",
    listener: "HTTPS",
    x: 470,
    y: 360,
    cost: 1.85,
  },
  {
    id: "n5",
    type: "payload_host",
    subtype: "Staging",
    name: "stage-host-01",
    provider: "aws",
    region: "us-east-1",
    status: "live",
    health: 92,
    ip: "203.0.113.44",
    domain: "updates-delivery.net",
    x: 860,
    y: 255,
    cost: 0.38,
  },
];

export const INITIAL_EDGES: CanvasEdge[] = [
  { id: "e1", from: "n1", to: "n3" },
  { id: "e2", from: "n2", to: "n3" },
  { id: "e3", from: "n1", to: "n4" },
  { id: "e4", from: "n3", to: "n5" },
];

export const NODE_TEMPLATES: NodeTemplate[] = [
  { type: "redirector", subtype: "HTTPS", label: "HTTPS Redirector", icon: "Globe", desc: "TLS traffic relay" },
  { type: "redirector", subtype: "HTTP", label: "HTTP Redirector", icon: "Globe", desc: "Cleartext relay" },
  { type: "redirector", subtype: "DNS", label: "DNS Redirector", icon: "Dns", desc: "DNS-over-relay" },
  { type: "c2_server", subtype: "Sliver", label: "C2 Server", icon: "Server", desc: "Team server host" },
  { type: "payload_host", subtype: "Staging", label: "Staging Host", icon: "HardDrive", desc: "Payload delivery" },
];

// Per-framework operator-access spec: how operators connect a native client and
// how fully RInfra automates it. Mirrors the control plane's per-framework
// c2.ManualAccess descriptors + support tiers.
export interface C2AccessSpec {
  client: string;
  protocol: string;
  port: number;
  mode: OperatorMode;
  liveClient: string; // "" for manual-only (Fronted tier)
  instructions: string;
}

export const C2_OPERATOR_ACCESS: Record<string, C2AccessSpec> = {
  sliver: {
    client: "sliver-client",
    protocol: "grpc-mtls",
    port: 31337,
    mode: "live",
    liveClient: "gRPC operator API (mTLS)",
    instructions:
      "Fetch the operator .cfg from the teamserver, open the tunnel, then `sliver-client import <cfg>` — it reaches the multiplayer listener at 127.0.0.1:31337 through the tunnel.",
  },
  mythic: {
    client: "Mythic web UI",
    protocol: "web-ui",
    port: 7443,
    mode: "live",
    liveClient: "GraphQL scripting API",
    instructions:
      "Open the tunnel, then browse https://127.0.0.1:7443 and log in with the operator credentials set at install. Drive tasks from the UI.",
  },
  metasploit: {
    client: "msfconsole / msfrpcd client",
    protocol: "https",
    port: 55553,
    mode: "live",
    liveClient: "msfrpcd RPC",
    instructions:
      "Open the tunnel, then connect your client to 127.0.0.1:55553 with the RPC credentials from /etc/msf/rpc.env.",
  },
  custom: {
    client: "your operator client",
    protocol: "tcp",
    port: 8443,
    mode: "live",
    liveClient: "your operator surface",
    instructions:
      "Open the tunnel and connect your client to the operator port you defined for this framework.",
  },
  havoc: {
    client: "Havoc client",
    protocol: "tcp",
    port: 40056,
    mode: "scripted",
    liveClient: "scripted operator API",
    instructions:
      "Open the tunnel, then connect the Havoc client to 127.0.0.1:40056. Some emulation steps are scripted; the rest are driven by hand.",
  },
  poshc2: {
    client: "PoshC2 ImplantHandler",
    protocol: "tcp",
    port: 8443,
    mode: "scripted",
    liveClient: "scripted CLI",
    instructions:
      "Open the tunnel and use the PoshC2 server CLI over it. Partial scripting; the operator drives the rest.",
  },
  cobalt: {
    client: "Cobalt Strike client (aggressor)",
    protocol: "tcp",
    port: 50050,
    mode: "manual",
    liveClient: "",
    instructions:
      "Open the tunnel, then connect the Cobalt Strike client to 127.0.0.1:50050 with the team server password set at deploy time. RInfra provisions and fronts CS; the operator drives it.",
  },
  bruteratel: {
    client: "Brute Ratel C4 commander",
    protocol: "https",
    port: 443,
    mode: "manual",
    liveClient: "",
    instructions:
      "Open the tunnel, then point the Brute Ratel commander at 127.0.0.1:443 with the operator profile/license for this engagement.",
  },
};

// Mock active operator sessions, keyed by C2 node id. Surfaced only for live nodes.
export const OPERATOR_SESSIONS: Record<string, OperatorSession[]> = {
  n3: [
    { id: "9f3c1a2b", host: "WS-FIN-04", user: "CORP\\j.reyes", os: "windows/amd64" },
    { id: "2b77e0d4", host: "SRV-DC-01", user: "NT AUTHORITY\\SYSTEM", os: "windows/amd64" },
  ],
};

export const STATUS_META: Record<string, { label: string; cls: string }> = {
  live: { label: "Live", cls: "ok" },
  provisioning: { label: "Provisioning", cls: "warn" },
  draining: { label: "Draining", cls: "info" },
  destroyed: { label: "Destroyed", cls: "" },
  pending: { label: "Draft", cls: "" },
  failed: { label: "Failed", cls: "danger" },
};
