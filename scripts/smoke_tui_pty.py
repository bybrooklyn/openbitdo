#!/usr/bin/env python3
"""Launch a packaged mock TUI in a real PTY and quit it cleanly."""

from __future__ import annotations

import fcntl
import os
import pty
import select
import signal
import struct
import sys
import tempfile
import termios
import time


def main() -> int:
    if len(sys.argv) != 2:
        print(f"usage: {sys.argv[0]} <openbitdo-binary>", file=sys.stderr)
        return 2

    binary = os.path.abspath(sys.argv[1])
    if not os.path.isfile(binary) or not os.access(binary, os.X_OK):
        print(f"missing or non-executable binary: {binary}", file=sys.stderr)
        return 1

    with tempfile.TemporaryDirectory(prefix="openbitdo-pty-") as temp_root:
        pid, master = pty.fork()
        if pid == 0:
            env = os.environ.copy()
            env["HOME"] = os.path.join(temp_root, "home")
            env["XDG_CONFIG_HOME"] = os.path.join(temp_root, "config")
            env.setdefault("TERM", "xterm-256color")
            os.makedirs(env["HOME"], exist_ok=True)
            os.makedirs(env["XDG_CONFIG_HOME"], exist_ok=True)
            os.execve(binary, [binary, "--mock"], env)

        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
        output = bytearray()
        deadline = time.monotonic() + 15
        sent_quit = False
        status: int | None = None
        try:
            while time.monotonic() < deadline:
                ready, _, _ = select.select([master], [], [], 0.2)
                if ready:
                    try:
                        chunk = os.read(master, 65536)
                    except OSError:
                        chunk = b""
                    output.extend(chunk)
                    if b"OpenBitdo" in output and not sent_quit:
                        # Initial PTY mode-setting escape sequences can arrive
                        # before Bubble Tea starts its event loop. Wait for the
                        # rendered application header so the ordinary safe quit
                        # key cannot be sent early and silently dropped.
                        os.write(master, b"q")
                        sent_quit = True

                waited_pid, waited_status = os.waitpid(pid, os.WNOHANG)
                if waited_pid == pid:
                    status = waited_status
                    break
        finally:
            os.close(master)

        if status is None:
            os.kill(pid, signal.SIGINT)
            _, status = os.waitpid(pid, 0)
            if not sent_quit:
                print("mock TUI never rendered its OpenBitdo header", file=sys.stderr)
                return 1
            print("mock TUI did not exit after q within 15 seconds", file=sys.stderr)
            return 1
        if not output:
            print("mock TUI produced no PTY output", file=sys.stderr)
            return 1
        if not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0:
            print(f"mock TUI exited abnormally (wait status {status})", file=sys.stderr)
            return 1

    print("packaged mock TUI launched and quit cleanly in a PTY")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
