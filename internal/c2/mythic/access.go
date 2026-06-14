package mythic

import (
	"fmt"

	"github.com/rinfra/rinfra/internal/c2"
)

// ManualAccess lets an operator drive Mythic by hand through its browser UI over
// the tunnel, instead of the automated Operator.
func (p *provider) ManualAccess(ts c2.Teamserver) (c2.ManualAccess, error) {
	return c2.ManualAccess{
		Framework:    "mythic",
		Client:       "Mythic web UI",
		Protocol:     c2.AccessWebUI,
		OperatorPort: mythicPort,
		Tunnel:       c2.DefaultTunnel(ts, mythicPort),
		Instructions: fmt.Sprintf("Open the tunnel, then browse https://127.0.0.1:%d and log in with the "+
			"operator credentials set during install (mythic-cli config). Drive tasks from the UI.", mythicPort),
	}, nil
}
