"""The stdio client: one subprocess, one JSON object per line, each way.

Everything about this module is about the subprocess never becoming a problem
of its own. A training run that spawns thousands of episodes cannot afford a
leaked process, a deadlocked read, or a stack trace on stderr swallowed into
nothing — so reads are bounded by a timeout, stderr is drained continuously
into a ring buffer that error messages can quote, and shutdown is layered
(``close`` → ``terminate`` → ``kill``) with a finalizer behind it.
"""

from __future__ import annotations

import atexit
import json
import os
import queue
import subprocess
import threading
import weakref
from collections import deque
from typing import Any, Dict, List, Optional

from ._binary import find_binary
from .errors import (
    EngineClosedError,
    EngineError,
    EngineTimeoutError,
    IllegalActionError,
    ProtocolError,
)

__all__ = ["EngineClient", "DEFAULT_TIMEOUT"]

#: Default per-request read timeout, in seconds. Generous: an expectimax
#: opponent at depth 3 on a bad turn is the slow case, and a spurious timeout
#: is worse than a slow step.
DEFAULT_TIMEOUT = 120.0

#: How long to wait for the process to exit at each shutdown stage.
_SHUTDOWN_GRACE = 5.0

#: How many stderr lines to keep for error messages.
_STDERR_LINES = 40

# Live clients, so an interpreter exit cannot leave engine processes behind
# even if a caller forgot to close one.
_LIVE: "weakref.WeakSet[EngineClient]" = weakref.WeakSet()


@atexit.register
def _close_all() -> None:  # pragma: no cover - exercised only at interpreter exit
    for client in list(_LIVE):
        try:
            client.close()
        except Exception:
            pass


