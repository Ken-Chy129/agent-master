package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/config"
	"github.com/Ken-Chy129/agent-master/internal/provider"
	"github.com/Ken-Chy129/agent-master/internal/service"
	"github.com/Ken-Chy129/agent-master/internal/version"
)

// cmdDoctor answers "it says it started — why doesn't it work?".
//
// Liveness and readiness are different questions, and for a long time this tool
// only answered the first: `start` printed ✓ once the service manager accepted
// the job, `status` printed ✓ once /health answered, and a client's dot went
// green once /api/info returned — none of which imply a run can succeed. A
// machine spent a month in that gap: healthy by every available signal, failing
// every message, with no command to ask why and no pointer to a log.
//
// The report is organized by problem, not by internal module: one entry per
// thing that is actually wrong, titled with its consequence, followed by the
// commands that fix it. Mechanism goes in the diagnostics block below, where it
// helps whoever wants it without making the verdict harder to read.
func cmdDoctor(_ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	d := &doctor{out: os.Stdout, cfg: cfg}
	d.run()
	if d.failures() > 0 {
		// Non-zero so `agent-master doctor` is usable in a script or CI check.
		return errSilent
	}
	return nil
}

// errSilent ends a command with a non-zero exit status without main printing an
// extra "error:" line — doctor has already said everything worth saying.
var errSilent = &silentError{}

type silentError struct{}

func (*silentError) Error() string { return "" }

// finding is one problem: what it means for the user, and how to fix it.
type finding struct {
	fatal bool     // true = nothing will run until this is fixed
	title string   // the consequence, not the mechanism
	fixes []string // commands to run or edits to make, in order of preference
}

// fact is an observation with no verdict attached, for the diagnostics block.
type fact struct {
	label string
	value string
	extra []string // raw lines (e.g. log excerpts) printed beneath
}

type doctor struct {
	out      io.Writer
	cfg      *config.Config
	findings []finding
	facts    []fact
}

func (d *doctor) fail(title string, fixes ...string) {
	d.findings = append(d.findings, finding{fatal: true, title: title, fixes: fixes})
}

func (d *doctor) warn(title string, fixes ...string) {
	d.findings = append(d.findings, finding{title: title, fixes: fixes})
}

func (d *doctor) note(label, value string, extra ...string) {
	d.facts = append(d.facts, fact{label: label, value: value, extra: extra})
}

func (d *doctor) failures() int {
	n := 0
	for _, f := range d.findings {
		if f.fatal {
			n++
		}
	}
	return n
}

func (d *doctor) run() {
	if info := d.inspectDaemon(); info != nil {
		d.inspectFromDaemon(info)
	} else {
		d.inspectLocally()
	}
	d.inspectLogs()
	d.render()
}

// inspectDaemon returns the running daemon's /api/info payload, or nil when there
// is no usable daemon to ask.
func (d *doctor) inspectDaemon() map[string]any {
	ver, up := probeHealth(d.cfg.Port)
	if !up {
		if service.Installed() {
			d.fail(fmt.Sprintf("守护进程未在运行，端口 %d 无响应", d.cfg.Port),
				"agent-master restart",
				"若仍无响应，执行 agent-master doctor 查看日志中的启动错误")
		} else {
			d.fail("守护进程尚未安装",
				"agent-master start")
		}
		d.note("守护进程", fmt.Sprintf("未运行（端口 %d）", d.cfg.Port))
		return nil
	}

	if !isDevBuild() && ver != version.Version {
		// The exact shape of the month-long failure: a stale process holds the
		// port, so every restart of the real service dies on bind and every
		// health probe is answered by the wrong daemon.
		d.fail(fmt.Sprintf("端口 %d 被另一个 agent-master 实例占用（v%s），当前版本 v%s 无法启动",
			d.cfg.Port, ver, version.Version),
			fmt.Sprintf("macOS：lsof -ti :%d | xargs kill", d.cfg.Port),
			fmt.Sprintf("Linux：ss -ltnp | grep %d，然后终止对应进程", d.cfg.Port),
			"随后执行 agent-master start")
	}
	d.note("守护进程", fmt.Sprintf("%s  v%s", d.cfg.Addr(), ver))

	info, err := d.fetchInfo()
	if err != nil {
		d.fail("守护进程响应 /health 但拒绝 /api/info："+err.Error(),
			"~/.agent-master/config.json 中的 token 可能与运行中的守护进程不一致",
			"agent-master restart")
		return nil
	}
	return info
}

