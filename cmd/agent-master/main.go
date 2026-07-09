// Command agent-master is the per-machine daemon that controls Claude Code on
// this host and exposes an HTTP API for remote clients (desktop, web, mobile).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/config"
	"github.com/Ken-Chy129/agent-master/internal/logging"
	"github.com/Ken-Chy129/agent-master/internal/provider"
	"github.com/Ken-Chy129/agent-master/internal/server"
	"github.com/Ken-Chy129/agent-master/internal/service"
	"github.com/Ken-Chy129/agent-master/internal/shellenv"
	"github.com/Ken-Chy129/agent-master/internal/session"
	"github.com/Ken-Chy129/agent-master/internal/store"
	"github.com/Ken-Chy129/agent-master/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	// Background service management.
	case "start":
		return cmdStart(args[1:])
	case "stop":
		return cmdStop(args[1:])
	case "restart":
		return cmdRestart(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "uninstall":
		return service.Uninstall()
	// Connecting a client.
	case "pair":
		return cmdPair(args[1:])
	case "token":
		return cmdToken(args[1:])
	// Other.
	case "serve":
		return cmdServe(args[1:])
	case "version", "-v", "--version":
		fmt.Println(version.Version)
		return nil
	case "help", "-h", "--help":
		if len(args) > 1 && (args[1] == "--all" || args[1] == "-a") {
			usageAll()
		} else {
			usage()
		}
		return nil
	// Back-compat alias: `service install|uninstall|status|start|stop|restart`.
	case "service":
		return cmdService(args[1:])
	default:
		usage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fs.Int("port", 0, "override listen port (default: config value)")
	host := fs.String("host", "", "override listen host")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *port != 0 {
		cfg.Port = *port
	}
	if *host != "" {
		cfg.Host = *host
	}

	// Own the daemon log in-process: a size-capped rolling file (bounded,
	// identical across launchd/systemd/Windows), mirrored to stderr so a
	// foreground `serve` still prints to the terminal. Set this up first so even
	// startup logs are captured. AGENT_MASTER_DEBUG=1 surfaces the high-volume
	// per-request access logs (Debug) for troubleshooting.
	level := slog.LevelInfo
	if os.Getenv("AGENT_MASTER_DEBUG") != "" {
		level = slog.LevelDebug
	}
	if logPath, lerr := config.LogPath(); lerr == nil {
		if rw, lerr := logging.NewRollingFile(logPath, 5<<20, 3); lerr == nil {
			defer rw.Close()
			slog.SetDefault(slog.New(slog.NewTextHandler(
				io.MultiWriter(os.Stderr, rw), &slog.HandlerOptions{Level: level})))
			// Also route panic/fatal stacks here, so a crash leaves a trace even
			// when the service manager sends stderr to /dev/null.
			if f := rw.File(); f != nil {
				_ = debug.SetCrashOutput(f, debug.CrashOptions{})
			}
		} else {
			slog.Warn("open daemon log; logging to stderr only", "err", lerr)
		}
	}

	// Import the user's interactive login-shell env (ANTHROPIC_*/CLAUDE_*) so the
	// claude CLI we spawn uses the same auth/endpoint as the user's terminal.
	// launchd/systemd start us without sourcing ~/.zshrc; non-fatal on failure.
	if imported, err := shellenv.Import(); err != nil {
		slog.Warn("shellenv import failed; claude will use its own credential lookup", "err", err)
	} else if len(imported) > 0 {
		slog.Info("imported env from login shell", "keys", strings.Join(imported, ","))
	}

	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	claudeBin := resolveClaudeBin(cfg)
	svc := session.NewService(st, provider.NewClaude(claudeBin))
	// Heal runs orphaned by a previous process's abrupt exit (crash/restart mid-run)
	// before serving, so sessions don't show a permanently-stuck "running" state.
	svc.ReconcileStuckRuns()
	srv := server.New(cfg, st, svc)
	ln, err := srv.Listen()
	if err != nil {
		return err // e.g. port in use — another instance is already serving
	}

	// Now that we hold the port, record our pid so `stop` can find the daemon on
	// platforms without a service manager (Windows). Binding first means a
	// failed duplicate `serve` can never clobber the live daemon's pidfile.
	// Best-effort; the daemon runs fine without it.
	if pidPath, err := config.PIDPath(); err == nil {
		_ = os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
		defer os.Remove(pidPath)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// resolveClaudeBin picks the claude binary: the configured override, else the
// one on PATH, else the bare name "claude" (runs will error clearly if absent).
func resolveClaudeBin(cfg *config.Config) string {
	if cfg.ClaudeBin != "" {
		return cfg.ClaudeBin
	}
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	// Background services (systemd/launchd) run with a minimal PATH that usually
	// omits ~/.local/bin — where the claude CLI commonly installs — so LookPath
	// fails even though claude is present. Probe the usual locations.
	if home, err := os.UserHomeDir(); err == nil {
		for _, p := range claudeCandidates(home) {
			if isExecutableFile(p) {
				return p
			}
		}
	}
	slog.Warn("claude not found on PATH; set claude_bin in config or install claude")
	return "claude"
}

func claudeCandidates(home string) []string {
	if runtime.GOOS == "windows" {
		candidates := []string{
			filepath.Join(home, ".local", "bin", "claude.exe"), // native installer
		}
		// npm -g installs a claude.cmd shim; prefer the native claude.exe — Go
		// (and cmd.exe quoting in general) can reject .cmd arguments containing
		// special characters, which chat messages routinely do.
		if appData := os.Getenv("APPDATA"); appData != "" {
			candidates = append(candidates, filepath.Join(appData, "npm", "claude.cmd"))
		}
		return candidates
	}
	return []string{
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".claude", "local", "claude"),
		filepath.Join(home, "bin", "claude"),
		"/usr/local/bin/claude",
		"/opt/homebrew/bin/claude",
		"/usr/bin/claude",
	}
}

func isExecutableFile(p string) bool {
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return false
	}
	// Windows has no execute mode bits; the probed paths carry explicit
	// executable extensions, so existing as a regular file is enough.
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}

