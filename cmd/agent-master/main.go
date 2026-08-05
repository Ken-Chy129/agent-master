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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/config"
	"github.com/Ken-Chy129/agent-master/internal/logging"
	"github.com/Ken-Chy129/agent-master/internal/provider"
	"github.com/Ken-Chy129/agent-master/internal/server"
	"github.com/Ken-Chy129/agent-master/internal/service"
	"github.com/Ken-Chy129/agent-master/internal/session"
	"github.com/Ken-Chy129/agent-master/internal/shellenv"
	"github.com/Ken-Chy129/agent-master/internal/store"
	"github.com/Ken-Chy129/agent-master/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		// doctor has already printed a full report; adding "error:" on top would
		// only bury it. It just needs the non-zero exit status.
		if !errors.Is(err, errSilent) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
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
	case "doctor":
		return cmdDoctor(args[1:])
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
		// Don't dump the whole usage page for a typo — the error scrolls out of
		// sight above it. One line plus where to look is more useful.
		return fmt.Errorf("未知命令 %q，运行 agent-master help 查看可用命令", args[0])
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
	// launchd/systemd start us without sourcing ~/.zshrc.
	//
	// Failing here must not exit non-zero: the service definitions use
	// KeepAlive/Restart=on-failure, so that would crash-loop the daemon and take
	// the diagnostics down with it. Instead keep serving, retry in the
	// background, and let Send refuse runs while the credentials a previous boot
	// resolved are missing — see the shellenv package docs.
	shellenv.SetOptional(cfg.ShellEnvOptional)
	logShellEnv(shellenv.Resolve(cfg.ShellEnvKeys))
	rememberShellEnvKeys(cfg, shellenv.Current())
	shellenv.StartHealing(cfg.ShellEnvKeys, func([]string) {
		st := shellenv.Current()
		logShellEnv(st)
		rememberShellEnvKeys(cfg, st)
	})

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

	// Report up front whether the CLI has anything to authenticate with. This is
	// the same lookup claude performs, so "no credentials" is knowable now rather
	// than as a per-message `Not logged in · Please run /login` that says nothing
	// about the daemon's environment. A warning, not a gate: the lookup can't
	// cover every valid setup (exotic gateways), so refusing runs on it would
	// trade a confusing error for a wrong outage.
	if auth := provider.ClaudeAuth(); !auth.OK {
		slog.Warn("claude has no credentials on this machine; runs will fail",
			"baseUrl", auth.BaseURL, "hint", auth.Hint)
	} else {
		slog.Info("claude credentials resolved", "source", auth.Source, "baseUrl", auth.BaseURL)
	}

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

// logShellEnv reports a shell-env resolution at a severity matching its actual
// consequence. The distinction that matters is not "did the probe work" but
// "would claude now use different credentials than the user's terminal": the
// first is routine on a machine with no exported keys, the second silently
// redirects every request and is what went unnoticed for two weeks.
func logShellEnv(st shellenv.Status) {
	switch {
	case len(st.MissingAuth) > 0:
		slog.Error("login-shell credentials are missing; claude runs are blocked to avoid silently using a different account",
			"missing", strings.Join(st.MissingAuth, ","),
			"probe_ok", st.OK, "attempts", st.Attempts, "err", st.LastError,
			"hint", "fix your shell rc file, or set shell_env_optional:true in ~/.agent-master/config.json to allow claude's own credential lookup")
	case !st.OK:
		slog.Warn("login-shell env probe failed; claude will use its own credential lookup",
			"shell", st.Shell, "attempts", st.Attempts, "err", st.LastError)
	case len(st.Imported) > 0:
		slog.Info("imported env from login shell", "shell", st.Shell, "keys", strings.Join(st.Imported, ","))
	default:
		// A probe that succeeds and finds nothing used to log nothing at all,
		// which is how probing the wrong shell stayed invisible: sh exits 0 and
		// skips the user's rc files, so it looks identical to a machine that
		// genuinely exports no credentials. Always say which shell was asked.
		slog.Warn("login shell exported no ANTHROPIC_*/CLAUDE_* variables; claude will use its own credential lookup",
			"shell", st.Shell)
	}
}