// inspectFromDaemon interprets the daemon's own view of its environment. Its
// answers are authoritative in a way local checks cannot be: the whole class of
// failure here is the daemon's environment differing from the shell's.
func (d *doctor) inspectFromDaemon(info map[string]any) {
	claude, _ := info["providers"].(map[string]any)
	cl, _ := claude["claude"].(map[string]any)
	claudePath, _ := cl["path"].(string)
	if claudePath != "" {
		d.note("Claude 可执行文件", claudePath)
	} else {
		d.note("Claude 可执行文件", "未找到")
		d.fail("未找到 claude 可执行文件，无法执行任何会话",
			"安装 Claude Code CLI",
			`或在 ~/.agent-master/config.json 中设置 "claude_bin": "/path/to/claude"`)
	}

	env, hasEnv := info["shell_env"].(map[string]any)
	shell, _ := env["shell"].(string)
	imported := stringsOf(env["imported"])
	auth, hasAuth := info["auth"].(map[string]any)

	// Record the environment as facts first, so the findings below can stay
	// focused on consequences.
	switch {
	case !hasEnv:
		d.note("登录 shell", "运行中的守护进程版本过旧，未上报")
	case truthy(env["blocked"]):
		d.note("登录 shell", fmt.Sprintf("%s，凭证变量已丢失", shellLabel(shell)))
	case !truthy(env["ok"]):
		reason, _ := env["reason"].(string)
		d.note("登录 shell", fmt.Sprintf("%s，环境探测失败：%s", shellLabel(shell), reason))
	case len(imported) > 0:
		d.note("登录 shell", fmt.Sprintf("%s，已导入 %s", shellLabel(shell), strings.Join(imported, "、")))
	default:
		d.note("登录 shell", fmt.Sprintf("%s，未导出 ANTHROPIC_* / CLAUDE_* 变量", shellLabel(shell)))
	}

	switch {
	case !hasAuth:
		d.note("凭证", "运行中的守护进程版本过旧，未上报")
	case truthy(auth["ok"]):
		src, _ := auth["source"].(string)
		val := src
		if base, _ := auth["baseUrl"].(string); base != "" {
			val += fmt.Sprintf("，请求指向 %s", base)
		}
		d.note("凭证", val)
	default:
		d.note("凭证", "未找到")
	}

	if !hasEnv || !hasAuth {
		d.warn("运行中的守护进程版本过旧，无法上报凭证与环境状态，本次诊断不完整",
			"升级后重启守护进程：npm install -g @ken-chy129/agent-master@latest && agent-master restart")
	}

	// A blocked shell env is the more specific and more severe problem: sends are
	// actively refused, so report that instead of the missing credential it
	// implies. Two entries for one cause would only make the fix ambiguous.
	if hasEnv && truthy(env["blocked"]) {
		missing := strings.Join(stringsOf(env["missing"]), "、")
		d.fail(fmt.Sprintf("登录 shell 中的凭证已丢失（%s），会话请求将被拒绝", missing),
			fmt.Sprintf("恢复 %s 中对应的 export，然后执行 agent-master restart", rcFileFor(shell)),
			`若已确认不再使用这些变量，在 ~/.agent-master/config.json 中设置 "shell_env_optional": true`)
		return
	}

	if hasAuth && !truthy(auth["ok"]) {
		fixes := []string{
			"在这台机器上执行 claude /login",
			fmt.Sprintf("或在 %s 中添加 export ANTHROPIC_API_KEY=sk-...", rcFileFor(shell)),
			"完成后执行 agent-master restart",
		}
		// When sh was probed, the rc-file advice alone will not work: sh does not
		// read it. Name that, or the user edits the right file to no effect.
		if isShellSh(shell) {
			fixes = append(fixes,
				"注意：本机守护进程探测的是 /bin/sh，它不读取 ~/.bashrc 与 ~/.zshrc；"+
					"升级守护进程，或为该服务显式设置 SHELL=/bin/bash")
		}
		d.fail("未找到 Claude 凭证，所有会话请求将失败", fixes...)
		return
	}

	// The probe failing while credentials came from elsewhere is worth mentioning
	// but breaks nothing today.
	if hasEnv && !truthy(env["ok"]) && !truthy(env["blocked"]) {
		d.warn("登录 shell 环境探测失败，守护进程正在后台重试",
			"当前凭证来自其他来源，会话可正常执行；若日后改用 shell 变量提供凭证，需先修复该探测")
	}
}

// inspectLocally runs what can be checked without a daemon. Every fact is
// labelled as coming from the caller's shell: an interactive shell has the user's
// rc files applied and a background service does not, and that discrepancy is
// exactly what hides this class of failure. A local pass proves nothing about the
// daemon.
func (d *doctor) inspectLocally() {
	d.note("检查范围", "守护进程未运行，以下结果取自当前 shell，不代表守护进程的运行环境")

	if path := resolveClaudeBin(d.cfg); path != "" && path != "claude" {
		d.note("Claude 可执行文件", path+"（当前 shell）")
	} else {
		d.note("Claude 可执行文件", "未找到（当前 shell）")
		d.fail("当前 shell 中未找到 claude 可执行文件",
			"安装 Claude Code CLI",
			`或在 ~/.agent-master/config.json 中设置 "claude_bin": "/path/to/claude"`)
	}

	if auth := provider.ClaudeAuth(); auth.OK {
		d.note("凭证", auth.Source+"（当前 shell；守护进程未必能读到）")
	} else {
		d.note("凭证", "未找到（当前 shell）")
		d.fail("当前 shell 中未找到 Claude 凭证",
			"在这台机器上执行 claude /login",
			"或在 shell 配置文件中添加 export ANTHROPIC_API_KEY=sk-...")
	}
}

