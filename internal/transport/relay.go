package transport

import (
	"strings"

	"github.com/pion/ice/v4"
	piontransport "github.com/pion/transport/v5"
	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/webrtc/v4"
)

// The pinned stdnet.Net zero value delegates socket, DNS and dialer methods to
// Go's net package, with an empty cached interface list. Embedding it in a
// distinct type also prevents ICE's *stdnet.Net refresh from calling Netlink.
// No synthetic interface or guessed host address is advertised. TURN gathering
// without interface/IP filters binds wildcard sockets and lets the OS route.
// This adapter must only be used with relay-only policy and mDNS disabled.
type interfaceFreeRelayNet struct{ piontransport.Net }

func configureInterfaceFreeRelay(s *webrtc.SettingEngine) {
	s.SetNet(&interfaceFreeRelayNet{Net: &stdnet.Net{}})
	s.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
}

func authenticatedTURN(servers []webrtc.ICEServer) bool {
	for _, server := range servers {
		password, ok := server.Credential.(string)
		if !ok || password == "" || server.Username == "" || server.CredentialType != webrtc.ICECredentialTypePassword {
			continue
		}
		for _, url := range server.URLs {
			if strings.HasPrefix(url, "turn:") || strings.HasPrefix(url, "turns:") {
				return true
			}
		}
	}
	return false
}

func hasRelayCandidate(sdp string) bool {
	for line := range strings.SplitSeq(sdp, "\r\n") {
		if !strings.HasPrefix(line, "a=candidate:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 8 && fields[6] == "typ" && fields[7] == "relay" {
			return true
		}
	}
	return false
}
