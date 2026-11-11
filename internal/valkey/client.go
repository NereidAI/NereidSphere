// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

// Package valkey is NereidSphere's deliberately small RESP client for
// ticket replay storage. It supports standalone endpoints and Sentinel master
// discovery with password authentication. The package intentionally exposes
// only the commands required by NS rather than modeling a generic database
// client.
package valkey

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NereidAI/NereidSphere/internal/config"
)

type respKind int

const (
	respStatus respKind = iota
	respError
	respInteger
	respBulk
	respArray
)

type reply struct {
	kind    respKind
	status  string
	integer int64
	bytes   []byte
	array   []reply
	null    bool
}

// Client is a concurrency-safe RESP client that reconnects to the Sentinel-
// reported master after failures.
type Client struct {
	cfg config.ValkeySection

	mu     sync.Mutex
	addr   string
	conn   net.Conn
	reader *bufio.Reader
}

// New builds a client. No network connection is established until the first
// command, so startup remains resilient to transient store unavailability.
func New(cfg config.ValkeySection) (*Client, error) {
	if cfg.Address == "" && len(cfg.Sentinel.Addresses) == 0 {
		return nil, errors.New("valkey address or sentinel addresses are required")
	}
	return &Client{cfg: cfg}, nil
}

// Close releases the active master connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Ready verifies that the current master answers PING.
func (c *Client) Ready(ctx context.Context) error {
	_, err := c.do(ctx, "PING")
	return err
}

// Claim atomically sets key with NX and TTL. In Sentinel mode it requires
// one replica to acknowledge the write before reporting success.
func (c *Client) Claim(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ms := ttl.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	r, err := c.do(ctx, "SET", key, "1", "NX", "PX", strconv.FormatInt(ms, 10))
	if err != nil {
		return false, err
	}
	if r.null {
		return false, nil
	}
	if r.status != "OK" {
		return false, fmt.Errorf("unexpected SET reply %q", r.status)
	}
	if c.cfg.Address != "" {
		return true, nil
	}
	ack, err := c.do(ctx, "WAIT", "1", "300")
	if err != nil {
		return false, err
	}
	if ack.integer < 1 {
		return false, errors.New("valkey write was not acknowledged by a replica")
	}
	return true, nil
}

func (c *Client) do(ctx context.Context, args ...string) (*reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	r, err := c.doLocked(ctx, args...)
	if err == nil {
		return r, nil
	}
	c.closeLocked()
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= 0 {
		return nil, err
	}
	r, retryErr := c.doLocked(ctx, args...)
	if retryErr != nil {
		return nil, retryErr
	}
	return r, nil
}

func (c *Client) doLocked(ctx context.Context, args ...string) (*reply, error) {
	if err := c.connectLocked(ctx); err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(deadline)
	} else {
		_ = c.conn.SetDeadline(time.Now().Add(c.cfg.OperationDuration))
	}
	if err := writeCommand(c.conn, args...); err != nil {
		return nil, err
	}
	r, err := readReply(c.reader)
	if err != nil {
		return nil, err
	}
	if r.kind == respError {
		return nil, errors.New(r.status)
	}
	return r, nil
}

func (c *Client) connectLocked(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	addr := c.cfg.Address
	if addr == "" {
		var err error
		addr, err = c.discoverMasterLocked(ctx)
		if err != nil {
			return err
		}
	}
	dialer := net.Dialer{Timeout: c.cfg.DialDuration}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	c.addr = addr
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(c.cfg.OperationDuration))
	}
	if c.cfg.Password != "" {
		if _, err := c.commandConnectedLocked("AUTH", c.cfg.Password); err != nil {
			_ = conn.Close()
			c.conn = nil
			return err
		}
	}
	if c.cfg.DB > 0 {
		if _, err := c.commandConnectedLocked("SELECT", strconv.Itoa(c.cfg.DB)); err != nil {
			_ = conn.Close()
			c.conn = nil
			return err
		}
	}
	return nil
}

func (c *Client) discoverMasterLocked(ctx context.Context) (string, error) {
	var lastErr error
	for _, sentinel := range c.cfg.Sentinel.Addresses {
		addr, err := c.askSentinelLocked(ctx, sentinel)
		if err == nil {
			return addr, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no sentinel addresses configured")
	}
	return "", fmt.Errorf("discover valkey master: %w", lastErr)
}

func (c *Client) askSentinelLocked(ctx context.Context, sentinel string) (string, error) {
	dialer := net.Dialer{Timeout: c.cfg.DialDuration}
	conn, err := dialer.DialContext(ctx, "tcp", sentinel)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(c.cfg.OperationDuration))
	}
	if c.cfg.Sentinel.Password != "" {
		if err := writeCommand(conn, "AUTH", c.cfg.Sentinel.Password); err != nil {
			return "", err
		}
		if _, err := readReply(reader); err != nil {
			return "", err
		}
	}
	if err := writeCommand(conn, "SENTINEL", "get-master-addr-by-name", c.cfg.Sentinel.MasterName); err != nil {
		return "", err
	}
	r, err := readReply(reader)
	if err != nil {
		return "", err
	}
	if r.null || r.kind != respArray || len(r.array) < 2 {
		return "", errors.New("sentinel did not return a master address")
	}
	host := string(r.array[0].bytes)
	port := string(r.array[1].bytes)
	if host == "" || port == "" {
		return "", errors.New("sentinel returned an empty master address")
	}
	return net.JoinHostPort(host, port), nil
}

func (c *Client) commandConnectedLocked(args ...string) (*reply, error) {
	if err := writeCommand(c.conn, args...); err != nil {
		return nil, err
	}
	return readReply(c.reader)
}

func (c *Client) closeLocked() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.reader = nil
}

func writeCommand(w io.Writer, args ...string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(arg), arg)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func readReply(r *bufio.Reader) (*reply, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, errors.New("empty RESP reply")
	}
	payload := line[1:]
	switch line[0] {
	case '+':
		return &reply{kind: respStatus, status: payload}, nil
	case '-':
		return nil, errors.New(payload)
	case ':':
		value, err := strconv.ParseInt(payload, 10, 64)
		if err != nil {
			return nil, err
		}
		return &reply{kind: respInteger, integer: value}, nil
	case '$':
		length, err := strconv.Atoi(payload)
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return &reply{kind: respBulk, null: true}, nil
		}
		data := make([]byte, length+2)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, err
		}
		return &reply{kind: respBulk, bytes: data[:length]}, nil
	case '*':
		count, err := strconv.Atoi(payload)
		if err != nil {
			return nil, err
		}
		if count < 0 {
			return &reply{kind: respArray, null: true}, nil
		}
		out := make([]reply, count)
		for i := range out {
			nested, err := readReply(r)
			if err != nil {
				return nil, err
			}
			out[i] = *nested
		}
		return &reply{kind: respArray, array: out}, nil
	default:
		return nil, fmt.Errorf("unsupported RESP prefix %q", line[0])
	}
}
