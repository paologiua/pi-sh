package ttymode

import (
	"reflect"
	"testing"
)

func TestModeVocabularyRejectsInjectionAndInvalidValues(t *testing.T) {
	for _, s := range []*Settings{
		{Flags: map[string]bool{"echo; printf injected": true}},
		{Characters: map[string]uint32{"intr;printf": 3}},
		{Characters: map[string]uint32{"eof": 256}},
		{CharacterSize: 9}, {CharacterSize: -1},
		{Baud: &Baud{Input: 9601, Output: 9600}},
	} {
		if _, err := s.SttyArgs(); err == nil {
			t.Fatal("invalid mode accepted")
		}
	}
}

func TestModeEncodingOwnsValuesAndPreservesOmission(t *testing.T) {
	var absent *Settings
	if args, err := absent.SttyArgs(); err != nil || len(args) != 0 {
		t.Fatal(args, err)
	}
	s := &Settings{Flags: map[string]bool{"echo": false, "icanon": true}, Characters: map[string]uint32{"intr": 29, "eof": 255, "quit": 0, "erase": 127}}
	args, err := s.SttyArgs()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-echo", "icanon", "eof", "undef", "erase", "^?", "intr", "^]", "quit", "^@"}
	if !reflect.DeepEqual(args, want) {
		t.Fatal(args)
	}
	s.Characters["intr"] = 3
	if !reflect.DeepEqual(args, want) {
		t.Fatal("mode arguments alias mutable settings")
	}
}

func TestLinuxBaudReferenceFallback(t *testing.T) {
	for _, rate := range []uint32{0, 50, 134, 9600, 19200, 115200, 230400} {
		if LinuxBaud(rate) != rate {
			t.Fatal("supported reference rate changed", rate)
		}
	}
	for _, rate := range []uint32{7200, 14400, 28800, 76800, 460800, 1000000} {
		if LinuxBaud(rate) != 9600 {
			t.Fatal("reference Linux fallback changed", rate)
		}
	}
}
