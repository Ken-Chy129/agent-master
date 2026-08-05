# @ken-chy129/agent-master

**简体中文** · [English](#english)

从对应的 GitHub Release 下载适用于 macOS、Linux 或 Windows 的原生 `agent-master`
守护进程，并校验其 SHA-256 校验和。

```bash
npm install -g @ken-chy129/agent-master
agent-master start
```

随后打开 `http://127.0.0.1:8888` 使用 Web 客户端，或用桌面端连接。执行
`agent-master pair` 可查看可用地址与访问令牌。

守护进程要求目标机器上已安装并登录 Claude Code。若 `start` 显示成功但会话仍然
失败，执行 `agent-master doctor` 查看原因与修复方式。

完整文档见[项目主页](https://github.com/Ken-Chy129/agent-master)。

---

## English

Installs the native `agent-master` daemon for macOS, Linux, or Windows from the
matching GitHub Release and verifies its SHA-256 checksum.

```bash
npm install -g @ken-chy129/agent-master
agent-master start
```

Then open `http://127.0.0.1:8888` for the Web client, or connect with the
desktop app. Run `agent-master pair` to display addresses and the access token.

The daemon requires Claude Code to be installed and authenticated on the target
machine. If `start` reports success but sessions still fail, run
`agent-master doctor` for the cause and how to fix it.

Command output is currently localized in Simplified Chinese. Full documentation
is on the [project page](https://github.com/Ken-Chy129/agent-master).
