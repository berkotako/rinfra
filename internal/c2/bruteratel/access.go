package bruteratel

import (
	"fmt"

	"github.com/rinfra/rinfra/internal/c2"
)

// ManualAccess is the primary usage mode for Brute Ratel C4: a Fronted-tier
// framework with no Operator, driven by a human connecting the native commander
// to the server over the tunnel.
func (p *provider) ManualAccess(ts c2.Teamserver) (c2.ManualAccess, error) {
	return c2.ManualAccess{
		Framework:    "bruteratel",
		Client:       "Brute Ratel C4 commander",
		Protocol:     c2.AccessHTTPS,
		OperatorPort: brcPort,
		Tunnel:       c2.DefaultTunnel(ts, brcPort),
		Instructions: fmt.Sprintf("Open the tunnel, then point the Brute Ratel commander at 127.0.0.1:%d "+
			"with the operator profile/license configured for this engagement.", brcPort),
	}, nil
}