class EngineClient:
    """A running ``pokearena-env`` process.

    One client is one environment instance: the binary holds at most one
    episode at a time, which is what keeps each episode's RNG stream trivially
    isolated. Run N clients for N parallel environments.

    Use it as a context manager, or call :meth:`close` when done::

        with EngineClient() as engine:
            engine.request("reset", {"seed": 0, "team": "Genesis"})
    """

    def __init__(
        self,
        binary: Optional[str] = None,
        *,
        data_dir: Optional[str] = None,
        teams: Optional[str] = None,
        expectimax_depth: Optional[int] = None,
        timeout: float = DEFAULT_TIMEOUT,
        extra_args: Optional[List[str]] = None,
    ):
        self._timeout = float(timeout)
        self._binary = find_binary(binary)

        argv = [self._binary]
        if data_dir:
            argv += ["-data", str(data_dir)]
        if teams:
            argv += ["-teams", str(teams)]
        if expectimax_depth:
            argv += ["-depth", str(int(expectimax_depth))]
        if extra_args:
            argv += list(extra_args)
        self.argv = argv

        try:
            self._proc: Optional[subprocess.Popen] = subprocess.Popen(
                argv,
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                bufsize=1,  # line buffered: the protocol is line oriented
                # A new process group would orphan the child if we were killed
                # mid-run; staying in ours means a Ctrl-C reaches it too.
                env=os.environ.copy(),
            )
        except OSError as exc:
            raise EngineClosedError(f"could not start {self._binary}: {exc}") from exc

        self._closed = False
        self._lock = threading.Lock()
        self._stdout_q: "queue.Queue[Optional[str]]" = queue.Queue()
        self._stderr: "deque[str]" = deque(maxlen=_STDERR_LINES)

        # Reader threads. Both are daemons: they must never keep the
        # interpreter alive, and both terminate on EOF when the child exits.
        self._readers = [
            threading.Thread(target=self._pump_stdout, name="pokearena-stdout", daemon=True),
            threading.Thread(target=self._pump_stderr, name="pokearena-stderr", daemon=True),
        ]
        for t in self._readers:
            t.start()

        _LIVE.add(self)

        # Fail fast and loudly if the binary cannot even introduce itself:
        # a startup error (a missing dataset directory, say) is otherwise only
        # visible as a mysterious timeout on the first reset.
        self.handshake: Dict[str, Any] = self.request("handshake")

    # -- plumbing ---------------------------------------------------------

    def _pump_stdout(self) -> None:
        assert self._proc is not None and self._proc.stdout is not None
        try:
            for line in self._proc.stdout:
                self._stdout_q.put(line)
        except (ValueError, OSError):  # pipe closed under us
            pass
        finally:
            self._stdout_q.put(None)  # sentinel: EOF

    def _pump_stderr(self) -> None:
        assert self._proc is not None and self._proc.stderr is not None
        try:
            for line in self._proc.stderr:
                self._stderr.append(line.rstrip("\n"))
        except (ValueError, OSError):
            pass

    def _stderr_tail(self) -> str:
        if not self._stderr:
            return ""
        return "\n  stderr: " + "\n          ".join(self._stderr)

    @property
    def closed(self) -> bool:
        return self._closed or self._proc is None

    def _ensure_open(self) -> subprocess.Popen:
        if self._closed or self._proc is None:
            raise EngineClosedError("the engine process is closed")
        if self._proc.poll() is not None:
            code = self._proc.returncode
            raise EngineClosedError(f"the engine process exited with code {code}{self._stderr_tail()}")
        return self._proc

    # -- protocol ---------------------------------------------------------

    def request(
        self,
        cmd: str,
        args: Optional[Dict[str, Any]] = None,
        *,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        """Send one command and return its ``result``.

        Raises:
            IllegalActionError: the action was rejected (episode untouched).
            EngineError: any other engine-side rejection.
            EngineTimeoutError: no reply within the timeout.
            EngineClosedError: the process is gone.
            ProtocolError: the reply was not a valid protocol message.
        """
        payload: Dict[str, Any] = {"cmd": cmd}
        if args:
            payload["args"] = args
        line = json.dumps(payload, separators=(",", ":"), ensure_ascii=False)

        wait = self._timeout if timeout is None else float(timeout)

        # One request at a time. The protocol is strictly request/response over
        # a single pipe pair, so two concurrent callers would interleave lines
        # and each read the other's answer.
        with self._lock:
            proc = self._ensure_open()
            assert proc.stdin is not None
            try:
                proc.stdin.write(line + "\n")
                proc.stdin.flush()
            except (BrokenPipeError, ValueError, OSError) as exc:
                raise EngineClosedError(
                    f"the engine process closed its input while sending {cmd!r}: {exc}{self._stderr_tail()}"
                ) from exc

            try:
                reply = self._stdout_q.get(timeout=wait)
            except queue.Empty:
                raise EngineTimeoutError(
                    f"the engine did not answer {cmd!r} within {wait:g}s{self._stderr_tail()}"
                ) from None

        if reply is None:
            raise EngineClosedError(
                f"the engine process exited while handling {cmd!r}{self._stderr_tail()}"
            )

        try:
            resp = json.loads(reply)
        except json.JSONDecodeError as exc:
            raise ProtocolError(f"engine wrote a non-JSON line: {reply!r}") from exc
        if not isinstance(resp, dict):
            raise ProtocolError(f"engine wrote a non-object response: {reply!r}")

        if resp.get("ok"):
            result = resp.get("result")
            return result if isinstance(result, dict) else {}

        err = resp.get("error") or {}
        code = err.get("code", "unknown")
        message = err.get("message", "(no message)")
        details = err.get("details") or {}
        if code == "illegal_action":
            raise IllegalActionError(code, message, details)
        raise EngineError(code, message, details)

    # -- lifecycle --------------------------------------------------------

    def close(self, timeout: float = _SHUTDOWN_GRACE) -> None:
        """Shut the engine down. Safe to call more than once.

        Escalates politely: a ``close`` command, then closing stdin (the binary
        treats EOF as a clean exit), then SIGTERM, then SIGKILL. Each stage
        exists because the one before it can fail — a wedged process must not
        survive this call.
        """
        if self._closed:
            return
        self._closed = True
        proc, self._proc = self._proc, None
        _LIVE.discard(self)
        if proc is None:
            return

        try:
            if proc.poll() is None and proc.stdin is not None:
                try:
                    proc.stdin.write('{"cmd":"close"}\n')
                    proc.stdin.flush()
                except (BrokenPipeError, ValueError, OSError):
                    pass
            for stream in (proc.stdin,):
                if stream is not None:
                    try:
                        stream.close()
                    except (BrokenPipeError, ValueError, OSError):
                        pass
            try:
                proc.wait(timeout=timeout)
            except subprocess.TimeoutExpired:
                proc.terminate()
                try:
                    proc.wait(timeout=timeout)
                except subprocess.TimeoutExpired:  # pragma: no cover - last resort
                    proc.kill()
                    proc.wait(timeout=timeout)
        finally:
            for stream in (proc.stdout, proc.stderr):
                if stream is not None:
                    try:
                        stream.close()
                    except (BrokenPipeError, ValueError, OSError):
                        pass

    def __enter__(self) -> "EngineClient":
        return self

    def __exit__(self, *exc_info) -> None:
        self.close()

    def __del__(self) -> None:  # pragma: no cover - GC timing
        try:
            self.close()
        except Exception:
            pass
