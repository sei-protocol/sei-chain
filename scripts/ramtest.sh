#!/usr/bin/env bash
# Run `go test` with all test scratch I/O redirected to a RAM-backed filesystem.
#
# BOTH GOTMPDIR and TMPDIR are required, and it is not the split you would guess.
# Measured on go1.26.0: t.TempDir() resolves GOTMPDIR when it is set and only falls back to
# TMPDIR otherwise, while os.TempDir() and os.MkdirTemp("") always resolve TMPDIR. Since
# t.TempDir() is where roughly 1,400 call sites in this repo get their working directory,
# GOTMPDIR carries the bulk of the win and dropping it would quietly reduce this script to
# doing almost nothing. TMPDIR still matters for the os.MkdirTemp("") holdouts, notably
# app/test_helpers.go's shared receipt-DB directory and sei-cosmos/testutil/network's
# cross-process port lock. Verify with a probe before "simplifying" either one away.
#
# The storage tests benefit most: they stand up real Pebble/WAL/FlatKV instances, and pebble
# already runs with NoSync, so what is left to win is file create/unlink metadata and
# writeback bandwidth rather than fsync. Measured 482s -> 109s on ./sei-db/... (24 GiB M-series
# Mac, warm build cache, -count=1). Expect much less on packages that are not I/O bound.
#
# GOCACHE is deliberately left on disk. It is tens of GiB for this tree, so a RAM copy
# could never hold a full build and every run would recompile Cosmos + Tendermint + EVM +
# wasmvm from scratch, costing far more than the I/O saves. The on-disk cache's hot subset
# is already RAM-resident for free via the buffer cache. GOMODCACHE stays for the same
# reason, and HOME stays because tests that need an isolated one already make their own
# (testutil/configtest.Isolate) inside TMPDIR.
#
# XDG_CACHE_HOME is never set: on Linux GOCACHE derives from it, so touching it would
# relocate the build cache as a side effect and silently make this a net loss.
#
# Both platforms work without sudo. macOS attaches an unmounted ram:// device and formats
# it case-sensitive HFS+; Linux prefers a subdirectory of /dev/shm, which is already tmpfs.
#
# Usage: ramtest.sh [flags] [go-test-args...]   (--help for the full list)
set -euo pipefail

PROG=$(basename "$0")

# Tags CI sets in GOFLAGS. Not applied by default: the tag set is part of Go's action-ID
# hash, so adding it gives the entire dependency graph a second cache population and makes
# the first run a full cold compile -- a real tax on a tool whose purpose is speed. The
# only packages affected are the four named in CI_TAG_PACKAGES, and none of them fail to
# build without the tags; their ledger tests simply are not compiled in.
CI_TAGS="ledger,test_ledger_mock"
CI_TAG_PACKAGES="sei-cosmos/crypto/{ledger,keyring}, sei-cosmos/client/keys, .../legacybech32"

# Exit codes distinct from go test's own 1, so a caller can tell an infrastructure failure
# from a real test failure.
EX_TESTFAIL=1
EX_EXHAUSTED=3
EX_LEAKED=4
EX_NORAM=5
EX_INTERRUPTED=130

# Watchdog poll interval. Short enough to catch a gradual fill, but a test that fills the
# volume in one burst will still outrun it; diagnose_failure says so rather than pretending
# the sampled peak is the whole story.
POLL_SECS=2

log()  { printf '%s: %s\n' "$PROG" "$*"; }
warn() { printf '%s: WARNING: %s\n' "$PROG" "$*" >&2; }
die()  { printf '%s: error: %s\n' "$PROG" "$*" >&2; exit "${2:-1}"; }

print_usage() {
	cat <<EOF
Usage: $PROG [flags] [go-test-args...]

Runs go test with TMPDIR and GOTMPDIR on a RAM disk. Any argument that is not a flag
below is passed through to go test verbatim, so package patterns, -run, -v, -count,
-parallel and friends all work as usual. Default target is ./...

  --size N        RAM disk size in GiB. Honored up to total physical RAM; warns above
                  60%. Default: auto, clamp(RAM/2, 4, 32). Env: SEI_RAMTEST_SIZE_GB
  --keep          Leave the volume mounted after the run, for reuse by the next one.
  --down          Tear down this worktree's volume and exit.
  --race          Add -race. Off by default: the race detector is usually the wall-clock
                  driver for this repo, which hides what the RAM disk changed.
  --ci-tags       Add -tags=$CI_TAGS, matching CI. Costs a separate
                  build-cache population; see the note at the top of this script.
  --require-ram   Fail instead of falling back to disk when RAM is unavailable.
  --print-env     Create or reuse the volume, print the export lines, run no tests.
  -h, --help      This text.

Examples:
  $PROG                                          # whole repo
  $PROG ./sei-db/...                             # one subtree
  $PROG ./sei-db/state_db/sc/flatkv/... -run TestSnapshot -v
  $PROG --down

For a baseline to compare against, run go test directly. Keep the timeout: the slowest
sei-db package takes over 8 minutes on a physical disk, against go's 10m default.
  go test -timeout=30m ./sei-db/...
EOF
}

