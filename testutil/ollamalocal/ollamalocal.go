package ollamalocal

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// envBaseURL, when set, points the tests at an existing OpenAI-compatible
	// backend; no container is started. envModel overrides the model to request.
	envBaseURL = "TESTMAKER_LLM_BASE_URL"
	envModel   = "TESTMAKER_LLM_MODEL"

	containerPrefix = "testmaker-ollama-"
	containerPort   = "11434" // Ollama's fixed in-container API port
	volumeName      = "testmaker-ollama-models"
	defaultImage    = "ollama/ollama"
	// defaultModel is a tiny (~0.4 GB) chat model; enough to prove wire mapping.
	// Override with WithModel or TESTMAKER_LLM_MODEL (e.g. "smollm2:135m").
	defaultModel = "qwen2.5:0.5b"

	runTimeout     = 60 * time.Second
	removeTimeout  = 30 * time.Second
	readyTimeout   = 60 * time.Second
	inspectTimeout = 10 * time.Second
	// imagePullTimeout and pullTimeout are generous: the first run downloads the
	// image (~1.5 GB) and the model over the network. Both are cached (image in
	// the local store, model in a named volume), so repeat runs skip the wait.
	imagePullTimeout = 10 * time.Minute
	pullTimeout      = 10 * time.Minute
)

type options struct {
	image        string
	model        string
	cleanOrphans bool
}

var (
	mu        sync.Mutex
	resolved  bool
	endpoint  string
	cleanupFn func()
	initErr   error
	opts      options
)

// Option configures the Ollama test infrastructure.
type Option func(*options)

// WithImage overrides the default Docker image ("ollama/ollama").
func WithImage(image string) Option { return func(o *options) { o.image = image } }

// WithModel overrides the model that Model reports and startContainer pulls.
func WithModel(model string) Option { return func(o *options) { o.model = model } }

// WithCleanOrphans removes leftover testmaker-ollama-* containers before start.
func WithCleanOrphans(enabled bool) Option {
	return func(o *options) { o.cleanOrphans = enabled }
}

// Configure applies options before the container is started. Must be called
// before the first Endpoint call (typically from TestMain).
func Configure(fns ...Option) {
	mu.Lock()
	defer mu.Unlock()
	if resolved {
		return
	}
	for _, fn := range fns {
		fn(&opts)
	}
}

// Endpoint returns the OpenAI-compatible base URL ("http://127.0.0.1:<port>/v1").
//
// On first call it honours TESTMAKER_LLM_BASE_URL; if unset it starts an Ollama
// container and pulls the model. The test is skipped under -short, when Docker
// is unavailable, or when the backend cannot be brought up.
//
// The very first run downloads the Ollama image (~1.5 GB) and the model, which
// can exceed Go's default 10m test timeout; startup is bounded by the test's own
// deadline (see budgetContext) so it t.Skips gracefully rather than panicking.
// For a guaranteed cold run use a generous -timeout (e.g. 25m); subsequent runs
// reuse the cached image and the models volume and are fast.
func Endpoint(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping LLM integration backend under -short")
	}

	mu.Lock()
	defer mu.Unlock()

	if !resolved {
		resolved = true
		if url := strings.TrimSpace(os.Getenv(envBaseURL)); url != "" {
			endpoint = url
		} else {
			ctx, cancel := budgetContext(t)
			endpoint, cleanupFn, initErr = startContainer(ctx)
			cancel()
		}
	}
	if initErr != nil {
		t.Skipf("Ollama backend unavailable: %v", initErr)
	}
	return endpoint
}

// Model returns the model name the backend was told to load. Precedence:
// WithModel, then TESTMAKER_LLM_MODEL, then the built-in default.
func Model(t testing.TB) string {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	return resolvedModel()
}

// Shutdown stops the container. Safe to call multiple times (e.g. from TestMain).
func Shutdown() {
	mu.Lock()
	defer mu.Unlock()
	if cleanupFn != nil {
		cleanupFn()
		cleanupFn = nil
	}
}

func resolvedModel() string {
	if opts.model != "" {
		return opts.model
	}
	if m := strings.TrimSpace(os.Getenv(envModel)); m != "" {
		return m
	}
	return defaultModel
}

func imageName() string {
	if opts.image != "" {
		return opts.image
	}
	return defaultImage
}

