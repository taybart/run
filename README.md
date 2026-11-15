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

```sh
$ run-daemon &; disown
$ run rest -s -d . -a :8005 &; disown
Running rest...
Press 'r' to reload, 'q' to quit
2025-03-17 13:02:52 [INFO] listening to :8005...
$ curl http://localhost:16354/restart?d=%2Fhome%2Fuser&p=8005
```

There is an mcp endpoint to restart the process at `http://localhost:19230/mcp`

# TODO

- [ ] grab logs from running process (maybe keep last 100 lines?)