SIZE_GB=""
KEEP=false
DOWN_ONLY=false
USE_RACE=false
USE_CI_TAGS=false
REQUIRE_RAM=false
PRINT_ENV=false
GO_TEST_ARGS=()

# Set only by ensure_ramdisk, when a Linux host can offer no RAM-backed filesystem and
# --require-ram was not given. There is no flag for it: a run that does not want a RAM disk
# is a run that should call go test directly.
DEGRADED=false

# Where this worktree's scratch volume lives and how it has to be released. The three kinds
# are acquired and released three different ways, and they answer "does it exist?" three
# different ways, so every such question dispatches on this rather than on the OS.
#   darwin -- an hdiutil ram:// device, released by detaching the device
#   shm    -- a directory inside the pre-existing /dev/shm tmpfs, released by removing it
#   tmpfs  -- a tmpfs this script mounted with sudo, released by unmounting it
VOLUME_KIND=""

# The --size recorded when the volume was created, as distinct from the one resolved for this
# run. An existing volume cannot be resized, so this is the figure that describes it.
RECORDED_SIZE_GB=""

# Identifies the owner of a /dev/shm directory. That tmpfs is shared with every other tenant
# on the host, so a directory found there is only ours once this file says so.
OWNER_MARKER=".ramtest-owner"

# What the volume reported in use before this run started writing. Subtracted from every later
# reading so the figures describe this run rather than the volume.
BASELINE_KB=0

parse_args() {
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--size) SIZE_GB=${2:?--size needs a value}; shift 2 ;;
		--keep) KEEP=true; shift ;;
		--down) DOWN_ONLY=true; shift ;;
		--race) USE_RACE=true; shift ;;
		--ci-tags) USE_CI_TAGS=true; shift ;;
		--require-ram) REQUIRE_RAM=true; shift ;;
		--print-env) PRINT_ENV=true; shift ;;
		-h | --help) print_usage; exit 0 ;;
		*) GO_TEST_ARGS+=("$1"); shift ;;
		esac
	done
}

detect_platform() {
	OS=$(uname -s)
	case "$OS" in
	Darwin | Linux) ;;
	*) die "unsupported platform $OS; only Darwin and Linux are handled" ;;
	esac
}

# The state directory must live on the real filesystem: it holds the log and the peak
# record, which have to survive the volume being filled and then unmounted.
init_paths() {
	REPO_ROOT=$(git rev-parse --show-toplevel)
	cd "$REPO_ROOT"
	STATE_DIR="$REPO_ROOT/build/.ramtest"
	STATE_FILE="$STATE_DIR/state"
	PEAK_FILE="$STATE_DIR/peak"
	EXHAUSTED_FILE="$STATE_DIR/exhausted"
	LOG_FILE="$REPO_ROOT/build/ramtest.log"
	mkdir -p "$STATE_DIR"
	rm -f "$PEAK_FILE" "$EXHAUSTED_FILE"

	# Names the volume after this worktree so sibling worktrees, which this repo uses for
	# parallel agents, cannot tear down each other's mounts. cksum rather than shasum
	# because shasum is not universally present on Linux, and all 32 bits of it because
	# discarding any of them only makes two worktrees more likely to collide on one name.
	local id
	id=$(printf '%s' "$REPO_ROOT" | cksum | cut -d' ' -f1)
	LABEL=$(printf 'sr-%08x' "$id")

	# The defaults for a cold start. load_state overrides them whenever a previous run left a
	# record, which is the only way the sudo-tmpfs location is ever found again.
	if [[ "$OS" == Darwin ]]; then
		VOLUME_KIND=darwin
		MNT="/Volumes/$LABEL"
	else
		VOLUME_KIND=shm
		MNT="/dev/shm/$LABEL"
	fi
}

