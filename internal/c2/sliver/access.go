package sliver

import (
	"fmt"

	"github.com/rinfra/rinfra/internal/c2"
)

// ManualAccess lets an operator drive Sliver by hand with the native
// sliver-client over an mTLS gRPC tunnel, instead of the automated Operator.
func (p *provider) ManualAccess(ts c2.Teamserver) (c2.ManualAccess, error) {
	return c2.ManualAccess{
		Framework:    "sliver",
		Client:       "sliver-client",
		Protocol:     c2.AccessGRPCMTLS,
		OperatorPort: sliverPort,
		Tunnel:       c2.DefaultTunnel(ts, sliverPort),
		Instructions: fmt.Sprintf("Fetch the operator .cfg from the teamserver (/root/.sliver/configs), "+
			"open the tunnel, then `sliver-client import <cfg>` and connect — the client reaches the "+
			"multiplayer listener at 127.0.0.1:%d through the tunnel.", sliverPort),
	}, nil
}
