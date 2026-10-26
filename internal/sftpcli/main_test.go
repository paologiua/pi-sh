package sftpcli

import (
	"os"
	"testing"

	"pi-sh/internal/account"
)

// TestMain isolates tests from the developer's own default account
// (PI_SH_ACCOUNT or ~/.config/pi-sh/account); an empty value disables both.
func TestMain(m *testing.M) {
	os.Setenv(account.EnvName, "")
	os.Exit(m.Run())
}