total_ram_bytes() {
	if [[ "$OS" == Darwin ]]; then
		sysctl -n hw.memsize
	else
		awk '/^MemTotal:/ {print $2 * 1024}' /proc/meminfo
	fi
}

resolve_size() {
	local ram_gb
	ram_gb=$(( $(total_ram_bytes) / 1024 / 1024 / 1024 ))
	[[ -z "$SIZE_GB" ]] && SIZE_GB=${SEI_RAMTEST_SIZE_GB:-}

	if [[ -z "$SIZE_GB" ]]; then
		# Half of RAM, floored at 4 and capped at 32. Generous is cheap: the backing store
		# is allocated lazily and its pages are pageable rather than wired, so an unused
		# volume costs nothing and an over-full one degrades to swap instead of OOM.
		SIZE_GB=$(( ram_gb / 2 ))
		if (( SIZE_GB < 4 )); then SIZE_GB=4; fi
		if (( SIZE_GB > 32 )); then SIZE_GB=32; fi
		return 0
	fi

	[[ "$SIZE_GB" =~ ^[0-9]+$ ]] || die "--size takes a whole number of GiB, got '$SIZE_GB'"
	(( SIZE_GB > 0 )) || die "--size must be at least 1"
	# An explicit size is the caller telling us about their machine, so it is honored right
	# up to the point where it cannot be backed at all.
	if (( SIZE_GB >= ram_gb )); then
		die "--size $SIZE_GB GiB is not less than this host's $ram_gb GiB of RAM"
	fi
	if (( SIZE_GB * 10 > ram_gb * 6 )); then
		warn "--size $SIZE_GB GiB is over 60% of ${ram_gb} GiB of RAM; if the host swaps, the"
		warn "  RAM disk is partly on disk and slower than doing nothing. The report prints a"
		warn "  swap delta -- check it."
	fi
}

save_state() {
	printf 'DEV=%s\nMNT=%s\nSIZE_GB=%s\nVOLUME_KIND=%s\n' \
		"${DEV:-}" "$MNT" "$SIZE_GB" "$VOLUME_KIND" > "$STATE_FILE"
}

# Restores the volume's identity from the record left by the run that created it. Creation and
# teardown happen in different processes whenever --keep is used, so everything teardown needs
# -- the device to detach, the mount point, the kind, and the size named in the leak warning --
# has to survive that gap. Nothing outside this function reads the state file.
#
# SIZE_GB is restored only when this run has not resolved one of its own, so a --size flag
# still wins for the run that is about to happen while a teardown-only run still learns the
# size it is releasing.
load_state() {
	[[ -f "$STATE_FILE" ]] || return 0
	local key value
	while IFS='=' read -r key value; do
		case "$key" in
		DEV) DEV="$value" ;;
		MNT) [[ -n "$value" ]] && MNT="$value" ;;
		VOLUME_KIND) [[ -n "$value" ]] && VOLUME_KIND="$value" ;;
		SIZE_GB) RECORDED_SIZE_GB="$value" ;;
		esac
	done < "$STATE_FILE"
	[[ -z "$SIZE_GB" ]] && SIZE_GB="$RECORDED_SIZE_GB"
	return 0
}

clear_state() { rm -f "$STATE_FILE"; }

is_mounted() { mount | grep -q " on $MNT "; }

# Whether this run's ram:// device is still attached, mounted or not. A format that failed
# leaves exactly that state, and the device holds its full reservation either way.
device_attached() {
	[[ -n "${DEV:-}" ]] || return 1
	hdiutil info | awk '{print $1}' | grep -qxF "$DEV"
}

# Whether this worktree's scratch volume is present and usable for a run. The /dev/shm kind is a
# directory inside a tmpfs the host already had mounted, where a mount check can only ever
# answer false.
volume_exists() {
	case "$VOLUME_KIND" in
	shm) [[ -d "$MNT" ]] ;;
	*) is_mounted ;;
	esac
}

# Whether anything is still held that teardown has to release. A Darwin device keeps its
# reservation with no filesystem on it, which is what a failed format leaves behind: releasable
# without being usable. Reuse asks volume_exists instead, because writing to a mount point whose
# volume never mounted does not reach a RAM disk.
volume_is_releasable() {
	volume_exists && return 0
	[[ "$VOLUME_KIND" == darwin ]] && device_attached
}

