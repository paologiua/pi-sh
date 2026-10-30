package sftpcli

import _ "embed"

// This UI text reproduces the observable OpenSSH 10.2 command interface.
// It is not an imported implementation or a runtime dependency on OpenSSH.
//
//go:embed help.txt
var consoleHelp string
