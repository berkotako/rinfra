// Shell session abstraction for the in-browser operator terminal.
//
// MockShellSession drives a believable simulated teamserver session for the
// static demo. WsShellSession is the real seam: in REST mode it connects to a
// backend WebSocket PTY bridge at
//   ws(s)://<api>/api/v1/engagements/{id}/c2/{nodeId}/shell
// and streams bytes both ways. getShellSession() picks the right one.
import type { DeployedC2 } from "./types";

export interface ShellSession {
  open(): void;
  send(line: string): void;
  close(): void;
  onData(cb: (chunk: string) => void): void;
  onClose(cb: () => void): void;
}

// Sentinel the terminal interprets as "clear the screen".
export const CLEAR = "\u0000CLEAR\u0000";

const CONSOLE_HELP = `Commands:
  help                 show this help
  sessions             list active agent sessions
  use <id>             interact with a session
  info                 teamserver / listener details
  whoami               current operator or session identity
  ps                   list processes on the active session
  ls [path]            list a directory
  ifconfig             network interfaces
  netstat              active connections
  background           detach from the active session
  clear                clear the screen
  exit                 close this shell
`;

const HOST_HELP = `Commands: help  whoami  id  pwd  ls  ps  uname  netstat  cat  clear  exit
`;

function fmtSessions(d: DeployedC2): string {
  if (d.sessions.length === 0) return "No active sessions.\n";
  const rows = d.sessions
    .map((s) => `  ${s.id}   ${s.host.padEnd(11)} ${s.user.padEnd(22)} ${s.os}`)
    .join("\n");
  return `  ID         HOST        USER                   OS\n${rows}\n`;
}

class MockShellSession implements ShellSession {
  private dataCb?: (c: string) => void;
  private closeCb?: () => void;
  private readonly console: boolean;
  private active?: string;

  constructor(private d: DeployedC2) {
    this.console = d.operatorMode !== "manual";
  }

  onData(cb: (c: string) => void) {
    this.dataCb = cb;
  }
  onClose(cb: () => void) {
    this.closeCb = cb;
  }

  private emit(s: string) {
    setTimeout(() => this.dataCb?.(s), 70);
  }

  prompt(): string {
    if (!this.console) return `root@${this.d.name}:~# `;
    return this.active ? `${this.d.framework} (${this.active}) > ` : `${this.d.framework} > `;
  }

  open() {
    const banner = this.console
      ? `RInfra web shell — ${this.d.frameworkName} operator console\nConnected to ${this.d.name} (${this.d.ip}) over the SSH tunnel.\nType 'help' for commands.\n\n`
      : `RInfra web shell — ${this.d.frameworkName} team server\nConnected to ${this.d.name} (${this.d.ip}).\nType 'help' for commands.\n\n`;
    this.emit(banner);
  }

  send(line: string) {
    const cmd = line.trim();
    if (cmd === "") return;
    const [name, ...args] = cmd.split(/\s+/);

    if (name === "clear") return this.emit(CLEAR);
    if (name === "exit" || name === "quit") {
      this.emit("closing session…\n");
      setTimeout(() => this.closeCb?.(), 250);
      return;
    }

    if (!this.console) return this.hostCommand(name, args);
    return this.consoleCommand(name, args);
  }

