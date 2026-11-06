package aiprice

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateFileLockAndRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	if err := New("USD").Save(path); err != nil {
		t.Fatal(err)
	}
	err := WithFileLock(context.Background(), path, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		return UpdateFile(ctx, path, func(*Config) error { return nil })
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected lock timeout, got %v", err)
	}
	sentinel := errors.New("abort")
	err = UpdateFile(context.Background(), path, func(c *Config) error { c.Agents["bad"] = Agent{Name: "Not saved"}; return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Agents) != 0 {
		t.Fatal("aborted edit was saved")
	}
	err = UpdateFile(context.Background(), path, func(c *Config) error { return c.SetAgent("ok", Agent{Name: "Saved"}) })
	if err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil || c.Agents["ok"].Name != "Saved" {
		t.Fatalf("load: %v, %+v", err, c)
	}
	if _, err = os.Stat(path + ".lock"); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
}

func TestLockProcessHelper(t *testing.T) {
	if os.Getenv("AIPRICE_LOCK_HELPER") != "1" {
		return
	}
	err := WithFileLock(context.Background(), os.Getenv("AIPRICE_LOCK_PATH"), func() error { fmt.Println("LOCKED"); select {} })
	if err != nil {
		t.Fatal(err)
	}
}
func TestLockReleasedAfterProcessExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	if err := New("USD").Save(path); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockProcessHelper$")
	cmd.Env = append(os.Environ(), "AIPRICE_LOCK_HELPER=1", "AIPRICE_LOCK_PATH="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line := make(chan string, 1)
	go func() {
		s := bufio.NewScanner(stdout)
		if s.Scan() {
			line <- s.Text()
		} else {
			line <- ""
		}
	}()
	select {
	case got := <-line:
		if got != "LOCKED" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("helper failed: %q", got)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("helper timed out")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = UpdateFile(ctx, path, func(c *Config) error { return c.SetAgent("recovered", Agent{Name: "Recovered"}) }); err != nil {
		t.Fatal(err)
	}
}
