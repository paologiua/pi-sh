package forwardspec

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Refusal reasons for SSH features without a Connect equivalent. Each is
// documented in docs/FORWARDING.md; only OpenSSH's "off" values are accepted.
var (
	ErrAgentForwarding = errors.New("agent forwarding (-A, ForwardAgent) is not supported: Connect exposes no agent channel, and a remote agent socket would be a prohibited remote filesystem write")
	ErrX11Forwarding   = errors.New("X11 forwarding (-X, -Y, ForwardX11) is not supported: it needs a remote X11 display listener and xauth cookie writes, which pi-sh must not create on the device")
	ErrMultiplexing    = errors.New("connection multiplexing (-M, -S, -O, ControlMaster, ControlPath, ControlPersist) is not supported: pi-sh has no control-socket protocol and each invocation opens its own Connect session; use -o ControlMaster=no -o ControlPath=none to override a configuration file")
	ErrTunnel          = errors.New("tun/tap tunnelling (-w, Tunnel) is not supported: it requires privileged network-interface configuration on the device, which is prohibited")
	ErrProxy           = errors.New("ProxyCommand/ProxyJump (-J) are not supported: pi-sh never opens an SSH TCP connection that a proxy could carry; Connect uses its own HTTPS/WebRTC route (pi-sh -W can itself serve as an ssh ProxyCommand)")
	ErrSubsystem       = errors.New("SessionType subsystem is not supported by pi-sh; use pi-sftp for SFTP")
)

// Features holds the accepted values of refused-feature keywords so -G prints
// them as ssh does.
type Features struct {
	X11Trusted  bool
	SessionNone bool
}

// FeatureKeyword reports keywords handled by ValidateFeature.
func FeatureKeyword(key string) bool {
	switch key {
	case "forwardagent", "forwardx11", "forwardx11trusted", "controlmaster", "controlpath",
		"controlpersist", "tunnel", "proxycommand", "proxyjump", "proxyusefdpass", "sessiontype":
		return true
	}
	return false
}

// ValidateFeature accepts only values that leave the feature off. args are the
// directive's arguments (ProxyCommand may have several).
func ValidateFeature(key string, args []string) error {
	if len(args) == 0 || (len(args) == 1 && args[0] == "") {
		return fmt.Errorf("no argument after keyword \"%s\"", key)
	}
	value := strings.Join(args, " ")
	single := len(args) == 1
	lower := strings.ToLower(value)
	off := single && (lower == "no" || lower == "false")
	switch key {
	case "forwardagent":
		if !off {
			return ErrAgentForwarding
		}
	case "forwardx11":
		if !off {
			if _, ok := Flag(value); !ok || !single {
				return fmt.Errorf("unsupported option \"%s\".", value)
			}
			return ErrX11Forwarding
		}
	case "forwardx11trusted":
		if _, ok := Flag(value); !ok || !single {
			return fmt.Errorf("unsupported option \"%s\".", value)
		}
	case "controlmaster", "controlpersist":
		if !off {
			return ErrMultiplexing
		}
	case "controlpath":
		if !single || lower != "none" {
			return ErrMultiplexing
		}
	case "tunnel":
		if !off {
			return ErrTunnel
		}
	case "proxycommand", "proxyjump":
		if !single || lower != "none" {
			return ErrProxy
		}
	case "proxyusefdpass":
		if !off {
			if _, ok := Flag(value); !ok || !single {
				return fmt.Errorf("unsupported option \"%s\".", value)
			}
			return ErrProxy
		}
	case "sessiontype":
		switch {
		case !single:
			return fmt.Errorf("unsupported option \"%s\".", value)
		case lower == "subsystem":
			return ErrSubsystem
		case lower != "none" && lower != "default":
			return fmt.Errorf("unsupported option \"%s\".", value)
		}
	}
	return nil
}

// Set records an accepted feature value (first-wins is the caller's).
func (f *Features) Set(key, value string) {
	switch key {
	case "forwardx11trusted":
		f.X11Trusted, _ = Flag(value)
	case "sessiontype":
		f.SessionNone = strings.ToLower(value) == "none"
	}
}

// WriteConfig prints the -G lines for these keywords as ssh does when the
// features are off.
func (f Features) WriteConfig(w io.Writer) error {
	trusted, session := "no", "default"
	if f.X11Trusted {
		trusted = "yes"
	}
	if f.SessionNone {
		session = "none"
	}
	_, err := fmt.Fprintf(w, "controlmaster false\ncontrolpersist no\nforwardagent no\nforwardx11 no\nforwardx11trusted %s\nproxyusefdpass no\nsessiontype %s\ntunnel false\n", trusted, session)
	return err
}
