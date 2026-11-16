---
name: run
description: Run a command with quick restart and file-watch mode, and talk to the run daemon to list, restart, or read the logs of running scripts. Use this skill whenever the user mentions `run` (the taybart tool), `run-daemon`, restarting or reloading a script/service they're developing, watching files for changes to restart on, or reading what a running service printed without restarting it (list_scripts, restart_script, get_logs, /list, /restart, /logs).
---

# `run`

`run` runs a command with quick restart and file-watch mode, and pairs with a
`run-daemon` service that tracks running scripts so they can be listed,
restarted, and read from elsewhere (a script or an agent).

## CLI

```sh
run [-w exts] [--] <script> [args...]
```

- Keys: `r` restart, `q` (or esc / ctrl+c) quit, `c` reconnect to the daemon.
- Flags stop at the script (`run -w mn -- rest -s` works), so the script's own
  flags pass through.
- `-w mn,rest,css` restarts when matching files change under the current dir
  (and below); `-w '*'` watches everything. Dot files/folders (`.git`) and
  `node_modules` are skipped. A burst of saves is one restart.

### watch mode + fd 3

A script can handle `-w` changes itself instead of being restarted. Every
script gets a socket on fd 3 (and `RUN_EVENTS_FD=3`). Write `hello` to opt in,
then read batches of changes (`change <kind> <path>` lines, ended by `done`);
write `restart` to be restarted anyway. `r` and daemon restarts are always
restarts. A script that stops reading is restarted instead of `run` waiting.

## Daemon (`run-daemon`)

`run-daemon install` makes it a per-user service (launchd agent on macOS,
systemd user unit on Linux) that starts at login and restarts if it dies.
`run-daemon uninstall` stops and removes it. Each `run` registers over a unix
socket and unregisters when it exits; `run`s reconnect on their own (every 2s).

It serves a JSON HTTP API on `http://127.0.0.1:19230` (loopback only):

- `GET /list?dir=<dir>` — scripts running in `dir` and below
  (`[{"dir","script","args"}]`)
- `POST /restart` — body `{"dir","script","args"}`; restarts matching runs and
  returns one result each: still running (`running`), or how it exited
  (`ok`,`exit_code`,`error`), plus `output` since the restart (last 4KB)
- `GET /logs?dir=..&script=..&args=..` — the tail of each matching run's output
  (stdout+stderr, last 64KB) without restarting it; `{"output","truncated"}`
- `POST /mcp` — the MCP (JSON-RPC) endpoint

`/restart` and `/logs` match exactly on `dir`, `script`, and `args` (`args`
repeat in the query, ex. `&args=-s&args=-d`). `404` means nothing matches. A
restart returns `500` if any instance didn't come back up.

## MCP tools

At `http://127.0.0.1:19230/mcp`:

- `list_scripts {dir}` — scripts registered in `dir`
- `restart_script {dir, script, args?}` — restart, then report each result as
  text (still running / exit code / output since the restart)
- `get_logs {dir, script, args?}` — the tail of a script's output as text

Use `get_logs` to see what a running service printed (or why a start failed)
without restarting it.

## Security

The daemon binds to loopback and rejects non-loopback `Host` headers (blocks
DNS rebinding) and cross-site browser requests (blocks CSRF restarts). Its unix
socket is in a per-user directory and `0600`, so another user on a shared
machine can't reach it. Logs are served raw, so treat a script's output as
sensitive.
