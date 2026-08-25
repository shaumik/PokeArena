"""Locating the ``pokearena-env`` engine binary.

This package is a thin client; the engine itself is a Go binary. That is an
honest constraint rather than something to paper over — there is no pure-Python
Pokémon engine hiding in this wheel, and pretending otherwise would only move
the failure from install time to the middle of someone's training run.

So: find the binary, and if it is not there, say exactly how to get it.
"""

from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path
from typing import List, Optional

from .errors import BinaryNotFoundError

__all__ = ["BINARY_NAME", "ENV_VAR", "GO_INSTALL_COMMAND", "find_binary", "binary_version"]

#: The executable this package drives.
BINARY_NAME = "pokearena-env"

#: Environment variable naming an explicit path to it.
ENV_VAR = "POKEARENA_ENV_BIN"

#: The one-liner that builds it.
GO_INSTALL_COMMAND = "go install github.com/shaumik/PokeArena/cmd/pokearena-env@latest"

_REPO = "https://github.com/shaumik/PokeArena"


def _candidate_paths() -> List[Path]:
    """Every place we look, in order.

    PATH comes first so a system-installed binary wins by default; the
    environment variable is the explicit override for a build that is not on
    PATH; and ``go install``'s default output directory is checked last,
    because that is where the recommended install command actually puts it and
    plenty of people never add it to PATH.
    """
    out: List[Path] = []

    on_path = shutil.which(BINARY_NAME)
    if on_path:
        out.append(Path(on_path))

    env = os.environ.get(ENV_VAR)
    if env:
        out.append(Path(env).expanduser())

    gobin = os.environ.get("GOBIN")
    if gobin:
        out.append(Path(gobin).expanduser() / BINARY_NAME)

    gopath = os.environ.get("GOPATH")
    roots = [Path(p).expanduser() for p in gopath.split(os.pathsep)] if gopath else []
    roots.append(Path.home() / "go")
    for root in roots:
        out.append(root / "bin" / BINARY_NAME)

    return out


def _usable(path: Path) -> bool:
    return path.is_file() and os.access(str(path), os.X_OK)


def find_binary(explicit: Optional[str] = None) -> str:
    """Return the path to the engine binary.

    Args:
        explicit: a path supplied by the caller, which wins over everything.

    Raises:
        BinaryNotFoundError: with an actionable message naming every place that
            was searched and both ways to obtain the binary.
    """
    if explicit:
        p = Path(explicit).expanduser()
        if _usable(p):
            return str(p)
        raise BinaryNotFoundError(
            f"{BINARY_NAME} was not found at the path you gave ({explicit!r}), "
            "or it is not executable."
        )

    searched = _candidate_paths()
    for p in searched:
        if _usable(p):
            return str(p)

    looked = "\n".join(f"    {p}" for p in searched) or "    (nothing on PATH)"
    raise BinaryNotFoundError(
        f"the PokéArena engine binary {BINARY_NAME!r} was not found.\n"
        f"\n"
        f"This package is a client for a Go binary — it does not contain the\n"
        f"engine itself. Get the binary one of two ways:\n"
        f"\n"
        f"  1. With a Go toolchain (1.22+):\n"
        f"       {GO_INSTALL_COMMAND}\n"
        f"     then make sure the install directory is on your PATH\n"
        f"     (it is `go env GOPATH`/bin, usually ~/go/bin).\n"
        f"\n"
        f"  2. Download a prebuilt binary for your platform from\n"
        f"       {_REPO}/releases\n"
        f"     and either put it on your PATH or point {ENV_VAR} at it:\n"
        f"       export {ENV_VAR}=/path/to/{BINARY_NAME}\n"
        f"\n"
        f"Looked in:\n{looked}"
    )


def binary_version(path: Optional[str] = None, timeout: float = 10.0) -> str:
    """Return the stdio protocol version the binary implements.

    Useful as a cheap liveness/compatibility check that does not start an
    episode.
    """
    exe = find_binary(path)
    try:
        out = subprocess.run(
            [exe, "-protocol-version"],
            capture_output=True,
            text=True,
            timeout=timeout,
            check=True,
        )
    except (OSError, subprocess.SubprocessError) as exc:  # pragma: no cover - environment specific
        raise BinaryNotFoundError(f"could not run {exe}: {exc}") from exc
    return out.stdout.strip()