  private consoleCommand(name: string, args: string[]) {
    switch (name) {
      case "help":
        return this.emit(CONSOLE_HELP);
      case "sessions":
        return this.emit(fmtSessions(this.d));
      case "use": {
        const id = args[0] ?? "";
        const match = this.d.sessions.find((s) => s.id.startsWith(id));
        if (!id) return this.emit("usage: use <session-id>\n");
        if (!match) return this.emit(`no session matching '${id}'\n`);
        this.active = match.id;
        return this.emit(`Active session: ${match.id}  (${match.host} — ${match.user})\n`);
      }
      case "info":
        return this.emit(
          `Framework : ${this.d.frameworkName}\nListener  : ${this.d.listener}\nHost      : ${this.d.ip}\nOperator  : ${this.d.manual.client} :${this.d.manual.operatorPort} (${this.d.manual.protocol})\nSessions  : ${this.d.sessions.length}\n`
        );
      case "whoami":
        return this.emit((this.active ? this.d.sessions.find((s) => s.id === this.active)?.user : "operator") + "\n");
      case "ps":
        if (!this.active) return this.emit("no active session — 'use <id>' first\n");
        return this.emit(
          "  PID   PPID  NAME\n  4120  680   explorer.exe\n  5012  4120  chrome.exe\n  640   504   lsass.exe\n  7330  4120  powershell.exe\n"
        );
      case "ls":
        return this.emit(
          this.active
            ? "Desktop\nDocuments\nDownloads\nfinance_Q3.xlsx\n"
            : "implants/\nloot/\nprofiles/\nteamserver.log\n"
        );
      case "ifconfig":
        return this.emit("eth0  inet 10.0.4.12  netmask 255.255.255.0\nlo    inet 127.0.0.1\n");
      case "netstat":
        return this.emit(
          "Proto  Local            Foreign           State\n tcp   10.0.4.12:8443   203.0.113.18:443  ESTABLISHED\n tcp   127.0.0.1:31337  127.0.0.1:51022   ESTABLISHED\n"
        );
      case "background":
        this.active = undefined;
        return this.emit("backgrounded.\n");
      default:
        return this.emit(`${name}: unknown command (try 'help')\n`);
    }
  }

  private hostCommand(name: string, args: string[]) {
    switch (name) {
      case "help":
        return this.emit(HOST_HELP);
      case "whoami":
        return this.emit("root\n");
      case "id":
        return this.emit("uid=0(root) gid=0(root) groups=0(root)\n");
      case "pwd":
        return this.emit("/root\n");
      case "ls":
        return this.emit("c2-data  install.log  teamserver  tls\n");
      case "ps":
        return this.emit(
          "  PID TTY          TIME CMD\n  680 ?        00:01:12 teamserver\n  704 ?        00:00:03 nginx\n  998 pts/0    00:00:00 bash\n"
        );
      case "uname":
        return this.emit("Linux teamserver 6.1.0-amd64 x86_64 GNU/Linux\n");
      case "netstat":
        return this.emit("tcp  0.0.0.0:443  LISTEN\ntcp  127.0.0.1:50050  LISTEN\n");
      case "cat":
        return this.emit(args[0] ? `${args[0]}: permission handling omitted in demo\n` : "usage: cat <file>\n");
      default:
        return this.emit(`${name}: command not found\n`);
    }
  }

  close() {
    this.closeCb?.();
  }
}

class WsShellSession implements ShellSession {
  private ws?: WebSocket;
  private dataCb?: (c: string) => void;
  private closeCb?: () => void;

  constructor(private url: string) {}

  onData(cb: (c: string) => void) {
    this.dataCb = cb;
  }
  onClose(cb: () => void) {
    this.closeCb = cb;
  }

  open() {
    try {
      this.ws = new WebSocket(this.url);
      this.ws.onmessage = (e) => this.dataCb?.(typeof e.data === "string" ? e.data : "");
      this.ws.onclose = () => this.closeCb?.();
      this.ws.onerror = () => this.dataCb?.("\n[connection error]\n");
    } catch {
      this.dataCb?.("\n[unable to open shell connection]\n");
    }
  }
  send(line: string) {
    this.ws?.send(line + "\n");
  }
  close() {
    this.ws?.close();
  }
}

export function getShellSession(engagementId: string, d: DeployedC2): ShellSession {
  const api = process.env.NEXT_PUBLIC_RINFRA_API;
  if (api) {
    const wsBase = api.replace(/^http/, "ws");
    return new WsShellSession(`${wsBase}/api/v1/engagements/${engagementId}/c2/${d.nodeId}/shell`);
  }
  return new MockShellSession(d);
}

// Expose the prompt for the mock session so the terminal can render it.
export function shellPrompt(d: DeployedC2): string {
  if (process.env.NEXT_PUBLIC_RINFRA_API) return "$ ";
  return d.operatorMode !== "manual" ? `${d.framework} > ` : `root@${d.name}:~# `;
}