// cmdStart installs + starts the background service, then prints the connection
// info so you can add this machine in a client without a separate `pair` step.
func cmdStart(_ []string) error {
	// Materialize config (and the token) before the daemon starts, so the
	// printed token matches the one the daemon uses (avoids a first-run race).
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Idempotent: if the daemon is already serving this version, don't tear it
	// down and re-bootstrap — just reprint the connection info. Re-running
	// `start` is a common accident and shouldn't surface a scary launchctl
	// error. We still reinstall when it's not responding (never started or
	// crashed) or when a newer binary needs the service reloaded.
	//
	// Dev builds all report the same "0.0.1-dev", so version equality can't tell
	// a rebuilt binary from the running one. Skipping the short-circuit there
	// means `start` always reloads the on-disk binary — otherwise a fresh build
	// would silently keep running the stale daemon.
	if ver, ok := probeHealth(cfg.Port); ok && ver == version.Version && !isDevBuild() {
		fmt.Println("✓ agent-master is already running.")
		printConnectInfo(cfg)
		return nil
	}

	if err := service.Install(); err != nil {
		return err
	}
	fmt.Println("✓ agent-master is running.")
	printConnectInfo(cfg)
	return nil
}

// isDevBuild reports whether this is an unversioned local build, where the
// version string can't distinguish two different binaries — so `start` must not
// treat a matching version as "already up to date".
func isDevBuild() bool {
	return version.Version == "" || strings.Contains(version.Version, "dev")
}

// printConnectInfo prints the shared "how to connect" block used by start.
func printConnectInfo(cfg *config.Config) {
	fmt.Println()
	fmt.Println("Add this machine in your client:")
	fmt.Printf("  URL     %s\n", candidateBaseURLs(cfg)[0])
	fmt.Printf("  Token   %s\n", cfg.Token)
	fmt.Println()
	fmt.Println("More addresses / QR to pair a phone:  agent-master pair")
}

func cmdStop(_ []string) error {
	if err := service.Stop(); err != nil {
		return err
	}
	fmt.Println("✓ agent-master stopped.")
	return nil
}

func cmdRestart(_ []string) error {
	if err := service.Restart(); err != nil {
		return err
	}
	fmt.Println("✓ agent-master restarted.")
	return nil
}

func cmdToken(_ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Println(cfg.Token)
	return nil
}

// cmdService keeps the older `service <sub>` form working as an alias.
func cmdService(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: agent-master service <install|uninstall|status|start|stop|restart>")
	}
	switch args[0] {
	case "install", "start":
		return cmdStart(nil)
	case "uninstall":
		return service.Uninstall()
	case "stop":
		return cmdStop(nil)
	case "restart":
		return cmdRestart(nil)
	case "status":
		return cmdStatus(nil)
	default:
		return fmt.Errorf("unknown service subcommand: %s", args[0])
	}
}

// usage is the short, everyday help: the handful of commands most people use,
// with the rest named on one line and the details behind `help --all`.
func usage() {
	fmt.Print(`agent-master — run Claude Code on this machine, manage it from anywhere.

Usage:
  agent-master <command>

  start     Start in the background (and on boot); prints how to connect
  status    Show whether it's running and how to connect
  pair      Show URL, token, and a QR to add this machine in a client
  stop      Stop it

More:  restart · uninstall · token · serve · version   →  agent-master help --all
Config & data live in ~/.agent-master/  (default port 8888).
`)
}

// usageAll is the full grouped reference, including low-frequency and dev
// commands, shown by `agent-master help --all`.
func usageAll() {
	fmt.Print(`agent-master — run Claude Code on this machine, manage it from anywhere.

Usage:
  agent-master <command> [flags]

Setup:
  start        Start in the background (also on boot); prints how to connect
  status       Show whether it's running and how to connect
  stop         Stop it
  restart      Restart it
  uninstall    Stop and remove the background service

Connect a client:
  pair         Print this machine's URL, token, and a QR to add it in an app
  token        Print just the auth token

Advanced:
  serve        Run in the foreground for dev/debug: [--port N] [--host H]
  version      Print the version

Config & data live in ~/.agent-master/  (default port 8888).
`)
}
