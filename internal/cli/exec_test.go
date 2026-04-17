package cli

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunnerSuccess(t *testing.T) {
	r := New("echo")
	out, err := r.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "hello" {
		t.Errorf("stdout = %q, want %q", string(out), "hello")
	}
}

func TestRunnerNotFound(t *testing.T) {
	r := New("this-binary-does-not-exist-cloudnav-test")
	_, err := r.Run(context.Background())
	if err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestRunnerFailingCommand(t *testing.T) {
	r := New("sh")
	_, err := r.Run(context.Background(), "-c", "echo failure 1>&2; exit 2")
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "failure") {
		t.Errorf("error = %q, want it to include stderr 'failure'", err.Error())
	}
}

func TestRunnerTimeout(t *testing.T) {
	r := New("sleep")
	r.Timeout = 50 * time.Millisecond
	start := time.Now()
	_, err := r.Run(context.Background(), "5")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > time.Second {
		t.Errorf("took %s, want < 1s", elapsed)
	}
}
