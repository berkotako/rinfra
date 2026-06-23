package c2

// Primitive is a portable, framework-agnostic action that a portable
// domain.Technique compiles down to. It is the contract between two layers:
//
//   - the technique catalog (internal/emulation/catalog) — open-ended and
//     data-driven: it maps an ATT&CK technique to a Primitive + argument
//     bindings, so adding a TTP that reuses an existing primitive is a data
//     change, not a code change;
//   - each framework's translator — a small, stable switch over the closed set
//     of PrimitiveKinds below that renders a Primitive into that framework's
//     native command(s). A framework that does not implement a given kind
//     reports it unsupported (honest BAS taxonomy — no fabricated attempt).
//
// This replaces the per-framework `switch t.AttackID` tables (which duplicated
// the same ~10 ATT&CK IDs across every adapter) with one catalog plus one small
// renderer per framework.
type Primitive struct {
	Kind PrimitiveKind
	// Args holds the resolved arguments for the primitive (defaults from the
	// catalog merged over the technique's Inputs). Keys are primitive-specific
	// and documented per PrimitiveKind below.
	Args map[string]string
}

// Arg returns the named argument (empty string if absent).
func (p Primitive) Arg(name string) string { return p.Args[name] }

// PrimitiveKind enumerates the closed set of portable actions a framework
// translator may be asked to render. The set grows deliberately: adding a kind
// means teaching the frameworks that can support it (the rest report it
// unsupported), so it stays small and stable while the technique catalog grows
// freely on top of it.
type PrimitiveKind string

const (
	// PrimPowerShell runs a PowerShell script. Args: "script".
	PrimPowerShell PrimitiveKind = "powershell"
	// PrimShell runs an OS command-shell command. Args: "command".
	PrimShell PrimitiveKind = "shell"
	// PrimSysInfo collects host/system information. Args: none.
	PrimSysInfo PrimitiveKind = "sysinfo"
	// PrimProcessList enumerates running processes. Args: none.
	PrimProcessList PrimitiveKind = "process_list"
	// PrimNetConnections lists active network connections. Args: none.
	PrimNetConnections PrimitiveKind = "net_connections"
	// PrimNetConfig reports network configuration. Args: none.
	PrimNetConfig PrimitiveKind = "net_config"
	// PrimFileList lists files/directories. Args: "path".
	PrimFileList PrimitiveKind = "file_list"
	// PrimDownload exfiltrates a file from the host. Args: "path" (required).
	PrimDownload PrimitiveKind = "download"
	// PrimScheduledTask creates a scheduled task. Args: "task_name".
	PrimScheduledTask PrimitiveKind = "scheduled_task"
	// PrimRegistryRunKey writes a Run-key persistence entry.
	// Args: "registry_key", "registry_value".
	PrimRegistryRunKey PrimitiveKind = "registry_run_key"

	// The following are read-only host/network discovery primitives. Each maps
	// to a benign Windows built-in enumeration command (see DiscoveryCommand) —
	// no payloads, no state change, no evasion — so every framework with a shell
	// can render them uniformly. Args: none.

	// PrimRemoteSystemDiscovery enumerates other hosts on the network (T1018).
	PrimRemoteSystemDiscovery PrimitiveKind = "remote_system_discovery"
	// PrimAccountDiscovery enumerates local user accounts (T1087.001).
	PrimAccountDiscovery PrimitiveKind = "account_discovery"
	// PrimPermissionGroupDiscovery enumerates local groups (T1069.001).
	PrimPermissionGroupDiscovery PrimitiveKind = "permission_group_discovery"
	// PrimServiceDiscovery enumerates running services (T1007).
	PrimServiceDiscovery PrimitiveKind = "service_discovery"
	// PrimShareDiscovery enumerates shared resources (T1135).
	PrimShareDiscovery PrimitiveKind = "network_share_discovery"
)

// IsCleanable reports whether a primitive creates host-side state that should be
// reverted at the end of a run (a persistence artifact). The emulation engine
// asks the operator to undo these via the optional Reverter capability, so an
// engagement leaves no orphaned persistence on the customer's host.
func IsCleanable(k PrimitiveKind) bool {
	return k == PrimScheduledTask || k == PrimRegistryRunKey
}

// DiscoveryCommand maps a read-only discovery primitive to the Windows built-in
// command that enumerates it. ok=false for any non-discovery kind. The commands
// are safe, non-destructive enumeration (no payloads, no evasion) and run
// verbatim from cmd.exe or PowerShell, so each framework renderer only needs to
// wrap them in its own invocation convention rather than restate the command.
func DiscoveryCommand(k PrimitiveKind) (string, bool) {
	switch k {
	case PrimRemoteSystemDiscovery:
		return "net view", true
	case PrimAccountDiscovery:
		return "net user", true
	case PrimPermissionGroupDiscovery:
		return "net localgroup", true
	case PrimServiceDiscovery:
		return "net start", true
	case PrimShareDiscovery:
		return "net share", true
	default:
		return "", false
	}
}
