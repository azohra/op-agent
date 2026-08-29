package agent

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	maxFrameSize   = 4 << 20
	protocolWindow = 5 * time.Second
	responseWindow = 2 * time.Minute
)

type Request struct {
	Version int      `json:"version"`
	Action  string   `json:"action"`
	Account string   `json:"account,omitempty"`
	Refs    []string `json:"refs,omitempty"`
	Fresh   bool     `json:"fresh,omitempty"`
}

type Response struct {
	Values   map[string]string `json:"values,omitempty"`
	Entries  int               `json:"entries,omitempty"`
	Accounts int               `json:"accounts,omitempty"`
	Error    string            `json:"error,omitempty"`
}

func writeFrame(conn net.Conn, value any) error {
	if err := conn.SetWriteDeadline(time.Now().Add(protocolWindow)); err != nil {
		return err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode daemon message: %w", err)
	}
	if len(payload) > maxFrameSize {
		return fmt.Errorf("daemon message exceeds %d bytes", maxFrameSize)
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(payload)))
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err = conn.Write(payload)
	return err
}

func readFrame(conn net.Conn, value any) error {
	return readFrameWithin(conn, value, protocolWindow)
}

func readFrameWithin(conn net.Conn, value any, timeout time.Duration) error {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header)
	if size == 0 || size > maxFrameSize {
		return fmt.Errorf("invalid daemon message size %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return err
	}
	if err := json.Unmarshal(payload, value); err != nil {
		return fmt.Errorf("decode daemon message: %w", err)
	}
	return nil
}
