package cobaltstrike

import (
	"fmt"

	"github.com/rinfra/rinfra/internal/c2"
)

// ManualAccess is the primary usage mode for Cobalt Strike: a Fronted-tier
// framework with no Operator, it is always driven by a human connecting the
// native Cobalt Strike client to the team server over the tunnel.
func (p *provider) ManualAccess(ts c2.Teamserver) (c2.ManualAccess, error) {
	return c2.ManualAccess{
		Framework:    "cobaltstrike",
		Client:       "Cobalt Strike client (aggressor)",
		Protocol:     c2.AccessTCP,
		OperatorPort: csPort,
		Tunnel:       c2.DefaultTunnel(ts, csPort),
		Instructions: fmt.Sprintf("Open the tunnel, then connect the Cobalt Strike client to 127.0.0.1:%d "+
			"with the team server password set at deploy time. RInfra provisions and fronts CS; the "+
			"operator drives it.", csPort),
	}, nil
}
