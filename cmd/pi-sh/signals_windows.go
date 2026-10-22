package main

import "os"

// Console window close/logoff events need separate native runtime verification.
func terminationSignals() []os.Signal { return []os.Signal{os.Interrupt} }