# Confirms a volume found without a state file is one of ours before any teardown path can act
# on it, because force-detaching or deleting an unidentified volume could destroy a real one.
adopt_volume() {
	case "$VOLUME_KIND" in
	darwin)
		diskutil info "$MNT" 2>/dev/null | grep -q 'Volume Name:.*'"$LABEL" ||
			die "$MNT is mounted but is not a $PROG volume; refusing to touch it"
		DEV=$(mount | awk -v m="$MNT" '$3 == m {print $1}')
		;;
	shm)
		[[ -f "$MNT/$OWNER_MARKER" ]] && grep -qxF "$REPO_ROOT" "$MNT/$OWNER_MARKER" || {
			warn "$MNT exists but carries no ownership marker for $REPO_ROOT."
			die "refusing to touch it; remove it by hand if it is yours: rm -rf $MNT"
		}
		;;
	esac
	save_state
}

# Reconciles the state file against reality. A record with no volume is the residue of a killed
# run and is simply cleared; a volume with no record is adopted once it is confirmed to be ours.
reap_orphans() {
	load_state
	if volume_is_releasable; then
		[[ -f "$STATE_FILE" ]] || adopt_volume
		return 0
	fi
	[[ -f "$STATE_FILE" ]] && { log "clearing stale state from an interrupted run"; clear_state; }
	return 0
}

create_ramdisk_darwin() {
	local sectors
	sectors=$(( SIZE_GB * 1024 * 1024 * 1024 / 512 ))
	DEV=$(hdiutil attach -nomount "ram://$sectors")
	DEV="${DEV//[[:space:]]/}"
	[[ -n "$DEV" ]] || die "hdiutil attach returned no device"
	# Claimed the moment the device exists rather than once it is usable. It is already holding
	# its reservation here, so a format that fails still has to leave the exit trap able to
	# release it and a later --down able to find it.
	CREATED_HERE=true
	save_state
	# HFSX is case-sensitive, unjournaled HFS+: the cheapest create/unlink path, which is
	# what this workload is. Case sensitivity also matches Linux and CI, so a case-collision
	# bug surfaces here rather than only in CI.
	diskutil eraseVolume HFSX "$LABEL" "$DEV" >/dev/null ||
		die "diskutil eraseVolume failed on $DEV"
	# Written before any test data lands, or fseventsd indexes a volume full of SST files.
	: > "$MNT/.metadata_never_index" 2>/dev/null || true
	mkdir -p "$MNT/.fseventsd" 2>/dev/null && : > "$MNT/.fseventsd/no_log" 2>/dev/null || true
	log "created $SIZE_GB GiB case-sensitive HFS+ RAM disk at $MNT ($DEV)"
}

create_ramdisk_linux() {
	local avail_gb
	if [[ -d /dev/shm && -w /dev/shm ]]; then
		avail_gb=$(( $(df -Pk /dev/shm | awk 'NR==2 {print $4}') / 1024 / 1024 ))
		if (( avail_gb >= SIZE_GB )); then
			VOLUME_KIND=shm
			mkdir -p -m 700 "$MNT"
			# Stamped at creation so teardown can tell this directory apart from any other
			# tenant's in a /dev/shm that the whole host shares.
			printf '%s\n' "$REPO_ROOT" > "$MNT/$OWNER_MARKER"
			CREATED_HERE=true
			save_state
			log "using /dev/shm (tmpfs, ${avail_gb} GiB available) at $MNT"
			return
		fi
		log "/dev/shm has only ${avail_gb} GiB available, need $SIZE_GB"
	fi

	if sudo -n true 2>/dev/null; then
		VOLUME_KIND=tmpfs
		MNT="/mnt/$LABEL"
		sudo mkdir -p "$MNT"
		# nr_inodes=0 lifts the inode cap; a full run creates tens of thousands of files.
		sudo mount -t tmpfs -o "size=${SIZE_GB}G,mode=0700,noatime,nr_inodes=0" "$LABEL" "$MNT" ||
			die "mounting tmpfs at $MNT failed"
		sudo chown "$(id -u):$(id -g)" "$MNT"
		CREATED_HERE=true
		save_state
		log "mounted $SIZE_GB GiB tmpfs at $MNT"
		return
	fi

	return 1
}

