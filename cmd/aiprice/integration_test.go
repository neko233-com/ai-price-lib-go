package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	aiprice "github.com/neko233-com/ai-price-lib-go"
)

func TestCLIHelper(t *testing.T) {
	if os.Getenv("AIPRICE_HELPER") != "1" {
		return
	}
	os.Args = []string{"aiprice", "-config", os.Getenv("AIPRICE_PATH"), "put", "agent", os.Getenv("AIPRICE_ID")}
	if err := run(); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentCLIUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	if err := aiprice.New("USD").Save(path); err != nil {
		t.Fatal(err)
	}
	const n = 20
	cmds := make([]*exec.Cmd, n)
	for i := range cmds {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCLIHelper$")
		cmd.Env = append(os.Environ(), "AIPRICE_HELPER=1", "AIPRICE_PATH="+path, fmt.Sprintf("AIPRICE_ID=agent-%d", i))
		cmd.Stdin = strings.NewReader(`{"name":"Agent"}`)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i] = cmd
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	c, err := aiprice.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Agents) != n {
		t.Fatalf("lost updates: got %d agents, want %d", len(c.Agents), n)
	}
}
