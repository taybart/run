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