ensure_ramdisk() {
	# A volume that was already here is reused rather than recreated, and deliberately not
	# claimed with CREATED_HERE: whoever kept it owns its teardown.
	if volume_exists; then
		log "reusing the RAM disk already at $MNT"
		if [[ -n "$RECORDED_SIZE_GB" && "$RECORDED_SIZE_GB" != "$SIZE_GB" ]]; then
			warn "this volume was created at $RECORDED_SIZE_GB GiB and cannot be resized;"
			warn "  the requested $SIZE_GB GiB does not apply. Run --down first to change it."
			SIZE_GB="$RECORDED_SIZE_GB"
		fi
		return
	fi

	# A recorded volume that is held but not usable is the debris of a run whose format failed.
	# It still holds its reservation, so it is released before a replacement is attached rather
	# than being abandoned with its record about to be overwritten.
	if volume_is_releasable; then
		log "releasing an unusable volume left by an earlier run"
		teardown_ramdisk || warn "could not release it; continuing with a new volume"
	fi

	if [[ "$OS" == Darwin ]]; then
		create_ramdisk_darwin
	else
		create_ramdisk_linux || {
			local why="no usable /dev/shm and no passwordless sudo to mount a tmpfs"
			if $REQUIRE_RAM; then
				die "$why (--require-ram was given)" $EX_NORAM
			fi
			warn "$why"
			warn "  falling back to the normal filesystem: this run will NOT be accelerated."
			warn "  In a container, pass --shm-size=${SIZE_GB}g. To make this an error, use --require-ram."
			DEGRADED=true
			return
		}
	fi
}

# A reused volume carries the debris of whatever the last run left behind when it was
# killed, and slow accumulation to ENOSPC is the classic way warm reuse fails.
prepare_dirs() {
	rm -rf "$MNT/tmp" "$MNT/gotmp"
	mkdir -p -m 700 "$MNT/tmp" "$MNT/gotmp"
}

# Both are load-bearing; see the note at the top of this file. gotmp is where the test data
# actually lands, because t.TempDir() prefers GOTMPDIR.
export_env() {
	export TMPDIR="$MNT/tmp"
	export TMP="$TMPDIR"
	export TEMP="$TMPDIR"
	export GOTMPDIR="$MNT/gotmp"
}

swap_counter() {
	if [[ "$OS" == Darwin ]]; then
		sysctl -n vm.swapusage | sed -n 's/.*used = \([0-9.]*\)M.*/\1/p'
	else
		awk '/^pswpout/ {print $2}' /proc/vmstat
	fi
}

# Kilobytes the volume reports in use, this run's and anything else's alike. On a volume of our
# own the filesystem's own accounting is exact and costs nothing, but inside /dev/shm it would
# count every other tenant on the host, so there the directory has to be walked instead.
volume_used_raw_kb() {
	case "$VOLUME_KIND" in
	shm) du -sk "$MNT" 2>/dev/null | awk '{print $1}' ;;
	*) df -Pk "$MNT" | awk 'NR==2 {print $3}' ;;
	esac
}

# What the volume already reported before this run wrote anything. A freshly formatted HFS+
# volume is tens of megabytes, not zero, and the /dev/shm directory carries its owner marker.
record_baseline_usage() { BASELINE_KB=$(volume_used_raw_kb); }

# Kilobytes this run has written. Everything downstream -- the watchdog's threshold, the peak
# in the report, and the check that anything reached the RAM disk at all -- is about this run's
# own consumption, so the volume's own overhead is not part of it.
volume_used_kb() {
	local raw
	raw=$(volume_used_raw_kb) || return 1
	[[ -n "$raw" ]] || return 1
	if (( raw > BASELINE_KB )); then
		printf '%s\n' $(( raw - BASELINE_KB ))
	else
		printf '0\n'
	fi
}

WATCHDOG_PID=""

# Polls the volume rather than waiting for ENOSPC, because ENOSPC surfaces deep inside a
# Pebble compaction as an error that looks nothing like "the disk you gave me filled up".
#
# Fullness is measured against --size, not against the filesystem's own capacity, so that this
# watchdog, report and diagnose_failure all judge against one ceiling. Under /dev/shm the
# filesystem's capacity is the whole host's, which would make --size no ceiling at all.
start_space_watchdog() {
	local target_pid=$1
	(
		local pct used cap_kb
		cap_kb=$(( SIZE_GB * 1024 * 1024 ))
		printf '0\n' > "$PEAK_FILE"
		while kill -0 "$target_pid" 2>/dev/null; do
			used=$(volume_used_kb 2>/dev/null || echo 0)
			[[ -n "$used" ]] || used=0
			pct=0
			(( cap_kb > 0 )) && pct=$(( used * 100 / cap_kb ))
			(( used > $(cat "$PEAK_FILE") )) && printf '%s\n' "$used" > "$PEAK_FILE"
			if (( pct >= 98 )); then
				: > "$EXHAUSTED_FILE"
				warn "RAM disk is ${pct}% full; terminating go test for a clean abort"
				kill -TERM "$target_pid" 2>/dev/null || true
				break
			elif (( pct >= 90 )); then
				warn "RAM disk is ${pct}% full; largest consumers:"
				du -sk "$MNT"/tmp "$MNT"/gotmp 2>/dev/null | sort -n | tail -5 >&2 || true
			fi
			sleep "$POLL_SECS"
		done
	) &
	WATCHDOG_PID=$!
}

