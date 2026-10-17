// cmd/mwb/peer.go
package main

import (
	"net"

	"github.com/lucky-verma/mwb-linux/internal/network"
)

// peerHost is where to dial the peer's file channel: the configured host, or,
// for a Linux peer waiting without one, the address the other machine
// connected from.
func peerHost(configured string, conn *network.Conn) string {
	if configured != "" {
		return configured
	}
	return conn.RemoteHost()
}

// localAddresses lists this machine's local-network addresses, the ones the
// other computer can enter as host to reach a waiting machine.
func localAddresses() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	return localAddressStrings(addrs)
}

func localAddressStrings(addrs []net.Addr) []string {
	var out []string
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		// Private ranges only: those are what a waiting peer accepts from, and
		// a link-local IPv6 address is not something to type into a form.
		if ipn.IP.IsPrivate() {
			out = append(out, ipn.IP.String())
		}
	}
	return out
}
