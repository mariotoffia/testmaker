package ollamalocal

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestModelResolution checks the precedence WithModel > env > default, since
// that is the one branch a caller relies on to target a specific model.
func TestModelResolution(t *testing.T) {
	t.Run("default when nothing set", func(t *testing.T) {
		resetOptions(t)
		t.Setenv(envModel, "")
		if got := Model(t); got != defaultModel {
			t.Errorf("Model = %q, want default %q", got, defaultModel)
		}
	})

	t.Run("env overrides default", func(t *testing.T) {
		resetOptions(t)
		t.Setenv(envModel, "smollm2:135m")
		if got := Model(t); got != "smollm2:135m" {
			t.Errorf("Model = %q, want env value", got)
		}
	})

	t.Run("WithModel wins over env", func(t *testing.T) {
		resetOptions(t)
		t.Setenv(envModel, "smollm2:135m")
		opts.model = "phi3:mini"
		if got := Model(t); got != "phi3:mini" {
			t.Errorf("Model = %q, want WithModel value", got)
		}
	})
}

// TestBudgetContext guards the deadline math: the budget must end exactly
// cleanupMargin before the test's own deadline (leaving room for teardown), and
// be unbounded when the test has no deadline (-timeout 0). It also pins the
// margin invariant independently of its literal value, so shrinking cleanupMargin
// below the teardown worst case (logFailure + cleanup = 2*removeTimeout) fails
// here rather than silently reintroducing the test-timeout panic.
func TestBudgetContext(t *testing.T) {
	if cleanupMargin <= 2*removeTimeout {
		t.Fatalf("cleanupMargin %v must exceed teardown worst case 2*removeTimeout=%v",
			cleanupMargin, 2*removeTimeout)
	}

	ctx, cancel := budgetContext(t)
	defer cancel()

	dl, ok := t.Deadline()
	got, gotOK := ctx.Deadline()
	switch {
	case ok && (!gotOK || !got.Equal(dl.Add(-cleanupMargin))):
		t.Fatalf("budget deadline = %v (ok=%v), want %v", got, gotOK, dl.Add(-cleanupMargin))
	case !ok && gotOK:
		t.Fatalf("expected no budget deadline when the test has none, got %v", got)
	}
}

// TestStartContainerSkipsDockerWhenBudgetExpired proves the guard added for the
// pre-start path: with an already-expired budget, startContainer must return
// before running any docker command, so a fake docker on PATH is never invoked.
func TestStartContainerSkipsDockerWhenBudgetExpired(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake docker shim is a unix shell script")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "docker-was-called")
	shim := "#!/bin/sh\ntouch " + marker + "\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(shim), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	cancel()
	if _, _, err := startContainer(ctx); err == nil {
		t.Fatal("startContainer should error when the startup budget is already expired")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("startContainer ran docker despite an expired startup budget")
	}
}

// resetOptions clears the package-level option state between subtests and
// restores it afterwards so ordering cannot leak model overrides.
func resetOptions(t *testing.T) {
	t.Helper()
	mu.Lock()
	prev := opts
	opts = options{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		opts = prev
		mu.Unlock()
	})
}
