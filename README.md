# run

Run a command with the ability to restart it quickly

```sh
$ run rest -s -d . -a :8005
Running rest...
Press 'r' to reload, 'q' to quit
2025-03-17 13:02:52 [INFO] listening to :8005...
// r is pressed
Reloading script...
2025-03-17 13:02:55 [INFO] listening to :8005...
// q (or esc or ctrl+c) is pressed
Exiting...
```

### watch mode

`-w` restarts the script when files with those extensions change in the
current directory (and below), `-w '*'` for any file. Dot files/folders
(`.git`) and `node_modules` are skipped.

```sh
$ run -w mn,rest,css rest -s -f .
Running rest...
Watching for changes to: mn, rest, css
Press 'r' to reload, 'q' to quit
// src/app.mn is saved
src/app.mn changed
Reloading script...
```

Flags stop at the script, so its own flags are passed through
(`run -w mn -- rest -s` works too). It polls file times and sizes every 500ms
instead of using file events, so it works the same on macOS and Linux and
sees editors that save by renaming a temp file. A burst of saves is one
restart. Don't watch files the script itself writes, or it'll restart in a
loop.

#### handling changes in the script (fd 3)

A script can handle `-w` changes itself instead of being restarted. Every
script gets a socket on fd 3 (and `RUN_EVENTS_FD=3`). A script that doesn't
use it never notices. One that writes `hello` to it gets each batch of changes
instead of a restart:

```
change modified src/docs/intro.md
change added public/img/new.png
change removed src/old.mn
done
```

It can write `restart` to get restarted anyway (ex. for a change it can't
handle). `r` and daemon restarts are always restarts. If it stops reading,
it's restarted instead of run waiting on it.

```sh
#!/bin/sh
echo hello >&3
while read -r kind what path <&3; do
  [ "$kind" = done ] && echo "end of batch" || echo "$what: $path"
done
```

### daemon mode

`run-daemon` keeps track of every `run` (in a terminal) so they can be listed
and restarted from elsewhere, ex. a script or an agent. Each `run` registers
when it starts and unregisters when it exits (however it exits).

```sh
$ run-daemon install   # once: a service that starts at login (launchd/systemd), started now
$ cd /home/user/site && run rest -s -d . -a :8005
Running rest...
Press 'r' to reload, 'q' to quit
Registered with daemon
2025-03-17 13:02:52 [INFO] listening to :8005...
```

```sh
# list what's running in a directory (and below)
$ curl 'http://localhost:19230/list?dir=/home/user'
[{"dir":"/home/user/site","script":"rest","args":["-s","-d",".","-a",":8005"]}]

# restart it: dir, script and args have to match
$ curl -X POST http://localhost:19230/restart \
    -d '{"dir":"/home/user/site","script":"rest","args":["-s","-d",".","-a",":8005"]}'
[{"ok":true,"running":true,"output":"2025-03-17 13:03:10 [INFO] listening to :8005...\n"}]
```

A restart waits for `run` to confirm it: about a second after restarting, it
reports whether the script is still running (or how it exited) and what it
printed since the restart (the last 4KB). There's one result per matching
`run`, and the status is `500` if any of them didn't come back up:

```json
[{"ok":false,"running":false,"exit_code":1,"error":"exited with exit status 1","output":"[ERROR] failed to read site.rest\n"}]
```

`404` means nothing matches. A `run` that doesn't confirm within 5s (ex. an
older version) gets `"error": "sent the restart, but run didn't confirm it"`.

#### logs

`run` keeps the last 64KB of the script's output, and the daemon can return
the tail of it without restarting anything. Like a restart, `dir`, `script`
and `args` have to match (`args` repeat in the query):

```sh
$ curl 'http://localhost:19230/logs?dir=/home/user/site&script=rest&args=-s&args=-d&args=.&args=-a&args=:8005'
[{"output":"2025-03-17 13:03:10 [INFO] listening to :8005...\n"}]
```

There's one result per matching `run`, each with the tail of what it printed
(`truncated` says there was more than it keeps). `404` means nothing matches.

#### the service

`run-daemon install` installs it as a per-user service and starts it: a
launchd agent on macOS (`~/Library/LaunchAgents/com.taybart.run-daemon.plist`,
logs in `~/Library/Logs/run-daemon.log`), a systemd user unit on linux
(`~/.config/systemd/user/run-daemon.service`, logs in
`journalctl --user -u run-daemon`). It starts at login and restarts if it dies.
No root needed. `run-daemon uninstall` stops and removes it.

To update, install the new binary and run `install` again, which restarts the
service with it:

```sh
go install ./cmd/run-daemon && run-daemon install
```

`run`s reconnect on their own (quietly, every 2s) when the daemon restarts, or
when it starts after them. `c` reconnects right away. Only one `run-daemon`
runs at a time: stop one started by hand (`run-daemon &`) before installing.

On linux, a user service stops when you log out. To keep it running (ex. on a
server): `loginctl enable-linger $USER`.

There is an mcp endpoint at `http://localhost:19230/mcp` with the same
`list_scripts` and `restart_script` (which returns the same report as text),
plus `get_logs` (the tail of a script's output, like `/logs`).

#### security

Everything binds to loopback (`127.0.0.1`) and only answers to loopback `Host`
headers, so a page open in your browser can't read the logs (DNS rebinding) or
trigger a restart (cross-site requests). The daemon's unix socket lives in a
per-user directory and is `0600`, so it can't be reached by another user on a
shared machine. Logs are served raw, so a script that prints secrets has them
in `/logs` — anything on the machine as you can already read them, but treat
the output as sensitive.

# TODO

See [LEFTOVERS.md](LEFTOVERS.md).
