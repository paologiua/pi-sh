package connectsession

import (
	"reflect"
	"testing"

	"pi-sh/internal/webauth"
)

func TestDeviceLabelsDisambiguateRepeatedNames(t *testing.T) {
	got := DeviceLabels([]webauth.Device{{ID: "a", Name: "pi"}, {ID: "b", Name: "pi"}, {ID: "c", Name: "garage"}, {ID: "d"}})
	want := []string{"pi (a)", "pi (b)", "garage", " (d)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %q", got)
	}
}
