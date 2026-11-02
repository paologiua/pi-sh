package connectsession

import (
	"context"
	"errors"
	"fmt"
	"io"

	"pi-sh/internal/loginprompt"
	"pi-sh/internal/webauth"
)

// chooser picks one of the listed devices; it is replaceable in tests.
var chooser = func(ctx context.Context, devices []webauth.Device) (int, error) {
	return loginprompt.Choose(ctx, "Raspberry Pi Connect devices:", DeviceLabels(devices))
}

// ChooseDevice lets the person pick the device after sign-in when only the
// account was named (pi-sh email, pi-sftp email, pi-cp email:path) and
// returns it. A single device is selected without asking. Without a terminal
// the list is printed and nothing connects, so a script never opens an
// arbitrary device.
func ChooseDevice(ctx context.Context, source Source, notices io.Writer) (webauth.Device, error) {
	devices, err := Devices(ctx, source)
	if err != nil {
		return webauth.Device{}, err
	}
	var chosen webauth.Device
	switch len(devices) {
	case 0:
		return webauth.Device{}, errors.New("this Raspberry Pi ID account has no Connect devices")
	case 1:
		chosen = devices[0]
		fmt.Fprintf(notices, "Using the only device: %s\n", chosen.Name)
	default:
		i, err := chooser(ctx, devices)
		if err != nil {
			if ctx.Err() != nil {
				return webauth.Device{}, ctx.Err()
			}
			if errors.Is(err, context.Canceled) {
				return webauth.Device{}, errors.New("no device selected")
			}
			for _, label := range DeviceLabels(devices) {
				fmt.Fprintln(notices, label)
			}
			return webauth.Device{}, errors.New("several devices are available; name one as email::device")
		}
		chosen = devices[i]
	}
	return chosen, Select(source, chosen.ID)
}

// DeviceLabels shows names, adding the ID only where names repeat.
func DeviceLabels(devices []webauth.Device) []string {
	count := map[string]int{}
	for _, d := range devices {
		count[d.Name]++
	}
	labels := make([]string, len(devices))
	for i, d := range devices {
		labels[i] = d.Name
		if count[d.Name] > 1 || d.Name == "" {
			labels[i] = fmt.Sprintf("%s (%s)", d.Name, d.ID)
		}
	}
	return labels
}
