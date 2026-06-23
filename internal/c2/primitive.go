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
)