stop_space_watchdog() {
	[[ -n "$WATCHDOG_PID" ]] && kill "$WATCHDOG_PID" 2>/dev/null || true
	WATCHDOG_PID=""
}

# go test flags that take their value as the following argument rather than after an "=". Their
# values are ordinary words like "1" or "TestFoo", and counting one of those as a package is
# what silently turns a whole-tree run into a root-package-only run that still exits 0.
#
# This list is the sole thing separating a flag's value from a package pattern, so a flag missing
# from it hands its value to go as a package name. That fails loudly -- go rejects the unknown
# flag or the unresolvable package -- which is the direction to be wrong in.
VALUE_FLAGS="
-run -bench -benchtime -fuzz -fuzztime -fuzzminimizetime -list -count -timeout -parallel -cpu
-shuffle -tags -coverprofile -covermode -coverpkg -cpuprofile -memprofile -memprofilerate
-blockprofile -blockprofilerate -mutexprofile -mutexprofilefraction -trace -outputdir -exec
-o -p -gcflags -ldflags -asmflags -gccgoflags -overlay -pkgdir -toolexec -mod -modfile
-buildmode -compiler -installsuffix -testlogfile
"

takes_separate_value() { [[ " $(echo $VALUE_FLAGS) " == *" $1 "* ]]; }

build_go_test_cmd() {
	GO_CMD=(go test)
	$USE_RACE && GO_CMD+=(-race)
	$USE_CI_TAGS && GO_CMD+=("-tags=$CI_TAGS")

	local has_pkg=false has_timeout=false skip_next=false arg
	for arg in ${GO_TEST_ARGS+"${GO_TEST_ARGS[@]}"}; do
		[[ "$arg" == -timeout* ]] && has_timeout=true
		# Everything past -args belongs to the test binary, so it names nothing here.
		[[ "$arg" == -args ]] && break
		if $skip_next; then
			skip_next=false
			continue
		fi
		if [[ "$arg" == -* ]]; then
			takes_separate_value "$arg" && skip_next=true
			continue
		fi
		# Whatever is left is positional, and go reads its positional arguments as the package
		# list. Judging the shape instead would reject the bare import paths go also accepts,
		# and appending the default beside one of those runs the whole tree unasked.
		has_pkg=true
	done
	$has_timeout || GO_CMD+=(-timeout=30m)

	# The default package has to precede -args, because go hands everything after that to the
	# test binary, where a package pattern would be read as one of its arguments.
	local placed=$has_pkg
	for arg in ${GO_TEST_ARGS+"${GO_TEST_ARGS[@]}"}; do
		if ! $placed && [[ "$arg" == -args ]]; then
			GO_CMD+=(./...)
			placed=true
		fi
		GO_CMD+=("$arg")
	done
	$placed || GO_CMD+=(./...)
}

run_tests() {
	log "running: ${GO_CMD[*]}"
	$DEGRADED || log "TMPDIR=$TMPDIR GOTMPDIR=$GOTMPDIR"
	$USE_CI_TAGS || log "note: no --ci-tags, so ledger tests do not run in $CI_TAG_PACKAGES"
	echo

	SWAP_BEFORE=$(swap_counter)
	local start=$SECONDS

	# An explicit FIFO rather than a process substitution, so that tee's pid is knowable and
	# can be waited on. diagnose_failure greps the log for ENOSPC, and ENOSPC lands in the
	# last few lines, which is exactly what is still in flight when go test exits. The FIFO
	# lives on the real filesystem because the RAM disk may be torn down before this returns.
	local fifo="$STATE_DIR/output.fifo"
	rm -f "$fifo"
	mkfifo "$fifo"

	set +e
	tee "$LOG_FILE" < "$fifo" &
	TEE_PID=$!
	"${GO_CMD[@]}" > "$fifo" 2>&1 &
	GO_PID=$!
	$DEGRADED || start_space_watchdog "$GO_PID"
	wait "$GO_PID"
	TEST_STATUS=$?
	wait "$TEE_PID"
	set -e
	rm -f "$fifo"
	stop_space_watchdog
	ELAPSED=$(( SECONDS - start ))
	SWAP_AFTER=$(swap_counter)
}

