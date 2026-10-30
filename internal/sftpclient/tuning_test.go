package sftpclient

import "testing"

func TestTransferXOptions(t *testing.T) {
	var buffer, requests uint
	for _, value := range []string{"buffer=1K", "nrequests=3", "buffer=1.5K"} {
		if err := ApplyTransferOption(value, &buffer, &requests); err != nil {
			t.Fatal(err)
		}
	}
	if buffer != 1536 || requests != 3 {
		t.Fatal(buffer, requests)
	}
	for _, value := range []string{"buffer=0", "buffer=1M", "buffer=nan", "buffer=1/2", "nrequests=0", "nrequests=262145", "unknown=1"} {
		if err := ApplyTransferOption(value, &buffer, &requests); err == nil {
			t.Fatal("accepted", value)
		}
	}
	if buffer != 1536 || requests != 3 {
		t.Fatal("invalid value changed settings")
	}
	if TransferBuffer(256*1024) != MaxTransferBuffer {
		t.Fatal("packet bound")
	}
}
