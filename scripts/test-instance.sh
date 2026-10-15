#!/usr/bin/env bash
# Runs an isolated autohide beside the real install, for safe live testing.
#
# The instance gets its own --config-dir (config, socket, log, and lock), no
# menu-bar item, a 10 s timeout, and an only_apps allowlist of throwaway apps,
# so it cannot hide anything the owner is using. This script only ever opens
# or quits the apps in APPS, and quits only the processes it launched itself.
#
#   scripts/test-instance.sh start        write config.json, start the daemon in the background
#   scripts/test-instance.sh stop         stop the daemon, quit the apps `e2e` launched
#   scripts/test-instance.sh term         SIGTERM the daemon only; check exit 0 and socket removal
#   scripts/test-instance.sh status       `autohide status` for the instance (args pass through)
#   scripts/test-instance.sh list         `autohide list` for the instance (args pass through)
#   scripts/test-instance.sh e2e          launch the apps, assert that the allowlisted ones hide
#   scripts/test-instance.sh agent-start  run the instance as a throwaway LaunchAgent instead
#   scripts/test-instance.sh agent-stop   boot that LaunchAgent out and delete its plist
#
# Environment:
#   AUTOHIDE_TEST_DIR       instance directory (default below; the socket path must stay < 104 bytes)
#   AUTOHIDE_BIN            binary to run (default: build and use .build/release/autohide)
#   AUTOHIDE_LAUNCHD_LABEL  label for agent-start/agent-stop (default com.autohide.test;
#                           the real com.autohide.daemon is refused)
#   AUTOHIDE_DEBUG=1        the daemon also logs every event and evaluation
#   E2E_WAIT                seconds e2e watches for hides (default 15)

set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
DIR=${AUTOHIDE_TEST_DIR:-/Users/shadowfax/llm/code/side-projects/clis/mac-auto-hide/med-autohide/test/instance}
BIN=${AUTOHIDE_BIN:-$REPO/.build/release/autohide}
WAIT=${E2E_WAIT:-15}
LABEL=${AUTOHIDE_LAUNCHD_LABEL:-com.autohide.test}

APPS=(Calculator Chess TextEdit Preview)
SHOULD_HIDE=(Calculator Chess TextEdit)
EXEMPT=Preview

PIDFILE=$DIR/daemon.pid
EXITFILE=$DIR/daemon.exit     # the daemon's exit status, written by the wrapper in start
LAUNCHED=$DIR/launched.pids   # "<pid> <app>" for each app process e2e started
LOG=$DIR/autohide.log

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

build_if_needed() {
    if [ -z "${AUTOHIDE_BIN:-}" ]; then
        (cd "$REPO" && swift build -c release >/dev/null) || die "swift build failed"
    fi
    [ -x "$BIN" ] || die "no binary at $BIN"
    local socket="$DIR/autohide.sock"
    [ "${#socket}" -lt 104 ] || die "socket path is ${#socket} bytes; unix sockets allow 103: $socket"
}

write_config() {
    mkdir -p "$DIR"
    cat >"$DIR/config.json" <<EOF
{
  "app_timeouts" : {},
  "exempt" : ["$EXEMPT"],
  "only_apps" : ["TextEdit", "Calculator", "Chess", "Preview"],
  "timeout" : "10s"
}
EOF
}

cmd_start() {
    local pid
    if pid=$(daemon_pid); then
        echo "test instance already running (pid $pid)"
        return 0
    fi
    build_if_needed
    write_config
    rm -f "$EXITFILE"
    # Detached from this shell: the instance must outlive the script. The
    # wrapper shell waits for the daemon so `term` can report its exit status.
    nohup bash -c '"$0" --config-dir "$1" daemon --no-menubar; echo $? >"$1/daemon.exit"' \
        "$BIN" "$DIR" </dev/null >>"$DIR/stdout.log" 2>&1 &
    local wrapper=$!

    for _ in $(seq 50); do
        if ah status >/dev/null 2>&1; then
            # The daemon writes its own pid into the lock file it holds.
            tr -d '\n' <"$DIR/autohide.lock" >"$PIDFILE"
            ah status
            return 0
        fi
        kill -0 "$wrapper" 2>/dev/null || die "daemon exited at start; see $DIR/stdout.log and $LOG"
        sleep 0.1
    done
    die "daemon did not answer within 5 s; see $DIR/stdout.log"
}

