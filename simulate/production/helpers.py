"""Shared helpers for production readiness QA simulations.

Extends the resilience helpers with HTTP response inspection for
middleware, probe, and cache header verification.
"""

import json
import os
import subprocess
import sys
import time
import urllib.request
import urllib.error

# Re-export everything from resilience helpers so test scripts can
# import from a single module.  Use importlib to avoid circular import
# (this file is also called helpers.py).
import importlib.util as _ilu

_resilience_path = os.path.join(os.path.dirname(__file__), "..", "resilience", "helpers.py")
_spec = _ilu.spec_from_file_location("resilience_helpers", _resilience_path)
_rh = _ilu.module_from_spec(_spec)
_spec.loader.exec_module(_rh)

REPO_ROOT = _rh.REPO_ROOT
DATABASE_URL = _rh.DATABASE_URL
ANVIL_PORT = _rh.ANVIL_PORT
CHAIN_ID = _rh.CHAIN_ID
cleanup = _rh.cleanup
register = _rh.register
start_anvil = _rh.start_anvil
deploy_contracts = _rh.deploy_contracts
truncate_all = _rh.truncate_all
wait_for_cursor = _rh.wait_for_cursor
check = _rh.check
results = _rh.results
reset_results = _rh.reset_results
print_summary = _rh.print_summary
_wait_ready = _rh._wait_ready

API_PORT = int(os.environ.get("API_PORT", "8080"))


# ---------------------------------------------------------------------------
# HTTP response wrapper
# ---------------------------------------------------------------------------


class HTTPResponse:
    """Wraps urllib response to expose status, headers, and body."""

    def __init__(self, status: int, headers: dict[str, str], body: bytes):
        self.status = status
        self.headers = headers
        self.body = body

    def json(self) -> dict:
        return json.loads(self.body)

    def header(self, name: str) -> str | None:
        """Case-insensitive header lookup."""
        lower = name.lower()
        for k, v in self.headers.items():
            if k.lower() == lower:
                return v
        return None


def http_request(
    url: str,
    method: str = "GET",
    headers: dict[str, str] | None = None,
    timeout: int = 10,
) -> HTTPResponse:
    """Make an HTTP request and return status + headers + body.

    Does NOT raise on 4xx/5xx — the caller inspects the response.
    """
    req = urllib.request.Request(url, method=method)
    if headers:
        for k, v in headers.items():
            req.add_header(k, v)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            resp_headers = {k: v for k, v in resp.getheaders()}
            return HTTPResponse(resp.status, resp_headers, resp.read())
    except urllib.error.HTTPError as e:
        resp_headers = {k: v for k, v in e.headers.items()}
        return HTTPResponse(e.code, resp_headers, e.read())


# ---------------------------------------------------------------------------
# API server launcher
# ---------------------------------------------------------------------------


