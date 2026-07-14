# @ken-chy129/agent-master

Installs the native `agent-master` daemon for macOS, Linux, or Windows from the
matching GitHub Release and verifies its SHA-256 checksum.

```bash
npm install -g @ken-chy129/agent-master
agent-master start
```

Then open `http://127.0.0.1:8888` for the Web client, or connect with the
desktop app. Run `agent-master pair` to display addresses and the access token.

The daemon requires Claude Code to be installed and authenticated on the target
machine.
