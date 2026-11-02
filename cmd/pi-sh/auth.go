package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"pi-sh/internal/connectsession"
	"pi-sh/internal/connpolicy"
	"pi-sh/internal/loginprompt"
	"pi-sh/internal/webauth"
)

type sessionSource = connectsession.Source

func authentication(ctx context.Context, destination string, direct bool, policy connpolicy.Policy) (sessionSource, error) {
	return connectsession.AuthenticateWithPolicy(ctx, destination, direct, policy, os.Stderr, "pi-sh")
}

// chooser picks one of the listed devices; it is replaceable in tests.
var chooser = func(ctx context.Context, devices []webauth.Device) (int, error) {
	return loginprompt.Choose(ctx, "Raspberry Pi Connect devices:", deviceLabels(devices))
}

// chooseDevice lets the person pick the device after sign-in ("pi-sh email").
// A single device is selected without asking. Without a terminal the list is
// printed and nothing connects, so a script never opens an arbitrary device.
func chooseDevice(ctx context.Context, source sessionSource, notices io.Writer) error {
	devices, err := connectsession.Devices(ctx, source)
	if err != nil {
		return err
	}
	switch len(devices) {
	case 0:
		return errors.New("this Raspberry Pi ID account has no Connect devices")
	case 1:
		fmt.Fprintf(notices, "Using the only device: %s\n", devices[0].Name)
		return connectsession.Select(source, devices[0].ID)
	}
	i, err := chooser(ctx, devices)
	if err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() == nil {
			return errors.New("no device selected")
		}
		if ctx.Err() == nil {
			for _, label := range deviceLabels(devices) {
				fmt.Fprintln(notices, label)
			}
			return errors.New("several devices are available; name one as email::device")
		}
		return err
	}
	return connectsession.Select(source, devices[i].ID)
}

// deviceLabels shows names, adding the ID only where names repeat.
func deviceLabels(devices []webauth.Device) []string {
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