def start_api(
    database_url: str = DATABASE_URL,
    port: int = API_PORT,
    extra_env: dict | None = None,
) -> subprocess.Popen:
    """Start the API server and wait for it to become ready."""
    print(f"==> Starting API on port {port}...")
    env = {
        **os.environ,
        "DATABASE_URL": database_url,
        "CHAIN_ID": str(CHAIN_ID),
        "PORT": str(port),
        "LOG_LEVEL": "info",
        "LOG_FORMAT": "text",
    }
    if extra_env:
        env.update(extra_env)
    proc = subprocess.Popen(
        ["go", "run", "./cmd/api"],
        cwd=REPO_ROOT,
        env=env,
        preexec_fn=os.setsid,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    register(proc)
    api_url = f"http://127.0.0.1:{port}"
    _wait_ready(lambda: http_request(f"{api_url}/health"), "API server", timeout=30)
    print(f"    API running (PID {proc.pid})")
    return proc


# ---------------------------------------------------------------------------
# Indexer launcher (re-export with API-friendly defaults)
# ---------------------------------------------------------------------------


def start_indexer(
    rpc_url: str,
    factory_addr: str,
    database_url: str = DATABASE_URL,
) -> subprocess.Popen:
    """Start the indexer and return its process handle."""
    print("==> Starting indexer...")
    env = {
        **os.environ,
        "DATABASE_URL": database_url,
        "RPC_URL": rpc_url,
        "CHAIN_ID": str(CHAIN_ID),
        "FACTORY_ADDRESS": factory_addr,
        "START_BLOCK": "0",
        "CONFIRMATIONS": "0",
        "POLL_INTERVAL": "1s",
        "BLOCK_BATCH_SIZE": "100",
        "MAX_BLOCK_RANGE": "2000",
        "LOG_LEVEL": "info",
        "LOG_FORMAT": "text",
    }
    proc = subprocess.Popen(
        ["go", "run", "./cmd/indexer"],
        cwd=REPO_ROOT,
        env=env,
        preexec_fn=os.setsid,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    register(proc)
    print(f"    Indexer running (PID {proc.pid})")
    return proc


# ---------------------------------------------------------------------------
# Cache-Control directive parser
# ---------------------------------------------------------------------------


def parse_cache_control(header: str | None) -> dict[str, str | bool]:
    """Parse a Cache-Control header into a dict of directives.

    Valueless directives (e.g. 'immutable', 'no-store') map to True.
    Key=value directives (e.g. 'max-age=86400') map to the string value.
    """
    if not header:
        return {}
    directives: dict[str, str | bool] = {}
    for part in header.split(","):
        part = part.strip()
        if "=" in part:
            k, v = part.split("=", 1)
            directives[k.strip().lower()] = v.strip()
        elif part:
            directives[part.strip().lower()] = True
    return directives


# ---------------------------------------------------------------------------
# Postgres stop/start for readiness probe testing
# ---------------------------------------------------------------------------


def stop_postgres() -> None:
    """Stop the local Postgres server (macOS brew or pg_ctl)."""
    print("    Stopping Postgres...")
    # Detect running brew Postgres service name
    svc_name = _detect_pg_brew_service()
    if svc_name:
        subprocess.run(
            ["brew", "services", "stop", svc_name],
            capture_output=True, text=True, timeout=15,
        )
    else:
        # Fallback to pg_ctl
        subprocess.run(
            ["pg_ctl", "stop", "-m", "fast"],
            capture_output=True, text=True, timeout=15,
        )
    # Wait until psql actually fails — the pool needs time to detect
    # the dead connection.
    deadline = time.time() + 15
    while time.time() < deadline:
        r = subprocess.run(
            ["psql", DATABASE_URL, "-c", "SELECT 1"],
            capture_output=True, text=True, timeout=5,
        )
        if r.returncode != 0:
            # Give pool a moment to expire cached connections
            time.sleep(1)
            return
        time.sleep(0.5)
    print("    WARNING: Postgres may still be accepting connections")


_pg_brew_service: str | None = None


def _detect_pg_brew_service() -> str | None:
    """Detect the Homebrew Postgres service name (e.g. postgresql@14)."""
    global _pg_brew_service
    if _pg_brew_service is not None:
        return _pg_brew_service
    result = subprocess.run(
        ["brew", "services", "list"],
        capture_output=True, text=True, timeout=10,
    )
    for line in result.stdout.splitlines():
        if "postgresql" in line.lower():
            parts = line.split()
            if parts:
                _pg_brew_service = parts[0]
                return _pg_brew_service
    return None


def start_postgres() -> None:
    """Start the local Postgres server (macOS brew or pg_ctl)."""
    print("    Starting Postgres...")
    svc_name = _detect_pg_brew_service()
    if svc_name:
        subprocess.run(
            ["brew", "services", "start", svc_name],
            capture_output=True, text=True, timeout=15,
        )
    else:
        subprocess.run(
            ["pg_ctl", "start"],
            capture_output=True, text=True, timeout=15,
        )
    # Wait for Postgres to accept connections
    deadline = time.time() + 15
    while time.time() < deadline:
        try:
            subprocess.run(
                ["psql", DATABASE_URL, "-c", "SELECT 1"],
                capture_output=True, text=True, timeout=5,
            )
            return
        except Exception:
            time.sleep(0.5)
    raise TimeoutError("Postgres did not start within 15s")
