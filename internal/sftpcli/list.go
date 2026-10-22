package sftpcli

import (
	"errors"
	"strings"
)

func listingFlags(args []string) ([]string, bool, error) {
	unsorted := false
	for len(args) > 0 {
		word := args[0]
		if word == "--" {
			return args[1:], unsorted, nil
		}
		if len(word) < 2 || !strings.HasPrefix(word, "-") {
			break
		}
		for _, flag := range word[1:] {
			if flag != 'f' {
				return nil, false, errors.New("unsupported ls option; only -f is implemented")
			}
			unsorted = true
		}
		args = args[1:]
	}
	return args, unsorted, nil
}