func startContainer(ctx context.Context) (base string, cleanup func(), err error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return "", nil, fmt.Errorf("docker not found: %w", err)
	}
	// Bail before any docker work if the startup budget is already spent, so a
	// short or nearly-expired test deadline degrades into a graceful skip rather
	// than blocking on background docker calls past the test-timeout killer.
	if err := ctx.Err(); err != nil {
		return "", nil, fmt.Errorf("startup budget exhausted: %w", err)
	}
	if opts.cleanOrphans {
		removeOrphans()
	}

	port, err := freePort()
	if err != nil {
		return "", nil, fmt.Errorf("find free port: %w", err)
	}
	name := fmt.Sprintf("%s%d", containerPrefix, port)
	cleanup = func() { _, _ = dockerRun(context.Background(), removeTimeout, "rm", "-f", name) }

	if err := ensureImage(ctx); err != nil {
		return "", nil, err
	}

	if out, err := dockerRun(ctx, runTimeout, "run", "-d",
		"--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:%s", port, containerPort),
		"-v", volumeName+":/root/.ollama",
		imageName(),
	); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("docker run: %w\n%s", err, out)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitForHTTP(ctx, baseURL+"/api/version", readyTimeout); err != nil {
		logFailure(name)
		cleanup()
		return "", nil, fmt.Errorf("ollama not ready: %w", err)
	}
	if out, err := dockerRun(ctx, pullTimeout, "exec", name, "ollama", "pull", resolvedModel()); err != nil {
		logFailure(name)
		cleanup()
		return "", nil, fmt.Errorf("pull model %q: %w\n%s", resolvedModel(), err, out)
	}
	return baseURL + "/v1", cleanup, nil
}

// ensureImage pulls the Ollama image only when it is not already in the local
// store, so repeat runs (and offline runs after the first) skip the download.
func ensureImage(ctx context.Context) error {
	if _, err := dockerRun(ctx, inspectTimeout, "image", "inspect", imageName()); err == nil {
		return nil // already present
	}
	if out, err := dockerRun(ctx, imagePullTimeout, "pull", imageName()); err != nil {
		return fmt.Errorf("pull image %q: %w\n%s", imageName(), err, out)
	}
	return nil
}

// dockerRun runs `docker args...` bounded by the smaller of the caller's context
// deadline and timeout, and returns combined output — so neither a stuck daemon
// nor a cold pull can overrun the test-timeout killer.
func dockerRun(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(cctx, "docker", args...).CombinedOutput()
	if cctx.Err() != nil {
		return out, fmt.Errorf("docker %v stopped after %s: %w", args, timeout, cctx.Err())
	}
	if err != nil {
		return out, fmt.Errorf("docker %v: %w", args, err)
	}
	return out, nil
}

func waitForHTTP(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("startup cancelled: %w", err)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("not ready within %s: %w", timeout, lastErr)
}

func removeOrphans() {
	out, err := dockerRun(context.Background(), removeTimeout, "ps", "-aq", "--filter", "name="+containerPrefix)
	if err != nil || len(out) == 0 {
		return
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	if len(ids) > 0 {
		_, _ = dockerRun(context.Background(), removeTimeout, append([]string{"rm", "-f"}, ids...)...)
	}
}

func logFailure(name string) {
	out, _ := dockerRun(context.Background(), removeTimeout, "logs", "--tail", "50", name)
	if len(out) > 0 {
		fmt.Fprintf(os.Stderr, "--- docker logs %s ---\n%s\n--- end ---\n", name, out)
	}
}

// cleanupMargin is reserved before the test's own deadline so that teardown
// after a startup abort finishes before Go's test-timeout killer fires. On the
// failure path startContainer runs logFailure then cleanup — two background
// docker calls each bounded by removeTimeout — so the margin must exceed their
// sum. startContainer also bails before any docker work once ctx is expired, so
// the pre-start path cannot overrun the deadline.
const cleanupMargin = 2*removeTimeout + 15*time.Second // 75s

// budgetContext returns a context bounded by the test's deadline minus
// cleanupMargin when the TB exposes Deadline(), reserving time for container
// teardown/Shutdown before Go's test-timeout killer fires. Under -timeout 0 (no
// deadline) it is unbounded and only the fixed per-step timeouts apply.
func budgetContext(t testing.TB) (context.Context, context.CancelFunc) {
	if d, ok := t.(interface{ Deadline() (time.Time, bool) }); ok {
		if dl, has := d.Deadline(); has {
			return context.WithDeadline(context.Background(), dl.Add(-cleanupMargin))
		}
	}
	return context.WithCancel(context.Background())
}

// Context returns a context for backend calls (e.g. Generate) against the
// provisioned backend, bounded by the test's own deadline minus cleanupMargin so
// a slow response surfaces as context.DeadlineExceeded (which the caller can skip
// on) while leaving room for TestMain's Shutdown to remove the container before
// Go's test-timeout killer fires. Cancellation is registered via t.Cleanup.
func Context(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := budgetContext(t)
	t.Cleanup(cancel)
	return ctx
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("listen: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}