func (d *doctor) inspectLogs() {
	path, err := config.LogPath()
	if err != nil {
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		d.note("日志", path+"（尚未创建）")
		return
	}
	d.note("日志", fmt.Sprintf("%s  %s，更新于 %s", path,
		humanSize(fi.Size()), fi.ModTime().Format("2006-01-02 15:04")))

	// Surface recent problems rather than making the user go read the file. This
	// closes the "nowhere to see the logs" gap: the log existed all along and
	// nothing ever pointed at it.
	if lines := recentProblems(path, 200); len(lines) > 0 {
		d.note("近期错误", fmt.Sprintf("%d 条（最近 200 行）", len(lines)), lines...)
	} else {
		d.note("近期错误", "无（最近 200 行）")
	}
}

func (d *doctor) render() {
	fmt.Fprintf(d.out, "agent-master 诊断  v%s\n\n", version.Version)

	if len(d.findings) == 0 {
		fmt.Fprintln(d.out, "✓ 全部检查通过")
	}
	for _, f := range d.findings {
		mark := "⚠"
		if f.fatal {
			mark = "✗"
		}
		fmt.Fprintf(d.out, "%s %s\n", mark, f.title)
		if len(f.fixes) > 0 {
			fmt.Fprintln(d.out, "  修复方式：")
			for _, fix := range f.fixes {
				fmt.Fprintf(d.out, "    %s\n", fix)
			}
		}
		fmt.Fprintln(d.out)
	}

	fmt.Fprintln(d.out, "诊断信息")
	// Pad by display width, not byte length: a CJK label is 3 bytes but 2 columns
	// wide, so %-Ns would stagger every line in a mixed-script report.
	width := 0
	for _, f := range d.facts {
		if w := displayWidth(f.label); w > width {
			width = w
		}
	}
	for _, f := range d.facts {
		fmt.Fprintf(d.out, "  %s  %s\n", padTo(f.label, width), f.value)
		for _, e := range f.extra {
			fmt.Fprintf(d.out, "  %s  %s\n", padTo("", width), truncate(e, 140))
		}
	}

	fails, warns := d.failures(), len(d.findings)-d.failures()
	fmt.Fprintln(d.out)
	switch {
	case fails > 0 && warns > 0:
		fmt.Fprintf(d.out, "发现 %d 项故障、%d 项警告。修复故障后会话才能正常执行。\n", fails, warns)
	case fails > 0:
		fmt.Fprintf(d.out, "发现 %d 项故障。修复后会话才能正常执行。\n", fails)
	case warns > 0:
		fmt.Fprintf(d.out, "未发现故障，%d 项警告。会话应可正常执行。\n", warns)
	default:
		fmt.Fprintln(d.out, "未发现问题。")
	}
}

func (d *doctor) fetchInfo() (map[string]any, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/api/info", d.cfg.Port), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.cfg.Token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body, nil
}

// recentProblems returns the last few WARN/ERROR lines from the daemon log.
func recentProblems(path string, tailLines int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
	}
	var out []string
	for _, l := range lines {
		if strings.Contains(l, "level=ERROR") || strings.Contains(l, "level=WARN") {
			out = append(out, l)
		}
	}
	// Keep the report short; the log path is printed above for the full picture.
	if len(out) > 5 {
		out = out[len(out)-5:]
	}
	return out
}

// rcFileFor names the file the given shell actually reads, so the fix points at
// the right one instead of guessing ~/.zshrc for a bash user.
func rcFileFor(shell string) string {
	switch {
	case strings.Contains(shell, "zsh"):
		return "~/.zshrc"
	case strings.Contains(shell, "fish"):
		return "~/.config/fish/config.fish"
	case strings.Contains(shell, "bash"):
		return "~/.bashrc"
	default:
		return "shell 配置文件"
	}
}

func shellLabel(shell string) string {
	if shell == "" {
		return "未知 shell"
	}
	return shell
}

// isShellSh reports whether the probed shell was sh, which reads neither
// ~/.bashrc nor ~/.zshrc no matter what it is a symlink to.
func isShellSh(shell string) bool {
	return shell == "/bin/sh" || strings.HasSuffix(shell, "/sh")
}

func truthy(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func stringsOf(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// displayWidth approximates terminal columns: East Asian wide characters occupy
// two, everything else one. Enough for aligning a fixed set of labels.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if isWideRune(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0xA4CF, // CJK radicals through Yi
		r >= 0xAC00 && r <= 0xD7A3, // Hangul syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK compatibility ideographs
		r >= 0xFE30 && r <= 0xFE6F, // CJK compatibility forms
		r >= 0xFF00 && r <= 0xFF60, // fullwidth forms
		r >= 0xFFE0 && r <= 0xFFE6:
		return true
	}
	return false
}

func padTo(s string, width int) string {
	if pad := width - displayWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func humanSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.0f KB", float64(n)/1024)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
