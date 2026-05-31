package master

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type ProcessManager struct {
	mu      sync.Mutex
	procs   map[string]*exec.Cmd
	dataDir string
}

func NewProcessManager(dataDir string) *ProcessManager {
	return &ProcessManager{procs: map[string]*exec.Cmd{}, dataDir: dataDir}
}

func (m *ProcessManager) Start(ctx context.Context, id string, port int, dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cmd := m.procs[id]; cmd != nil && cmd.Process != nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(dir, "pockethost.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, exe,
		"tenant",
		"--dir", dir,
		"--http", "127.0.0.1:"+strconv.Itoa(port),
	)
	cmd.Dir = filepath.Dir(exe)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start tenant: %w", err)
	}
	m.procs[id] = cmd
	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
		_ = logFile.Close()
		m.mu.Lock()
		if m.procs[id] == cmd {
			delete(m.procs, id)
		}
		m.mu.Unlock()
	}()
	if err := waitForPort(ctx, "127.0.0.1:"+strconv.Itoa(port), exited, 5*time.Second); err != nil {
		delete(m.procs, id)
		_ = cmd.Process.Kill()
		return err
	}
	return nil
}

func (m *ProcessManager) Stop(id string) error {
	m.mu.Lock()
	cmd := m.procs[id]
	delete(m.procs, id)
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func waitForPort(ctx context.Context, addr string, exited <-chan error, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		select {
		case err := <-exited:
			if err != nil {
				return fmt.Errorf("tenant exited before listening: %w", err)
			}
			return fmt.Errorf("tenant exited before listening")
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("tenant did not start listening on %s", addr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
