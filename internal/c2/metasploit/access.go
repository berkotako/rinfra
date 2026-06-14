package metasploit

import (
	"fmt"

	"github.com/rinfra/rinfra/internal/c2"
)

// ManualAccess lets an operator drive Metasploit by hand, connecting a local
// msfconsole/Armitage to the msfrpcd RPC port over the tunnel.
func (p *provider) ManualAccess(ts c2.Teamserver) (c2.ManualAccess, error) {
	return c2.ManualAccess{
		Framework:    "metasploit",
		Client:       "msfconsole / msfrpcd client",
		Protocol:     c2.AccessHTTPS,
		OperatorPort: msfRpcdPort,
		Tunnel:       c2.DefaultTunnel(ts, msfRpcdPort),
		Instructions: fmt.Sprintf("Open the tunnel, then connect your client to 127.0.0.1:%d using the "+
			"RPC credentials from /etc/msf/rpc.env (e.g. msfconsole `load msgrpc` or msf-client).", msfRpcdPort),
	}, nil
}