report() {
	local peak_kb=0 fstype=""
	[[ -f "$PEAK_FILE" ]] && peak_kb=$(cat "$PEAK_FILE")

	echo
	log "---- ramtest report ----"
	log "command : ${GO_CMD[*]}"
	log "elapsed : ${ELAPSED}s ($(( ELAPSED / 60 ))m$(( ELAPSED % 60 ))s)"
	if $DEGRADED; then
		log "location: normal filesystem (TMPDIR=${TMPDIR:-unset}) -- no RAM disk in use"
	else
		if [[ "$OS" == Darwin ]]; then
			fstype=$(diskutil info "$MNT" 2>/dev/null | awk -F': *' '/Type \(Bundle\)/ {print $2}')
		else
			fstype=$(stat -f -c %T "$MNT" 2>/dev/null || echo tmpfs)
		fi
		log "location: $MNT (${fstype:-unknown}, $SIZE_GB GiB)"
		log "peak use: $(( peak_kb / 1024 )) MiB of $(( SIZE_GB * 1024 )) MiB"
		if (( peak_kb == 0 )); then
			warn "nothing was written to the RAM disk. Either this run had no test data to"
			warn "  write, or TMPDIR did not take effect and it was not accelerated."
		fi
		du -sh "$MNT"/tmp "$MNT"/gotmp 2>/dev/null | sed "s/^/$PROG: usage  : /" || true
	fi

	# Swap growth is the one way this is a net loss: the RAM disk ends up partly on disk
	# and pays the compressor tax on top.
	if [[ -n "${SWAP_BEFORE:-}" && -n "${SWAP_AFTER:-}" && "$SWAP_BEFORE" != "$SWAP_AFTER" ]]; then
		if awk "BEGIN {exit !($SWAP_AFTER > $SWAP_BEFORE)}"; then
			warn "the host swapped during this run ($SWAP_BEFORE -> $SWAP_AFTER); the RAM disk was"
			warn "  partly on disk. Lower --size, or drop --race."
		fi
	fi
	# Names the timeout because go's default is 10m and the slowest sei-db package takes
	# over 8 minutes on a physical disk, so a baseline typed without it can fail for a
	# reason that has nothing to do with what is being compared.
	$DEGRADED || log "compare : go test -timeout=30m ${GO_TEST_ARGS[*]:-./...}"
	log "log     : $LOG_FILE"
}

diagnose_failure() {
	(( TEST_STATUS == 0 )) && return 0

	local peak_kb=0 cap_kb pct=0
	[[ -f "$PEAK_FILE" ]] && peak_kb=$(cat "$PEAK_FILE")
	cap_kb=$(( SIZE_GB * 1024 * 1024 ))
	(( cap_kb > 0 )) && pct=$(( peak_kb * 100 / cap_kb ))

	# Two independent kinds of evidence, reported differently. An ENOSPC in the log is proof;
	# a high sampled peak is only an inference. They are separated because the sampler runs on
	# an interval and a test that fills the volume in under one interval leaves a peak far
	# below capacity -- reporting that percentage as the reason reads as a contradiction.
	if [[ -f "$EXHAUSTED_FILE" ]] ||
		grep -qE 'no space left on device|ENOSPC' "$LOG_FILE" 2>/dev/null; then
		echo
		warn "the RAM disk ran out of space. This is not a test failure."
		warn "  Re-run with --size $(( SIZE_GB * 2 )) (currently $SIZE_GB GiB)."
		warn "  Sampled peak was $(( peak_kb / 1024 )) MiB; the sampler polls every ${POLL_SECS}s, so"
		warn "  it under-reads a volume that fills faster than that."
		exit $EX_EXHAUSTED
	fi

	if (( pct >= 95 )); then
		echo
		warn "the RAM disk reached ${pct}% of $(( SIZE_GB * 1024 )) MiB, so this failure may be a"
		warn "  space problem rather than a real one. Re-run with --size $(( SIZE_GB * 2 )) to rule it out."
		exit $EX_EXHAUSTED
	fi

	if (( TEST_STATUS == 137 )) && [[ "$OS" == Linux ]]; then
		echo
		warn "go test was SIGKILLed. In a memory-limited container, tmpfs pages count against"
		warn "  the cgroup, so filling the RAM disk gets the process OOM-killed rather than"
		warn "  returning ENOSPC. Lower --size."
		exit $EX_EXHAUSTED
	fi

	if (( TEST_STATUS == EX_INTERRUPTED )); then
		exit $EX_INTERRUPTED
	fi

	log "tests failed (exit $TEST_STATUS); the RAM disk was not the problem"
	exit $EX_TESTFAIL
}

