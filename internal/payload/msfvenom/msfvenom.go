// Package msfvenom adapts the upstream msfvenom payload generator (part of the
// Metasploit Framework) to RInfra's payload.Generator interface. It produces
// meterpreter stagers that call back to a deployed Metasploit listener.
//
// SCOPE: this adapter INVOKES the operator's installed upstream msfvenom binary
// with parameters derived from the Spec. It authors no payload bytes, encoders,
// or evasion logic.
package msfvenom

import (
	"context"
	"errors"

	"github.com/rinfra/rinfra/internal/payload"
)

func init() { payload.Register(&generator{}) }

type generator struct{}

func (g *generator) Name() string { return "msfvenom" }

// PairsWith: msfvenom stagers connect back to a Metasploit/meterpreter listener.
func (g *generator) PairsWith() []string { return []string{"metasploit"} }

func (g *generator) Generate(ctx context.Context, spec payload.Spec) (payload.Artifact, error) {
	// TODO(claude-code): build the upstream msfvenom argument vector from spec
	// (payload type for the meterpreter session, LHOST/LPORT from spec.Callback,
	// -f spec.Format), exec the installed binary via os/exec, write the output to
	// the engagement's payload host, hash it, and return the Artifact metadata.
	// Do not embed payload/encoder content here — only orchestrate the binary.
	return payload.Artifact{}, errors.New("msfvenom.Generate: not implemented")
}
