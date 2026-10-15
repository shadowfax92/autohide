#!/usr/bin/env bash
# Runs an isolated autohide beside the real install, for safe live testing.
#
# The instance gets its own --config-dir (config, socket, log, and lock), no
# menu-bar item, a 10 s timeout, and an only_apps allowlist of throwaway apps,
# so it cannot hide anything the owner is using. This script only ever opens
# or quits the apps in APPS, and quits only the processes it launched itself.
#
#   scripts/test-instance.sh start    write config.json, start the daemon in the background
#   scripts/test-instance.sh stop     stop the daemon, quit the apps `e2e` launched
#   scripts/test-instance.sh status   `autohide status` for the instance (args pass through)
#   scripts/test-instance.sh list     `autohide list` for the instance (args pass through)
#   scripts/test-instance.sh e2e      launch the apps, assert that the allowlisted ones hide
#
# Environment:
#   AUTOHIDE_TEST_DIR  instance directory (default below; the socket path must stay < 104 bytes)
#   AUTOHIDE_BIN       binary to run (default: build and use .build/release/autohide)
#   E2E_WAIT           seconds e2e watches for hides (default 15)

set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
DIR=${AUTOHIDE_TEST_DIR:-/Users/shadowfax/llm/code/side-projects/clis/mac-auto-hide/med-autohide/test/instance}
BIN=${AUTOHIDE_BIN:-$REPO/.build/release/autohide}
WAIT=${E2E_WAIT:-15}

APPS=(Calculator Chess TextEdit Preview)
SHOULD_HIDE=(Calculator Chess TextEdit)
EXEMPT=Preview

PIDFILE=$DIR/daemon.pid
LAUNCHED=$DIR/launched.pids   # "<pid> <app>" for each app process e2e started

die() { echo "test-instance: $*" >&2; exit 1; }
ah() { "$BIN" --config-dir "$DIR" "$@"; }

# Prints the daemon's pid if the recorded process is still our daemon. The
# command-line check guards against a recycled pid.
daemon_pid() {
    [ -f "$PIDFILE" ] || return 1
    local pid
    pid=$(cat "$PIDFILE")
    kill -0 "$pid" 2>/dev/null || return 1
    case "$(ps -p "$pid" -o command= 2>/dev/null)" in
        *"--config-dir $DIR daemon"*) echo "$pid" ;;
        *) return 1 ;;
    esac
}

cmd_start() {
    local pid
    if pid=$(daemon_pid); then
        echo "test instance already running (pid $pid)"
        return 0
    fi
    if [ -z "${AUTOHIDE_BIN:-}" ]; then
        (cd "$REPO" && swift build -c release >/dev/null) || die "swift build failed"
    fi
    [ -x "$BIN" ] || die "no binary at $BIN"
    local socket="$DIR/autohide.sock"
    [ "${#socket}" -lt 104 ] || die "socket path is ${#socket} bytes; unix sockets allow 103: $socket"

    mkdir -p "$DIR"
    cat >"$DIR/config.json" <<EOF
{
  "app_timeouts" : {},
  "exempt" : ["$EXEMPT"],
  "only_apps" : ["TextEdit", "Calculator", "Chess", "Preview"],
  "timeout" : "10s"
}
EOF
    # Detached from this shell: the instance must outlive the script.
    nohup "$BIN" --config-dir "$DIR" daemon --no-menubar </dev/null >>"$DIR/stdout.log" 2>&1 &
    pid=$!
    echo "$pid" >"$PIDFILE"

    for _ in $(seq 50); do
        if ah status >/dev/null 2>&1; then
            ah status
            return 0
        fi
        kill -0 "$pid" 2>/dev/null || die "daemon exited at start; see $DIR/stdout.log and $DIR/autohide.log"
        sleep 0.1
    done
    die "daemon (pid $pid) did not answer within 5 s; see $DIR/stdout.log"
}

cmd_stop() {
    local pid
    if pid=$(daemon_pid); then
        kill -TERM "$pid"
        for _ in $(seq 50); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
        if kill -0 "$pid" 2>/dev/null; then
            echo "daemon $pid ignored SIGTERM for 5 s; sending SIGKILL" >&2
            kill -KILL "$pid" 2>/dev/null || true
        fi
        echo "stopped test daemon (pid $pid)"
    else
        echo "test daemon not running"
    fi
    rm -f "$PIDFILE"
    quit_launched
}

