// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

package valkey

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NereidAI/NereidSphere/internal/config"
)

type fakeValkey struct {
	ln       net.Listener
	waitAcks int64
	password string
	mu       sync.Mutex
	keys     map[string]bool
}

func newFakeValkey(t *testing.T, waitAcks int64) *fakeValkey {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeValkey{ln: ln, waitAcks: waitAcks, keys: map[string]bool{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeValkey) addr() string { return f.ln.Addr().String() }

func (f *fakeValkey) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	for {
		cmd, err := readCommand(r)
		if err != nil {
			return
		}
		switch strings.ToUpper(cmd[0]) {
		case "PING":
			_, _ = io.WriteString(conn, "+PONG\r\n")
		case "AUTH":
			if f.password == "" || cmd[1] == f.password {
				_, _ = io.WriteString(conn, "+OK\r\n")
			} else {
				_, _ = io.WriteString(conn, "-ERR invalid password\r\n")
			}
		case "SELECT":
			_, _ = io.WriteString(conn, "+OK\r\n")
		case "SENTINEL":
			host, port, _ := net.SplitHostPort(f.addr())
			_, _ = fmt.Fprintf(conn, "*2\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n", len(host), host, len(port), port)
		case "WAIT":
			_, _ = fmt.Fprintf(conn, ":%d\r\n", f.waitAcks)
		case "SET":
			f.mu.Lock()
			if f.keys[cmd[1]] {
				_, _ = io.WriteString(conn, "$-1\r\n")
			} else {
				f.keys[cmd[1]] = true
				_, _ = io.WriteString(conn, "+OK\r\n")
			}
			f.mu.Unlock()
		default:
			_, _ = io.WriteString(conn, "-ERR unknown command\r\n")
		}
	}
}

func readCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line[0] != '*' {
		return nil, fmt.Errorf("expected array, got %q", line)
	}
	count, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	out := make([]string, count)
	for i := range out {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		header = strings.TrimSpace(header)
		if header[0] != '$' {
			return nil, fmt.Errorf("expected bulk, got %q", header)
		}
		length, err := strconv.Atoi(header[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, length+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		out[i] = string(buf[:length])
	}
	return out, nil
}

func testConfig() config.ValkeySection {
	return config.ValkeySection{DialDuration: time.Second, OperationDuration: 2 * time.Second}
}

func TestStandaloneClaimAcrossClients(t *testing.T) {
	f := newFakeValkey(t, 0)
	cfg := testConfig()
	cfg.Address = f.addr()
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := first.Claim(t.Context(), "ticket-1", 30*time.Second)
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v", claimed, err)
	}
	claimed, err = second.Claim(t.Context(), "ticket-1", 30*time.Second)
	if err != nil || claimed {
		t.Fatalf("second claim = %v, %v", claimed, err)
	}
	if err := first.Ready(t.Context()); err != nil {
		t.Fatalf("ready: %v", err)
	}
}

func TestStandaloneAuthenticates(t *testing.T) {
	f := newFakeValkey(t, 0)
	f.password = "secret"
	cfg := testConfig()
	cfg.Address = f.addr()
	cfg.Password = "secret"
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Ready(t.Context()); err != nil {
		t.Fatalf("ready with valid password: %v", err)
	}
}

func TestStandaloneRejectsInvalidPassword(t *testing.T) {
	f := newFakeValkey(t, 0)
	f.password = "secret"
	cfg := testConfig()
	cfg.Address = f.addr()
	cfg.Password = "wrong"
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Ready(t.Context()); err == nil || !strings.Contains(err.Error(), "invalid password") {
		t.Fatalf("error = %v, want invalid password", err)
	}
}

func TestSentinelClaimRequiresReplicaAck(t *testing.T) {
	f := newFakeValkey(t, 1)
	cfg := testConfig()
	cfg.Sentinel.Addresses = []string{f.addr()}
	cfg.Sentinel.MasterName = "mymaster"
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := client.Claim(t.Context(), "ticket-2", 30*time.Second)
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
}

func TestSentinelWriteFailsWithoutReplicaAck(t *testing.T) {
	f := newFakeValkey(t, 0)
	cfg := testConfig()
	cfg.Sentinel.Addresses = []string{f.addr()}
	cfg.Sentinel.MasterName = "mymaster"
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Claim(t.Context(), "ticket-3", 30*time.Second); err == nil {
		t.Fatal("expected WAIT acknowledgement failure")
	}
}

func TestSentinelUnavailable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	cfg := testConfig()
	cfg.Sentinel.Addresses = []string{addr}
	cfg.Sentinel.MasterName = "mymaster"
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Ready(t.Context()); err == nil {
		t.Fatal("expected sentinel discovery failure")
	}
}
