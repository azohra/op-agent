package agent

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
)

func TestProtocolRejectsOversizedFrameBeforeAllocation(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		header := make([]byte, 4)
		binary.BigEndian.PutUint32(header, maxFrameSize+1)
		_, err := client.Write(header)
		done <- err
	}()
	var response Response
	err := readFrame(server, &response)
	if err == nil || !strings.Contains(err.Error(), "invalid daemon message size") {
		t.Fatalf("error = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
