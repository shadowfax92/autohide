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
#   scripts/test-instance.sh pause-queue  launch the apps, pause while hide-others' hides are queued,
#                                         assert the queued ones are dropped and later hide on a fresh timeout
#   scripts/test-instance.sh agent-start  run the instance as a throwaway LaunchAgent instead
#   scripts/test-instance.sh agent-stop   boot that LaunchAgent out and delete its plist
#
# Environment:
#   AUTOHIDE_TEST_DIR       instance directory (default below; the socket path must stay < 104 bytes)
#   AUTOHIDE_BIN            binary to run (default: build and use .build/release/autohide)
#   AUTOHIDE_LAUNCHD_LABEL  label for agent-start/agent-stop (default com.autohide.test;
#                           the real com.autohide.daemon is refused)
#   AUTOHIDE_DEBUG=1        the daemon also logs every event and evaluation
#   E2E_WAIT                seconds e2e (and pause-queue, after its resume) watches for hides (default 15)

set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
DIR=${AUTOHIDE_TEST_DIR:-/Users/shadowfax/llm/code/side-projects/clis/mac-auto-hide/med-autohide/test/instance}
BIN=${AUTOHIDE_BIN:-$REPO/.build/release/autohide}
WAIT=${E2E_WAIT:-15}
LABEL=${AUTOHIDE_LAUNCHD_LABEL:-com.autohide.test}

# Not Chess: on macOS 26, activating a game summons GameOverlayUI, which got
# in the way of live runs.
# The exempt app is launched last, so a broken exemption shows within WAIT.
APPS=(Calculator TextEdit Preview "Font Book")
SHOULD_HIDE=(Calculator TextEdit Preview)
EXEMPT="Font Book"

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
  "only_apps" : ["TextEdit", "Calculator", "Preview", "Font Book"],
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
        sips -s format png -z 400 400 \
            /System/Library/CoreServices/CoreTypes.bundle/Contents/Resources/GenericApplicationIcon.icns \
            --out "$DIR/e2e.png" >/dev/null
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

# The first capture group of sed pattern $1 in text $2; empty if none.
first_match() {
    sed -n "s/$1/\\1/p" <<<"$2" | sed -n 1p
}

# The STATE column of `list` for one app. Names can contain spaces, so the
# name is matched as a prefix and the columns after it are counted.
app_state() {
    ah list 2>/dev/null | awk -v app="$1" '
        substr($0, 1, length(app) + 1) == app " " { $0 = substr($0, length(app) + 2); print $2; exit }'
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

    # Watch the whole window, not just until the three hide: the exempt app
    # launched last, so if its exemption were broken it would hide a moment later.
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
    local failures=0
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
        echo "PASS  $EXEMPT not hidden: $(app_state "$EXEMPT")"
    else
        echo "FAIL  $EXEMPT is exempt but was hidden at ${preview_hid}s"
        failures=$((failures + 1))
    fi

    echo "clean up with: $0 stop"
    [ "$failures" -eq 0 ]
}

# Sends hide-others and pause on two connections, both opened before either
# request is written. The server serves connections one at a time in accept
# order, so pause reaches main right after hide-others' reply, while the hides
# it queued are still going out one per run-loop turn.
hide_others_then_pause() {
    python3 -I - "$DIR/autohide.sock" <<'PY'
import socket, sys
first, second = (socket.socket(socket.AF_UNIX) for _ in range(2))
first.connect(sys.argv[1])
second.connect(sys.argv[1])
first.sendall(b'{"command":"hide-others"}\n')
second.sendall(b'{"command":"pause"}\n')
for conn in (first, second):
    reply = b""
    while not reply.endswith(b"\n"):
        chunk = conn.recv(65536)
        if not chunk:
            break
        reply += chunk
    print(reply.decode().strip()[:160])
PY
}

