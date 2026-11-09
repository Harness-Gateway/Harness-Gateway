// Command acceptance runs Spring Boot acceptance against isolated gateways.
// The default suite uses Go CLI fixtures; -live also uses authenticated CLIs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"syscall"
	"time"

	"github.com/harness-gateway/harness-gateway/internal/process"
)

func main() {
	live := flag.Bool("live", false, "also run authenticated Codex/Copilot inference (uses account allowance)")
	models := flag.String("models", "codex,copilot", "comma-separated model IDs for live acceptance")
	nativeModels := flag.String("native-models", "", "explicit native IDs for live override checks (default: one discovered model per harness)")
	gatewayBinary := flag.String("gateway-binary", "", "existing gateway executable to test instead of building it")
	outputDirectory := flag.String("output-dir", "", "parent directory for retained per-run diagnostics (default: retain temporary logs only on failure)")
	soak := flag.Duration("soak", 0, "also run Linux resource soak for this duration per harness (1s to 1h; no inference)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "acceptance does not accept positional arguments")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, *live, *models, *nativeModels, *gatewayBinary, *outputDirectory, *soak); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func command(ctx context.Context, name string, args ...string) error {
	fmt.Fprintln(os.Stderr, "acceptance:", name, args)
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := runCommand(ctx, cmd); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

// Own cancellation explicitly: exec.CommandContext's watcher may otherwise
// signal a group concurrently with Cmd.Wait reaping its leader.
func runCommand(ctx context.Context, cmd *exec.Cmd) error {
	child, err := startCommand(ctx, cmd)
	if err != nil {
		return err
	}
	return waitCommand(ctx, child)
}

func startCommand(ctx context.Context, cmd *exec.Cmd) (*process.Child, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cmd.Cancel != nil {
		return nil, errors.New("acceptance commands require owner-managed cancellation")
	}
	cmd.WaitDelay = 2 * time.Second
	return process.Start(cmd)
}

func waitCommand(ctx context.Context, child *process.Child) error {
	stopOnCancel := context.AfterFunc(ctx, func() { _ = stopCommand(child) })
	waitErr := waitCommandExit(child)
	stopOnCancel()
	return errors.Join(waitErr, child.Stop(), ctx.Err())
}

func run(ctx context.Context, live bool, models, nativeModels, gatewayBinary, outputDirectory string, soak time.Duration) (runErr error) {
	if !live && nativeModels != "" {
		return errors.New("--native-models requires --live")
	}
	if soak < 0 || (soak > 0 && (soak < time.Second || soak > time.Hour)) {
		return errors.New("soak must be zero (disabled) or between 1s and 1h per harness")
	}
	if soak > 0 && runtime.GOOS != "linux" {
		return errors.New("resource soak requires Linux /proc accounting")
	}
	if _, err := os.Stat("examples/spring-ai/pom.xml"); err != nil {
		return errors.New("run acceptance from the repository root")
	}
	directory, err := os.MkdirTemp("", "harness-gateway-acceptance-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	logs, err := createLogsDirectory(outputDirectory)
	if err != nil {
		return err
	}
	defer func() { runErr = finishLogs(logs, outputDirectory != "", runErr) }()
	gateway, err := resolveGateway(ctx, directory, gatewayBinary)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "acceptance: testing gateway executable", gateway)
	paths := map[string]string{}
	for _, harness := range []string{"codex", "copilot"} {
		paths[harness] = filepath.Join(directory, harness+"-fixture")
		if err := command(ctx, "go", "test", "-c", "-o", paths[harness], "./internal/adapters/outbound/"+harness); err != nil {
			return err
		}
	}
	pids := filepath.Join(logs, "pids")
	if err := os.Mkdir(pids, 0700); err != nil {
		return err
	}
	env := append(os.Environ(),
		"HARNESS_GATEWAY_CODEX_TEST=acceptance",
		"HARNESS_GATEWAY_COPILOT_TEST=acceptance",
		"HARNESS_GATEWAY_COPILOT_LOG="+filepath.Join(logs, "copilot-methods"),
		"HARNESS_GATEWAY_COPILOT_PID="+filepath.Join(logs, "copilot-pid"),
		"HARNESS_GATEWAY_ACCEPTANCE_PIDS="+pids)
	failure, err := startGateway(ctx, logs, "failure", gateway, env,
		"--codex-path", paths["codex"], "--copilot-path", paths["copilot"], "-t", "4s", "-c", "1")
	if err != nil {
		return err
	}
	err = command(ctx, "mvn", "--batch-mode", "--file", "examples/spring-ai/pom.xml",
		"-Dtest=GatewayFailureTest,GatewayStressTest,GatewayContractTest,NativeModelsTest", "-Dgateway.failure.url="+failure.url+"/v1",
		"-Dgateway.failure.pids="+pids, "-Dgateway.failure.directory="+logs,
		"-Dgateway.failure.executable="+gateway,
		"-Dgateway.failure.codex="+paths["codex"], "-Dgateway.failure.copilot="+paths["copilot"],
		"-Dsurefire.reportNameSuffix=failure", "test")
	stopErr := failure.stop()
	if err := errors.Join(err, stopErr); err != nil {
		return err
	}
	if soak > 0 {
		observed := filepath.Join(directory, "gateway-observed")
		if err := command(ctx, "go", "test", "-c", "-o", observed, "./cmd/harness-gateway"); err != nil {
			return err
		}
		if err := command(ctx, "mvn", "--batch-mode", "--file", "examples/spring-ai/pom.xml",
			"-Dtest=GatewaySoakTest", "-Dgateway.failure.url="+failure.url+"/v1",
			"-Dgateway.failure.pids="+pids, "-Dgateway.failure.directory="+logs,
			"-Dgateway.failure.executable="+gateway, "-Dgateway.soak.executable="+observed,
			fmt.Sprintf("-Dgateway.soak.seconds=%d", (soak+time.Second-1)/time.Second),
			"-Dgateway.failure.codex="+paths["codex"], "-Dgateway.failure.copilot="+paths["copilot"],
			"-Dsurefire.reportNameSuffix=soak", "test"); err != nil {
			return err
		}
	}
	if live {
		server, err := startGateway(ctx, logs, "live", gateway, os.Environ())
		if err != nil {
			return err
		}
		err = command(ctx, "mvn", "--batch-mode", "--file", "examples/spring-ai/pom.xml",
			"-Dtest=GatewayTest", "-Dgateway.url="+server.url+"/v1", "-Dgateway.models="+models,
			"-Dgateway.native-models="+nativeModels,
			"-Dsurefire.reportNameSuffix=live", "test")
		if err := errors.Join(err, server.stop()); err != nil {
			return err
		}
	}
	fmt.Fprintln(os.Stderr, "acceptance: passed; owned gateways stopped")
	return nil
}

func resolveGateway(ctx context.Context, directory, supplied string) (string, error) {
	if supplied != "" {
		path, err := filepath.Abs(supplied)
		if err != nil {
			return "", fmt.Errorf("gateway binary: %w", err)
		}
		path, err = exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("gateway binary: %w", err)
		}
		return path, nil
	}
	gateway := filepath.Join(directory, "gateway")
	if err := command(ctx, "go", "build", "-o", gateway, "./cmd/harness-gateway"); err != nil {
		return "", err
	}
	return gateway, nil
}

type gatewayProcess struct {
	cmd   *exec.Cmd
	child *process.Child
	done  chan struct{}
	err   error // Read only after done closes.
	log   *os.File
	url   string
}

var listeningURL = regexp.MustCompile(`url=(http://127\.0\.0\.1:[0-9]+)`)

func startGateway(ctx context.Context, directory, name, executable string, env []string, args ...string) (*gatewayProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	log, err := os.Create(filepath.Join(directory, name+".log"))
	if err != nil {
		return nil, err
	}
	p := &gatewayProcess{cmd: exec.Command(executable, append([]string{"-l", "127.0.0.1:0"}, args...)...), done: make(chan struct{}), log: log}
	p.cmd.Env = env
	p.cmd.Stdout, p.cmd.Stderr = log, io.MultiWriter(log, os.Stderr)
	p.child, err = startCommand(ctx, p.cmd)
	if err != nil {
		_ = log.Close()
		return nil, err
	}
	go func() { p.err = p.child.Wait(); close(p.done) }()
	ready, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			_ = log.Close()
			if p.err == nil {
				return nil, errors.New("gateway exited before readiness with status 0")
			}
			return nil, fmt.Errorf("gateway exited before readiness: %w", p.err)
		case <-ready.Done():
			return nil, errors.Join(fmt.Errorf("gateway readiness: %w", ready.Err()), p.stop())
		case <-ticker.C:
			data, err := os.ReadFile(log.Name())
			if err != nil {
				return nil, errors.Join(err, p.stop())
			}
			match := listeningURL.FindSubmatch(data)
			if len(match) == 0 {
				continue
			}
			p.url = string(match[1])
			request, err := http.NewRequestWithContext(ready, http.MethodGet, p.url+"/health", nil)
			if err != nil {
				return nil, errors.Join(err, p.stop())
			}
			response, err := client.Do(request)
			if err != nil {
				continue
			}
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return p, nil
			}
		}
	}
}

func (p *gatewayProcess) stop() error {
	defer p.log.Close()
	select {
	case <-p.done:
		return fmt.Errorf("gateway exited unexpectedly: %v", p.err)
	default:
	}
	var cleanupErr error
	if err := interruptGateway(p.child); err != nil && !errors.Is(err, os.ErrProcessDone) {
		cleanupErr = errors.Join(err, p.child.Stop())
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-p.done:
		return errors.Join(p.err, cleanupErr)
	case <-timer.C:
		cleanupErr = errors.Join(cleanupErr, p.child.Stop())
		<-p.done
		return errors.Join(errors.New("gateway did not stop gracefully within 5s"), cleanupErr)
	}
}
