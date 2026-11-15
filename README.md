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

### daemon mode

`run-daemon` keeps track of every `run` (in a terminal) so they can be listed
and restarted from elsewhere, ex. a script or an agent. Each `run` registers
when it starts and unregisters when it exits (however it exits).

```sh
$ run-daemon &; disown
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
[{"ok":false,"running":false,"exit_code":1,"error":"exited w/ exit status 1","output":"[ERROR] failed to read site.rest\n"}]
```

`404` means nothing matches. A `run` that doesn't confirm within 5s (ex. an
older version) gets `"error": "sent the restart, but run didn't confirm it"`.

There is an mcp endpoint at `http://localhost:19230/mcp` with the same
`list_scripts` and `restart_script` (which returns the same report as text).

# TODO

- [ ] grab logs from running process (maybe keep last 100 lines?), run keeps
      the last 64KB for restart reports, an endpoint could serve it
