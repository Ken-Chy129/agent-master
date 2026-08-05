package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/config"
	"github.com/Ken-Chy129/agent-master/internal/service"
	"github.com/Ken-Chy129/agent-master/internal/version"
)

// cmdStatus reports whether the daemon is actually serving by probing its own
// /health endpoint, then prints the connection info. This replaces dumping the
// raw `launchctl print` / `systemctl status` output, which is unreadable.
func cmdStatus(_ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if ver, ok := probeHealth(cfg.Port); ok {
		if ver != "" {
			fmt.Printf("✓ agent-master 正在运行  v%s\n", ver)
		} else {
			fmt.Println("✓ agent-master 正在运行")
		}
		// "Running" is not the same as "able to run claude" — say so here too,
		// since this is where a user looks after something failed.
		warnIfDegraded(cfg)
		fmt.Println()
		fmt.Println("在客户端中添加这台机器：")
		fmt.Printf("  地址    %s\n", candidateBaseURLs(cfg)[0])
		fmt.Printf("  令牌    %s\n", cfg.Token)
		fmt.Println()
		fmt.Println("其他命令：agent-master stop · restart · pair")
		return nil
	}

	// Not responding: distinguish "installed but down" from "never started".
	if service.Installed() {
		fmt.Printf("✗ agent-master 已安装，但端口 %d 无响应\n", cfg.Port)
		fmt.Println()
		fmt.Println("尝试：agent-master restart")
		fmt.Println("查看原因：agent-master doctor")
		return nil
	}
	fmt.Println("✗ agent-master 未在运行")
	fmt.Println()
	fmt.Println("启动：agent-master start")
	return nil
}

// startupGrace bounds how long start/restart wait for the daemon to answer.
// It has to cover the whole boot path: up to 8s resolving the login shell, plus
// opening/migrating the database and reconciling orphaned runs.
const startupGrace = 30 * time.Second

// waitHealthy polls /health until the daemon answers, so start/restart can
// report what actually happened instead of just "the service manager accepted
// the job" — a launchd bootstrap succeeds even when the daemon then exits, which
// is how a broken start could print ✓ and leave the failure only in daemon.log.
//
// For a versioned build it also requires the reported version to match this
// binary: during a restart the outgoing daemon can still be serving for a moment,
// and answering from it would be exactly the false ✓ this is meant to remove.
// Dev builds all report the same string, so there the check is skipped.
func waitHealthy(port int, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	// The wait is normally imperceptible; start printing progress only once it
	// isn't, so a slow rc file doesn't look like a hang.
	quietUntil := time.Now().Add(3 * time.Second)
	printing := false
	done := func() {
		if printing {
			fmt.Println()
		}
	}

	for {
		if ver, ok := probeHealth(port); ok && (isDevBuild() || ver == version.Version) {
			done()
			return ver, true
		}
		if time.Now().After(deadline) {
			done()
			return "", false
		}
		if !printing && time.Now().After(quietUntil) {
			fmt.Print("  正在等待守护进程启动")
			printing = true
		}
		if printing {
			fmt.Print(".")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// logTail returns the last n lines of the daemon log, so a failed start can show
// why instead of telling the user to go find the file.
func logTail(n int) string {
	path, err := config.LogPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// startupFailure builds the error for a daemon that never answered, with the log
// tail inline — that is the one piece of information that makes it actionable.
func startupFailure(port int) error {
	// The most common way a start silently fails is a stale process still holding
	// the port: the new daemon exits with "bind: address already in use" — which
	// goes to the service manager's journal, not daemon.log, so the log tail below
	// would say nothing about it. If something is still answering, name it. One
	// orphaned daemon held a port for a month while the service behind it
	// restarted half a million times, because nothing ever said this.
	if ver, ok := probeHealth(port); ok {
		return fmt.Errorf("端口 %d 已被另一个 agent-master 实例占用（v%s），新实例无法绑定。\n\n"+
			"若 agent-master stop 无法释放，说明有残留进程仍持有该端口：\n"+
			"  macOS：lsof -ti :%d\n"+
			"  Linux：ss -ltnp | grep %d\n"+
			"终止该进程后重新执行 agent-master start", port, ver, port, port)
	}

	msg := fmt.Sprintf("守护进程未能启动：%s 内端口 %d 始终无响应", startupGrace, port)
	if tail := logTail(15); tail != "" {
		msg += "\n\n守护进程日志末尾：\n" + tail
	}
	msg += "\n\n完整诊断：agent-master doctor"
	return errors.New(msg)
}

// warnIfDegraded prints a warning when the daemon is serving but cannot run
// claude — today that means it could not read the credentials this machine is
// known to export from the login shell. Without this, `start` reports a bare ✓
// for a daemon that will refuse every send.
func warnIfDegraded(cfg *config.Config) {
	writeDegradedWarning(os.Stdout, cfg)
}

func writeDegradedWarning(w io.Writer, cfg *config.Config) {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/api/info", cfg.Port), nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var body struct {
		ShellEnv struct {
			Blocked bool     `json:"blocked"`
			Missing []string `json:"missing"`
			Shell   string   `json:"shell"`
		} `json:"shell_env"`
		Auth struct {
			// Pointer, not bool: an older daemon omits this field entirely, and a
			// missing value must not read as "no credentials" — that would make a
			// new CLI falsely warn about every daemon it hasn't been upgraded with.
			OK   *bool  `json:"ok"`
			Hint string `json:"hint"`
		} `json:"auth"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return
	}

	if body.ShellEnv.Blocked {
		fmt.Fprintf(w, "\n⚠ 无法从登录 shell 读取 %s\n",
			strings.Join(body.ShellEnv.Missing, "、"))
		fmt.Fprintln(w, "  会话请求将被拒绝，以避免使用与终端不同的账号执行。")
		fmt.Fprintln(w, "  守护进程正在后台重试。")
		fmt.Fprintln(w, "  原因与修复方式：agent-master doctor")
		return
	}

	// Not blocked, but claude has nothing to authenticate with — runs will fail
	// per message with the CLI's own "Not logged in". Say it here instead.
	if body.Auth.OK != nil && !*body.Auth.OK {
		fmt.Fprintln(w, "\n⚠ 这台机器上未找到 Claude 凭证，会话请求将失败。")
		if body.ShellEnv.Shell != "" {
			fmt.Fprintf(w, "  已探测的登录 shell：%s\n", body.ShellEnv.Shell)
		}
		fmt.Fprintln(w, "  修复方式：agent-master doctor")
	}
}

// probeHealth does a short GET /health on localhost. It returns the reported
// version and whether the daemon answered — the truthful "is it serving" signal
// (works whether it runs as a service or a foreground `serve`).
func probeHealth(port int) (version string, ok bool) {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var body struct {
		Version string `json:"version"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body.Version, true
}