// rememberShellEnvKeys persists the variable NAMES a successful probe produced,
// so a later boot can tell a missing credential from a machine that never had
// one. Values are never written — only names.
//
// The baseline is deliberately not updated while a credential is missing: doing
// so would disarm the guard on the very boot that detected the problem, which is
// exactly the silent-divergence failure this is meant to catch. A user who
// really did remove the variable clears the warning with shell_env_optional.
func rememberShellEnvKeys(cfg *config.Config, st shellenv.Status) {
	if !st.OK || len(st.MissingAuth) > 0 || slices.Equal(cfg.ShellEnvKeys, st.Imported) {
		return
	}
	cfg.ShellEnvKeys = st.Imported
	if err := cfg.Save(); err != nil {
		slog.Warn("persist shell env keys", "err", err)
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
		fmt.Println("✓ agent-master 已在运行")
		printConnectInfo(cfg)
		return nil
	}

	if err := service.Install(); err != nil {
		return err
	}
	// Installing only means the service manager accepted the job — launchd
	// bootstraps happily and a daemon that exits a second later leaves nothing
	// but a log line. Wait for it to actually answer before claiming success, so
	// a failed start is a visible error instead of a ✓ the user trusts.
	if _, ok := waitHealthy(cfg.Port, startupGrace); !ok {
		return startupFailure(cfg.Port)
	}
	fmt.Println("✓ agent-master 正在运行")
	warnIfDegraded(cfg)
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
	writeConnectInfo(os.Stdout, cfg)
}

func writeConnectInfo(w io.Writer, cfg *config.Config) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Web 界面  http://127.0.0.1:%d\n", cfg.Port)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "在桌面端或其他浏览器中添加这台机器：")
	fmt.Fprintf(w, "  地址    %s\n", candidateBaseURLs(cfg)[0])
	fmt.Fprintf(w, "  令牌    %s\n", cfg.Token)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "更多地址与配对二维码：agent-master pair")
}

func cmdStop(_ []string) error {
	if err := service.Stop(); err != nil {
		return err
	}
	fmt.Println("✓ agent-master 已停止")
	return nil
}

func cmdRestart(_ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := service.Restart(); err != nil {
		return err
	}
	// Same false-✓ hazard as start: confirm the new process is serving.
	if _, ok := waitHealthy(cfg.Port, startupGrace); !ok {
		return startupFailure(cfg.Port)
	}
	fmt.Println("✓ agent-master 已重启")
	warnIfDegraded(cfg)
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
		return errors.New("用法：agent-master service <install|uninstall|status|start|stop|restart>")
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
func usage() { writeUsage(os.Stdout) }

// Both help pages keep every line well inside a standard terminal: CJK glyphs are
// double-width, so a line that looks short in source can overflow and wrap
// mid-token — `agent-master help --all` once broke into "help -" / "-all", which
// is neither readable nor copyable. TestHelpLinesFitATerminal guards the width.
func writeUsage(w io.Writer) {
	fmt.Fprint(w, `agent-master —— 在本机运行 Claude Code，从任意设备管理会话

用法
  agent-master <命令>

常用命令
  start     在后台启动（并随开机自启），输出连接方式
  status    查看运行状态与连接方式
  doctor    启动看起来正常但会话失败时，诊断原因并给出修复方式
  pair      输出地址、令牌与配对二维码
  stop      停止运行

更多
  其他命令   restart · uninstall · token · serve · version
  完整帮助   agent-master help --all
  配置与数据 ~/.agent-master/（默认端口 8888）
`)
}

// usageAll is the full grouped reference, including low-frequency and dev
// commands, shown by `agent-master help --all`.
func usageAll() { writeUsageAll(os.Stdout) }

func writeUsageAll(w io.Writer) {
	fmt.Fprint(w, `agent-master —— 在本机运行 Claude Code，从任意设备管理会话

用法
  agent-master <命令> [参数]

安装与运行
  start        在后台启动（并随开机自启），输出连接方式
  status       查看运行状态与连接方式
  doctor       检查会话执行所依赖的前置条件，并给出修复方式
               存在阻塞性问题时退出码为 1，可用于脚本
  stop         停止运行
  restart      重启（升级后需执行，否则仍运行旧版本）
  uninstall    停止并移除后台服务，保留数据与配置

连接客户端
  pair         输出本机地址、令牌与配对二维码
  token        仅输出访问令牌

其他
  serve        前台运行，用于开发与调试
               [--port N] [--host H]
  version      输出版本号
  help --all   显示本页

配置与数据
  ~/.agent-master/    配置、事件账本、日志、上传的图片
  默认端口 8888       仅在可信网络中暴露

诊断入口
  会话失败但 status 显示正常时，先执行 agent-master doctor
`)
}