# SIGTERM, not an Apple Event "quit": an Apple Event would raise an
# Automation permission prompt for the terminal. The fixtures have no unsaved
# changes, so nothing is lost.
quit_launched() {
    [ -f "$LAUNCHED" ] || return 0
    local pid app
    while read -r pid app; do
        [ -n "$pid" ] || continue
        # Only if that pid is still the app we launched.
        if [ "$(basename "$(ps -p "$pid" -o comm= 2>/dev/null)")" = "$app" ]; then
            kill -TERM "$pid" 2>/dev/null && echo "quit $app (pid $pid)"
        fi
    done <"$LAUNCHED"
    rm -f "$LAUNCHED"
}

make_fixtures() {
    echo "autohide e2e fixture" >"$DIR/e2e.txt"
    [ -f "$DIR/e2e.png" ] ||
        sips -s format png -z 400 400 "$REPO/assets/autohide.icns" --out "$DIR/e2e.png" >/dev/null
}

# Opens one app in the background. If it was not already running, records the
# new pid so `stop` can quit it; an app the owner already had open is never quit.
launch() {
    local app=$1 before pid
    before=$(pgrep -x -u "$(id -u)" "$app" || true)
    case "$app" in
        TextEdit) open -g -a TextEdit "$DIR/e2e.txt" ;;
        Preview) open -g -a Preview "$DIR/e2e.png" ;;
        *) open -g -a "$app" ;;
    esac
    if [ -n "$before" ]; then
        echo "  $app was already running (pid $before); it will not be quit"
        return 0
    fi
    for _ in $(seq 50); do
        pid=$(pgrep -x -u "$(id -u)" "$app" | head -n 1 || true)
        if [ -n "$pid" ]; then
            echo "$pid $app" >>"$LAUNCHED"
            echo "  launched $app (pid $pid)"
            return 0
        fi
        sleep 0.1
    done
    echo "  $app did not start within 5 s" >&2
}

# `list --json` → one "<name><TAB><state>" line per app. JXA ships with every
# macOS and sends no Apple Events, so it needs no permission and no install.
states() {
    local json
    json=$(ah list --json 2>/dev/null) || return 0
    osascript -l JavaScript -e '
        function run(argv) {
            var reply = JSON.parse(argv[0]);
            return (reply.apps || []).map(function (a) { return a.name + "\t" + a.state; }).join("\n");
        }' "$json"
}

state_of() {   # app, states-text → the app's state, "missing" if absent
    local found
    found=$(printf '%s\n' "$2" | awk -F '\t' -v app="$1" '$1 == app { print $2; exit }')
    echo "${found:-missing}"
}

cmd_e2e() {
    daemon_pid >/dev/null || cmd_start >/dev/null
    mkdir -p "$DIR"
    make_fixtures

    echo "launching throwaway apps in the background:"
    local start app
    start=$(date +%s)
    for app in "${APPS[@]}"; do launch "$app"; done

    # Watch the whole window, not just until the three hide: Preview launched
    # last, so if its exemption were broken it would hide a moment later.
    local -a hidden_at=()
    local preview_hid="" snapshot now elapsed i
    while :; do
        snapshot=$(states)
        now=$(date +%s)
        elapsed=$((now - start))
        for i in "${!SHOULD_HIDE[@]}"; do
            if [ -z "${hidden_at[$i]:-}" ] && [ "$(state_of "${SHOULD_HIDE[$i]}" "$snapshot")" = hidden ]; then
                hidden_at[$i]=$elapsed
            fi
        done
        [ "$(state_of "$EXEMPT" "$snapshot")" = hidden ] && preview_hid=$elapsed
        [ "$elapsed" -ge "$WAIT" ] && break
        sleep 0.5
    done

    echo "results after ${WAIT}s:"
    local failures=0
    for i in "${!SHOULD_HIDE[@]}"; do
        app=${SHOULD_HIDE[$i]}
        if [ -n "${hidden_at[$i]:-}" ]; then
            echo "PASS  $app hidden (by ${hidden_at[$i]}s)"
        else
            echo "FAIL  $app not hidden within ${WAIT}s (state: $(state_of "$app" "$snapshot"))"
            failures=$((failures + 1))
        fi
    done
    if [ -z "$preview_hid" ]; then
        echo "PASS  $EXEMPT not hidden (state: $(state_of "$EXEMPT" "$snapshot"))"
    else
        echo "FAIL  $EXEMPT is exempt but was hidden at ${preview_hid}s"
        failures=$((failures + 1))
    fi

    echo "clean up with: $0 stop"
    [ "$failures" -eq 0 ]
}

case "${1:-}" in
    start) cmd_start ;;
    stop) cmd_stop ;;
    status) shift; ah status "$@" ;;
    list) shift; ah list "$@" ;;
    e2e) cmd_e2e ;;
    *) echo "usage: $0 start|stop|list|status|e2e" >&2; exit 2 ;;
esac
