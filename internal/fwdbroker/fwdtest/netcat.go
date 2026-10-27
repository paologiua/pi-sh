// Package fwdtest holds test doubles shared by forwarding tests.
package fwdtest

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
)

// Netcat is an OpenBSD-netcat test double for the forwarding adapter: it
// accepts the connector argv pi-sh builds (-N, -v, -U, --) and relays stdio to
// one connection with half-close. Tests run their own binary under the name
// "nc" and call it from TestMain.
func Netcat(args []string) int {
	verbose, unix := false, false
	var operands []string
	for i, a := range args {
		if a == "--" {
			operands = args[i+1:]
			break
		}
		switch a {
		case "-h":
			fmt.Fprintln(os.Stderr, "OpenBSD netcat (pi-sh test double)")
			return 1
		case "-v":
			verbose = true
		case "-U":
			unix = true
		case "-N":
		default:
			return 2
		}
	}
	var conn net.Conn
	var err error
	if unix && len(operands) == 1 {
		conn, err = net.Dial("unix", operands[0])
	} else if len(operands) == 2 {
		conn, err = net.Dial("tcp", net.JoinHostPort(operands[0], operands[1]))
	} else {
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "nc: connect to %s failed: Connection refused\n", strings.Join(operands, " port "))
		return 1
	}
	if verbose && !unix {
		fmt.Fprintf(os.Stderr, "Connection to %s %s port [tcp/*] succeeded!\n", operands[0], operands[1])
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(conn, os.Stdin)
		conn.(interface{ CloseWrite() error }).CloseWrite()
	}()
	go func() {
		defer wg.Done()
		io.Copy(os.Stdout, conn)
		os.Stdout.Close()
	}()
	wg.Wait()
	return 0
}
