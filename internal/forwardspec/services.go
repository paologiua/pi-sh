package forwardspec

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var services struct {
	once  sync.Once
	ports map[string]int
}

// servicesPath is the local services database consulted by getservbyname.
func servicesPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "services")
	}
	return "/etc/services"
}

// lookupService resolves a TCP service name case-sensitively, like
// getservbyname(3) on the reference platform. Go's net.LookupPort folds case
// and has a built-in table, so it would accept names ssh rejects.
func lookupService(name string) (int, bool) {
	if name == "" {
		return 0, false
	}
	services.once.Do(func() { services.ports = readServices(servicesPath()) })
	port, ok := services.ports[name]
	return port, ok
}

func readServices(path string) map[string]int {
	ports := make(map[string]int)
	f, err := os.Open(path)
	if err != nil {
		return ports
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		words := strings.Fields(line)
		if len(words) < 2 {
			continue
		}
		number, protocol, ok := strings.Cut(words[1], "/")
		if !ok || protocol != "tcp" {
			continue
		}
		port, err := strconv.Atoi(number)
		if err != nil || port < 0 || port > 65535 {
			continue
		}
		for _, name := range append([]string{words[0]}, words[2:]...) {
			if _, exists := ports[name]; !exists {
				ports[name] = port
			}
		}
	}
	return ports
}