# Hides that hide-others queued but that had not reached Hider when a pause
# landed must be dropped. Otherwise a resume that
# beats their turn lets the pre-pause command hide apps whose timers the
# resume just refreshed. Pass: the dropped apps hide later by=timeout, a full
# timeout after the resume.
cmd_pause_queue() {
    daemon_pid >/dev/null || cmd_start >/dev/null
    mkdir -p "$DIR"
    make_fixtures
    echo "launching throwaway apps in the background:"
    local app
    for app in "${APPS[@]}"; do launch "$app"; done

    # Every app that should hide must be on screen and counting down. Polling
    # `list` runs evaluations, which is fine here: no timing is measured yet.
    local waiting=() _
    for _ in $(seq 50); do
        waiting=()
        for app in "${SHOULD_HIDE[@]}"; do
            [ "$(app_state "$app")" = pending ] || waiting+=("$app")
        done
        [ "${#waiting[@]}" -eq 0 ] && break
        sleep 0.2
    done
    [ "${#waiting[@]}" -eq 0 ] || die "not on screen and pending: ${waiting[*]} (\`stop\` first if they are hidden)"

    local mark
    mark=$(wc -l <"$LOG" | tr -d ' ')
    echo "hide-others, then pause, back to back:"
    hide_others_then_pause | sed 's/^/  /'
    sleep 1.5                     # anything already in Hider confirms within 1 s
    ah resume >/dev/null
    echo "resumed; watching ${WAIT}s for the dropped apps to hide on a fresh timeout"
    sleep "$WAIT"

    # Here-strings, not pipes from printf: under pipefail, a reader that stops
    # early would fail the pipeline with SIGPIPE.
    local after queued dropped early failures=0
    after=$(tail -n "+$((mark + 1))" "$LOG")
    queued=$(first_match '.*hide-others: queued \([0-9]*\) hides.*' "$after")
    dropped=$(first_match '.*paused until resumed; dropped \([0-9]*\) queued hides.*' "$after")
    early=$(grep -c 'INFO hid app=.* by=hide-others' <<<"$after" || true)
    queued=${queued:-0} dropped=${dropped:-0}

    echo "results:"
    if [ "$queued" -eq "${#SHOULD_HIDE[@]}" ] && [ "$dropped" -ge 1 ]; then
        echo "PASS  hide-others queued $queued hides; the pause dropped $dropped before their turn"
    else
        echo "FAIL  hide-others queued $queued, the pause dropped $dropped (want ${#SHOULD_HIDE[@]} and >= 1)"
        failures=$((failures + 1))
    fi
    if [ "$early" -eq $((queued - dropped)) ]; then
        echo "PASS  $early hide-others hide(s) were already in Hider; none of the dropped ones ran"
    else
        echo "FAIL  $early hides by=hide-others, expected $((queued - dropped))"
        failures=$((failures + 1))
    fi
    # The resume line splits the log: a dropped app must hide after it, by
    # its timer, idle about one full timeout.
    local after_resume line idle
    after_resume=$(sed -n '/INFO resumed:/,$p' <<<"$after")
    for app in "${SHOULD_HIDE[@]}"; do
        grep -q "INFO hid app=\"$app\".* by=hide-others" <<<"$after" && continue
        line=$(grep -m 1 "INFO hid app=\"$app\"" <<<"$after_resume" || true)
        idle=$(first_match '.* idle=\([0-9]*\)\.[0-9]*s .*' "$line")
        if [[ "$line" == *"by=timeout"* ]] && [ "${idle:-0}" -ge 9 ]; then
            echo "PASS  $app hid after the resume on a fresh timeout: ${line#* INFO }"
        else
            echo "FAIL  $app: ${line:-no hide within ${WAIT}s of the resume}"
            failures=$((failures + 1))
        fi
    done
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
    pause-queue) cmd_pause_queue ;;
    agent-start) cmd_agent_start ;;
    agent-stop) cmd_agent_stop ;;
    *) echo "usage: $0 start|stop|term|status|list|e2e|pause-queue|agent-start|agent-stop" >&2; exit 2 ;;
esac