teardown_darwin() {
	diskutil unmount "$MNT" >/dev/null 2>&1 || diskutil unmount force "$MNT" >/dev/null 2>&1 || true
	# Unmounting alone does not hand the memory back; only detaching the device does. Every
	# Darwin release passes through here, so not knowing the device is reported as the leak it
	# is rather than being taken for a clean teardown.
	if [[ -z "${DEV:-}" ]]; then
		warn "no device recorded for $MNT, so it could not be detached and its memory stays"
		warn "  reserved. Find it and release it with:"
		warn "  hdiutil info | grep -B12 $LABEL"
		return $EX_LEAKED
	fi
	hdiutil detach "$DEV" >/dev/null 2>&1 || hdiutil detach -force "$DEV" >/dev/null 2>&1 || {
		warn "could not detach $DEV; ${SIZE_GB:-?} GiB of memory stays reserved until you run:"
		warn "  hdiutil detach -force $DEV"
		return $EX_LEAKED
	}
	return 0
}

teardown_linux() {
	if [[ "$VOLUME_KIND" == tmpfs ]]; then
		# Unmounting is what returns the pages. Deleting through a live mount would empty it
		# and leave the mount, which is worse than leaving it alone.
		sudo umount "$MNT" 2>/dev/null || {
			warn "could not unmount $MNT; $SIZE_GB GiB stays reserved until you run:"
			warn "  sudo umount $MNT"
			return $EX_LEAKED
		}
		sudo rmdir "$MNT" 2>/dev/null || true
		return 0
	fi
	# /dev/shm is shared with the rest of the host, so this deletes a directory only after
	# confirming it is the one this worktree stamped.
	if [[ -f "$MNT/$OWNER_MARKER" ]] && ! grep -qxF "$REPO_ROOT" "$MNT/$OWNER_MARKER"; then
		warn "$MNT belongs to another worktree; leaving it alone"
		return 0
	fi
	rm -rf "$MNT"
	return 0
}

teardown_ramdisk() {
	volume_is_releasable || { clear_state; return 0; }
	log "tearing down $MNT"
	local rc=0
	case "$VOLUME_KIND" in
	darwin) teardown_darwin || rc=$? ;;
	*) teardown_linux || rc=$? ;;
	esac
	# The record is the only way back to a volume that outlived its run, so it is dropped only
	# once there is nothing left to find.
	(( rc == 0 )) && clear_state
	return $rc
}

cleanup() {
	local rc=$?
	stop_space_watchdog
	if [[ "${CREATED_HERE:-false}" == true ]] && ! $KEEP && ! $PRINT_ENV; then
		# A leak that goes unreported reads as a clean run, so it becomes the exit status
		# whenever the tests themselves had nothing to say.
		if ! teardown_ramdisk && (( rc == 0 )); then
			rc=$EX_LEAKED
		fi
	fi
	exit "$rc"
}

main() {
	parse_args "$@"
	detect_platform
	init_paths

	if $DOWN_ONLY; then
		reap_orphans
		local rc=0
		teardown_ramdisk || rc=$?
		exit $rc
	fi

	resolve_size
	build_go_test_cmd

	# Armed before anything is created, so a failure between creating the volume and starting
	# the tests still hands the memory back.
	trap cleanup EXIT INT TERM
	reap_orphans
	ensure_ramdisk

	if ! $DEGRADED; then
		prepare_dirs
		export_env
		record_baseline_usage
	fi

	if $PRINT_ENV; then
		# There is nothing to export when the fallback took effect, and printing empty
		# assignments would be worse than saying so.
		if $DEGRADED; then
			die "no RAM disk was available, so there is no environment to print" $EX_NORAM
		fi
		cat <<EOF
export TMPDIR="$TMPDIR"
export TMP="$TMPDIR"
export TEMP="$TMPDIR"
export GOTMPDIR="$GOTMPDIR"
# This volume outlives the script on purpose. Tear it down when you are done -- whatever
# it still holds stays in RAM until you do, and nothing else will release it:
#   $0 --down
EOF
		exit 0
	fi

	run_tests
	report
	diagnose_failure
}

main "$@"
