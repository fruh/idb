# idb — adb-style CLI for iOS

Single Go binary built on [go-ios](https://github.com/danielpaulus/go-ios). Works on the host
and inside Docker, the same way `adb` reaches a host adb server.

```
container: idb <cmd> --host.docker.internal:27016--> host: idb server --> usbmuxd --> iPhone
```

## Install

```
go install github.com/fruh/idb@latest     # Go 1.26+; installs `idb` into $(go env GOPATH)/bin (put it on PATH)
idb version                               # idb v0.1.0 (commit abc1234, ...)
```
From a checkout: `make build` (bin/idb), `make install`, `make dist` (cross-compiled binaries in dist/).
Versions come from git tags: `git tag v0.1.0 && git push --tags` makes `go install ...@latest` report `v0.1.0`
(untagged installs show a pseudo-version).

## Host

```
idb server                 # starts the background daemon (127.0.0.1:27016, Docker Desktop on macOS/Windows),
                           # or just prints its status if it is already running
idb server --bind 172.17.0.1   # Linux: docker bridge address
idb kill-server            # stop it
idb server --foreground    # stay attached (Docker/launchd/systemd entrypoint)
```
`idb start-server` is an alias of `idb server`. State and log: `~/.idb/server.json`, `~/.idb/server.log`.
Changing settings of a running daemon needs `idb kill-server` first. The relay has no authentication; keep it on loopback or the docker bridge.

## Container

```
docker run --rm --add-host host.docker.internal:host-gateway \
  -v $PWD:/w -w /w <image> idb devices -l
```

No `IDB_HOST` is needed: without a local usbmuxd socket idb uses `host.docker.internal:27016` on its own
(`--add-host` is only required on Linux). Set `-H` / `IDB_HOST` for a different host or port.

State lives in `~/.idb/` (`IDB_HOME` overrides): `known_hosts`, `images/`, `pair/` (tunnel agent identity).

Endpoint selection is automatic: if there is no local `/var/run/usbmuxd`, idb probes
`host.docker.internal:27016` and uses it when an `idb server` answers. Overrides, in order of precedence:
`-H` flag, `IDB_HOST`, `USBMUXD_SOCKET_ADDRESS`. Set `IDB_NO_FALLBACK=1` to disable the probe.
`idb server` never uses the fallback.
Pairing records stay on the host, so the container needs no trust step.

## Commands

```
adb-style tool for iOS devices (host or Docker)

Usage:
  idb [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  crashlogs   List or download crash reports
  devices     List attached devices
  doctor      Check usbmuxd connectivity and device state
  forward     Forward a host TCP port to a port on the device (until Ctrl-C)
  help        Help about any command
  info        Print all lockdown values of the device as JSON
  install     Install a signed .ipa (or .app directory)
  kill        Terminate a process (developer services)
  kill-server Stop the idb server daemon
  launch      Launch an app (developer services)
  logcat      Stream the device syslog
  ls          List remote files (media partition, or an app container with -a)
  ls-apps     List installed apps with bundle and data-container paths
  mount       Download and mount the developer disk image if none is mounted
  ps          List running processes (developer services)
  pull        Copy a remote file or directory to the host
  push        Copy a host file to the device
  reverse     Expose a host service on the device (like adb reverse; jailbroken devices, until Ctrl-C)
  rm          Delete a remote file (or directory with -r)
  screenshot  Capture the screen (needs developer services)
  server      Start the host relay daemon (usbmuxd over TCP) or print its status
  shell       Shell on the device: SSH (jailbroken), else a file browser
  uninstall   Uninstall an app
  version     Print the idb version

Flags:
  -h, --help              help for idb
  -H, --host string       usbmuxd/idb server address, e.g. host.docker.internal:27016 (env IDB_HOST)
      --no-auto-mount     do not download/mount the developer image automatically (env IDB_NO_AUTO_MOUNT)
  -s, --serial string     device UDID (env IDB_SERIAL)
      --tunnel-port int   tunnel agent port for iOS 17+ developer services (env GO_IOS_AGENT_PORT) (default 60105)
  -v, --verbose           verbose go-ios logging
      --version           version for idb

Use "idb [command] --help" for more information about a command.
```

`idb <command> --help` shows the details of each command, e.g. the relay daemon:

```
Starts a background server that relays the host's usbmuxd to a TCP port, so containers
can reach your devices (they find it at host.docker.internal:<port> automatically, or via
IDB_HOST). If it is already running, only its status is printed. Stop it with "idb kill-server".

--foreground keeps the server attached to the terminal (use it as a Docker, launchd or
systemd entrypoint). State and log live in ~/.idb/server.json and ~/.idb/server.log.

Docker Desktop (macOS/Windows) reaches the host's loopback, so the default
--bind 127.0.0.1 works. On Linux use the docker bridge address, e.g.
--bind 172.17.0.1, with --add-host host.docker.internal:host-gateway.
The relay is unauthenticated: anyone who can reach it can use your paired devices.

Usage:
  idb server [flags]

Aliases:
  server, start-server

Flags:
      --bind string           address to listen on (default "127.0.0.1")
      --foreground            stay attached to the terminal instead of daemonizing
  -h, --help                  help for server
      --pair-records string   directory for the tunnel agent's own pairing identity (default "~/.idb/pair")
      --port string           TCP port to listen on (default "27016")
      --tunnel                also run the iOS 17+ tunnel agent (userspace, no root) for ps/launch/kill/screenshot
      --upstream string       usbmuxd address (default: local socket)

Global Flags:
  -H, --host string       usbmuxd/idb server address, e.g. host.docker.internal:27016 (env IDB_HOST)
      --no-auto-mount     do not download/mount the developer image automatically (env IDB_NO_AUTO_MOUNT)
  -s, --serial string     device UDID (env IDB_SERIAL)
      --tunnel-port int   tunnel agent port for iOS 17+ developer services (env GO_IOS_AGENT_PORT) (default 60105)
  -v, --verbose           verbose go-ios logging
```

`idb shell` SSH options: `--user` (default mobile), `-i key`, `--password`/`IDB_SSH_PASSWORD`, `--ssh-port`.
Auth order: key, ssh-agent, password, the jailbreak default `alpine` (only if no password was given; `--no-default-password` disables it), prompt. If the default is rejected, idb asks for the password (3 tries). Host keys are pinned per UDID in `~/.idb/known_hosts`
(a changed key is refused unless `--insecure-hostkey`). A failed login is an error, not a fallback. Without a jailbreak there is no shell on iOS.

## Reverse (jailbroken devices)

```
idb reverse 8080 8080                          # phone's 127.0.0.1:8080 -> localhost:8080 where idb runs
idb reverse 8080 host.docker.internal:8080     # from a container: reach a service on the host
```

iOS has no device-to-host channel over USB (usbmuxd only opens host -> device connections), so unlike
`adb reverse` this needs a jailbreak: idb logs in to OpenSSH on the device (same options as `idb shell`) and
uses SSH remote forwarding, so the phone's sshd listens and tunnels each connection back to the target.
It runs in the foreground until Ctrl-C and ends when the SSH session drops. The listener is bound to the
phone's loopback unless you pass `--bind`; sshd must allow TCP forwarding. On stock iOS make the app connect
to the host's LAN IP instead.

Select a device with `-s <udid>` or `IDB_SERIAL`. Container access with `-a` needs a
developer-signed app (full container) or UIFileSharingEnabled (Documents only).

## Developer services (ps, launch, kill, screenshot)

1. The developer image is downloaded to `~/.idb/images` (`IDB_IMAGES` overrides) and mounted automatically the first time
   a developer command needs it; idb tells you when it does. `--no-auto-mount` / `IDB_NO_AUTO_MOUNT=1` disables this, `idb mount`
   does it explicitly. The download happens where idb runs, so in Docker mount a volume: `-v ~/.idb:/root/.idb` (this also keeps `known_hosts` and the tunnel identity).
   iOS 17+ also needs Developer Mode on. Images stay mounted until the device reboots.
2. iOS 15/16: nothing more; works through usbmuxd, also from containers.
3. iOS 17+: run `idb server --tunnel` on the host. It starts a userspace tunnel agent (no root),
   serving tunnel info on `--tunnel-port` (default 60105) next to the usbmuxd relay. Clients find it
   on the same host as the relay. With `--bind 172.17.0.1` the per-device tunnel ports are relayed
   to that address too. Like the relay, the agent is unauthenticated.