# SIGTERM the daemon and verify the clean-shutdown contract: exit status 0
# (so launchd's KeepAlive{SuccessfulExit=false} leaves it stopped) and no
# socket file left behind. Leaves the apps alone.
cmd_term() {
    local pid status
    pid=$(daemon_pid) || { echo "test daemon not running"; return 0; }
    kill -TERM "$pid"
    for _ in $(seq 50); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
    if kill -0 "$pid" 2>/dev/null; then
        echo "FAIL  daemon $pid ignored SIGTERM for 5 s; sending SIGKILL" >&2
        kill -KILL "$pid" 2>/dev/null || true
        rm -f "$PIDFILE"
        return 1
    fi
    rm -f "$PIDFILE"
    for _ in $(seq 20); do [ -f "$EXITFILE" ] && break; sleep 0.1; done
    status=$(cat "$EXITFILE" 2>/dev/null || echo "unknown")
    local failures=0
    if [ "$status" = 0 ]; then echo "PASS  daemon $pid exited 0 on SIGTERM"
    else echo "FAIL  daemon $pid exited with status $status"; failures=1; fi
    if [ -e "$DIR/autohide.sock" ]; then echo "FAIL  socket left behind: $DIR/autohide.sock"; failures=1
    else echo "PASS  socket removed"; fi
    [ "$failures" -eq 0 ]
}

cmd_stop() {
    if daemon_pid >/dev/null; then
        cmd_term || true
    else
        echo "test daemon not running"
        rm -f "$PIDFILE"
    fi
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

# A LaunchAgent run gives the daemon no inherited TCC grants (a terminal
# launch borrows the terminal's) and exercises KeepAlive. The label and plist
# are throwaway; the real com.autohide.daemon is never touched.
agent_env() {
    [ "$LABEL" != com.autohide.daemon ] || die "refusing to manage the real com.autohide.daemon"
    export AUTOHIDE_LAUNCHD_LABEL=$LABEL
    export AUTOHIDE_LAUNCHD_PLIST=$DIR/$LABEL.plist
}

cmd_agent_start() {
    daemon_pid >/dev/null && die "a test daemon started with \`start\` is running; \`stop\` it first"
    agent_env
    build_if_needed
    write_config
    ah install --no-menubar
    launchctl print "gui/$(id -u)/$LABEL" | awk '/^\t(state|runs|pid|last exit code) = / && !seen[$1]++'
}

cmd_agent_stop() {
    agent_env
    ah uninstall
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

# The daemon's "hid" line for an app, among log lines after line $2.
hid_line() {
    tail -n "+$(($2 + 1))" "$LOG" 2>/dev/null | grep -m 1 "INFO hid app=\"$1\"" || true
}

# Hides are read from the daemon's log, not by polling `list`: every IPC
# request runs an evaluation, and a poll every 0.5 s would mask exactly the
# detection delays this test exists to catch.
cmd_e2e() {
    daemon_pid >/dev/null || cmd_start >/dev/null
    mkdir -p "$DIR"
    make_fixtures
    local mark
    mark=$(wc -l <"$LOG" | tr -d ' ')

    echo "launching throwaway apps in the background:"
    local start app
    start=$(date +%s)
    for app in "${APPS[@]}"; do launch "$app"; done

    # Watch the whole window, not just until the three hide: Preview launched
    # last, so if its exemption were broken it would hide a moment later.
    local -a hidden_at=() hidden_line=()
    local preview_hid="" now elapsed i line
    while :; do
        now=$(date +%s)
        elapsed=$((now - start))
        for i in "${!SHOULD_HIDE[@]}"; do
            if [ -z "${hidden_at[$i]:-}" ]; then
                line=$(hid_line "${SHOULD_HIDE[$i]}" "$mark")
                if [ -n "$line" ]; then hidden_at[$i]=$elapsed; hidden_line[$i]=$line; fi
            fi
        done
        [ -n "$(hid_line "$EXEMPT" "$mark")" ] && preview_hid=$elapsed
        [ "$elapsed" -ge "$WAIT" ] && break
        sleep 0.5
    done

    echo "results after ${WAIT}s:"
    local failures=0 snapshot
    snapshot=$(ah list 2>/dev/null || true)
    for i in "${!SHOULD_HIDE[@]}"; do
        app=${SHOULD_HIDE[$i]}
        if [ -n "${hidden_at[$i]:-}" ]; then
            echo "PASS  $app hidden ${hidden_at[$i]}s after launch: ${hidden_line[$i]#* INFO }"
        else
            echo "FAIL  $app not hidden within ${WAIT}s"
            failures=$((failures + 1))
        fi
    done
    if [ -z "$preview_hid" ]; then
        echo "PASS  $EXEMPT not hidden: $(printf '%s\n' "$snapshot" | awk -v app="$EXEMPT" '$1 == app { print $3 }')"
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
    term) cmd_term ;;
    status) shift; ah status "$@" ;;
    list) shift; ah list "$@" ;;
    e2e) cmd_e2e ;;
    agent-start) cmd_agent_start ;;
    agent-stop) cmd_agent_stop ;;
    *) echo "usage: $0 start|stop|term|status|list|e2e|agent-start|agent-stop" >&2; exit 2 ;;
esac
