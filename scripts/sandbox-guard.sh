# Sourced by the scripts in this directory. in_sandbox_or_die NAME PATH
# refuses PATH unless it lies inside the repo's .sandbox/, so a script
# can never point at ~/.coal or a real project by accident. Set
# COAL_ALLOW_OUTSIDE_SANDBOX=1 to allow it on purpose.
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
sandbox="$repo/.sandbox"

inside_sandbox() {
	local p
	p=$(realpath -m -- "$1")
	case "$p" in "$(realpath -m -- "$sandbox")"/*) return 0 ;; esac
	return 1
}

in_sandbox_or_die() {
	inside_sandbox "$2" && return 0
	if [ "${COAL_ALLOW_OUTSIDE_SANDBOX:-}" = 1 ]; then
		echo "warning: $1=$2 is outside $sandbox (COAL_ALLOW_OUTSIDE_SANDBOX=1)" >&2
		return 0
	fi
	echo "refusing $1=$2: it is outside $sandbox; set COAL_ALLOW_OUTSIDE_SANDBOX=1 to allow it" >&2
	exit 2
}
