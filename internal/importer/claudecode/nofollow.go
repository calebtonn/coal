package claudecode

import "syscall"

// noFollow makes opening a transcript fail if it was swapped for a symlink.
const noFollow = syscall.O_NOFOLLOW
