"""Shared helpers for resilience QA simulations."""

import json
import os
import signal
import subprocess
import sys
import time
import urllib.request
import urllib.error

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
CHAIN_DIR = os.path.join(os.path.dirname(__file__), "..", "chain")
PROXY_SCRIPT = os.path.join(os.path.dirname(__file__), "faultproxy.py")

DATABASE_URL = os.environ.get(
    "DATABASE_URL",
    "postgres://cca:cca@localhost:5432/cca_indexer?sslmode=disable",
)
ANVIL_PORT = int(os.environ.get("ANVIL_PORT", "8545"))
PROXY_PORT = int(os.environ.get("PROXY_PORT", "9545"))
CHAIN_ID = 31337

# ---------------------------------------------------------------------------
# Process management
# ---------------------------------------------------------------------------

_procs: list[subprocess.Popen] = []


def _kill(proc: subprocess.Popen) -> None:
    if proc.poll() is None:
        try:
            os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
        except (ProcessLookupError, OSError):
            pass
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(os.getpgid(proc.pid), signal.SIGKILL)


def cleanup() -> None:
    for p in reversed(_procs):
        _kill(p)
    _procs.clear()


def register(proc: subprocess.Popen) -> subprocess.Popen:
    _procs.append(proc)
    return proc


# ---------------------------------------------------------------------------
# JSON-RPC helpers
# ---------------------------------------------------------------------------


def rpc_call(url: str, method: str, params=None):
    """Send a JSON-RPC request and return the result field."""
    payload = json.dumps({
        "jsonrpc": "2.0",
        "method": method,
        "params": params or [],
        "id": 1,
    }).encode()
    req = urllib.request.Request(
        url,
        data=payload,
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=10) as resp:
        body = json.loads(resp.read())
    if "error" in body and body["error"]:
        raise RuntimeError(f"RPC error: {body['error']}")
    return body.get("result")


def get_block_number(url: str) -> int:
    result = rpc_call(url, "eth_blockNumber")
    return int(result, 16)


def mine_blocks(url: str, n: int) -> None:
    for _ in range(n):
        rpc_call(url, "evm_mine")


def evm_snapshot(url: str) -> str:
    return rpc_call(url, "evm_snapshot")


def evm_revert(url: str, snapshot_id: str) -> bool:
    return rpc_call(url, "evm_revert", [snapshot_id])


def get_block_hash(url: str, block_num: int) -> str:
    result = rpc_call(url, "eth_getBlockByNumber", [hex(block_num), False])
    if result is None:
        return ""
    return result["hash"]


# ---------------------------------------------------------------------------
# Anvil
# ---------------------------------------------------------------------------


def start_anvil(port: int = ANVIL_PORT) -> subprocess.Popen:
    print(f"==> Starting Anvil on port {port}...")
    proc = subprocess.Popen(
        ["anvil", "--port", str(port), "--silent"],
        preexec_fn=os.setsid,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    register(proc)
    url = f"http://127.0.0.1:{port}"
    _wait_ready(lambda: rpc_call(url, "eth_blockNumber"), "Anvil", timeout=10)
    print(f"    Anvil running (PID {proc.pid})")
    return proc


# ---------------------------------------------------------------------------
# Contract deployment
# ---------------------------------------------------------------------------


def deploy_contracts(rpc_url: str) -> tuple[str, str]:
    """Deploy CCA factory and create test auction. Returns (factory_addr, auction_addr)."""
    print("==> Deploying contracts...")
    env = {**os.environ, "RPC_URL": rpc_url}
    result = subprocess.run(
        ["bash", os.path.join(CHAIN_DIR, "deploy.sh")],
        capture_output=True,
        text=True,
        env=env,
        cwd=CHAIN_DIR,
    )
    if result.returncode != 0:
        print(result.stdout)
        print(result.stderr)
        raise RuntimeError(f"deploy.sh failed (exit {result.returncode})")

    output = result.stdout + "\n" + result.stderr
    factory_addr = ""
    auction_addr = ""
    for line in output.splitlines():
        lower = line.lower()
        if "factory deployed to:" in lower:
            factory_addr = line.split()[-1].strip()
        if "auction created at:" in lower:
            auction_addr = line.split()[-1].strip()

    if not factory_addr or not auction_addr:
        print(output)
        raise RuntimeError("Failed to parse contract addresses from deploy output")

    print(f"    Factory: {factory_addr}")
    print(f"    Auction: {auction_addr}")
    return factory_addr, auction_addr


# ---------------------------------------------------------------------------
# Fault proxy
# ---------------------------------------------------------------------------


def start_proxy(upstream: str, port: int = PROXY_PORT) -> subprocess.Popen:
    print(f"==> Starting fault proxy on port {port} -> {upstream}...")
    proc = subprocess.Popen(
        [
            sys.executable, PROXY_SCRIPT,
            "--upstream", upstream,
            "--port", str(port),
        ],
        preexec_fn=os.setsid,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    register(proc)
    proxy_url = f"http://127.0.0.1:{port}"
    _wait_ready(lambda: _http_get(f"{proxy_url}/_fault/status"), "fault proxy", timeout=10)
    print(f"    Proxy running (PID {proc.pid})")
    return proc


def fault_on(proxy_url: str) -> None:
    _http_post(f"{proxy_url}/_fault/on")
    print("    Fault injection ENABLED")


def fault_off(proxy_url: str) -> None:
    _http_post(f"{proxy_url}/_fault/off")
    print("    Fault injection DISABLED")


# ---------------------------------------------------------------------------
# Indexer
# ---------------------------------------------------------------------------


def start_indexer(
    rpc_url: str,
    factory_addr: str,
    database_url: str = DATABASE_URL,
    poll_interval: str = "1s",
    confirmations: int = 0,
    extra_env: dict | None = None,
) -> subprocess.Popen:
    print("==> Starting indexer...")
    env = {
        **os.environ,
        "DATABASE_URL": database_url,
        "RPC_URL": rpc_url,
        "CHAIN_ID": str(CHAIN_ID),
        "FACTORY_ADDRESS": factory_addr,
        "START_BLOCK": "0",
        "CONFIRMATIONS": str(confirmations),
        "POLL_INTERVAL": poll_interval,
        "BLOCK_BATCH_SIZE": "100",
        "MAX_BLOCK_RANGE": "2000",
        "LOG_LEVEL": "info",
        "LOG_FORMAT": "text",
    }
    if extra_env:
        env.update(extra_env)
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
# Database queries (via psql)
# ---------------------------------------------------------------------------


def _psql(database_url: str, query: str) -> str:
    result = subprocess.run(
        ["psql", database_url, "-t", "-A", "-c", query],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode != 0:
        raise RuntimeError(f"psql failed: {result.stderr}")
    return result.stdout.strip()


def query_cursor(database_url: str = DATABASE_URL, chain_id: int = CHAIN_ID) -> tuple[int, str]:
    """Returns (block_number, block_hash) from indexer_cursors."""
    row = _psql(
        database_url,
        f"SELECT last_block, last_block_hash FROM indexer_cursors WHERE chain_id = {chain_id}",
    )
    if not row:
        return 0, ""
    parts = row.split("|")
    return int(parts[0]), parts[1]


def query_block_hashes(
    database_url: str = DATABASE_URL,
    chain_id: int = CHAIN_ID,
    from_block: int = 0,
    to_block: int = 999999,
) -> dict[int, str]:
    """Returns {block_number: block_hash} from indexed_blocks."""
    rows = _psql(
        database_url,
        f"SELECT block_number, block_hash FROM indexed_blocks "
        f"WHERE chain_id = {chain_id} AND block_number >= {from_block} AND block_number <= {to_block} "
        f"ORDER BY block_number",
    )
    result = {}
    for line in rows.splitlines():
        if not line.strip():
            continue
        parts = line.split("|")
        result[int(parts[0])] = parts[1]
    return result


def query_block_parents(
    database_url: str = DATABASE_URL,
    chain_id: int = CHAIN_ID,
    from_block: int = 0,
    to_block: int = 999999,
) -> list[tuple[int, str, str]]:
    """Returns [(block_number, block_hash, parent_hash)] from indexed_blocks."""
    rows = _psql(
        database_url,
        f"SELECT block_number, block_hash, parent_hash FROM indexed_blocks "
        f"WHERE chain_id = {chain_id} AND block_number >= {from_block} AND block_number <= {to_block} "
        f"ORDER BY block_number",
    )
    result = []
    for line in rows.splitlines():
        if not line.strip():
            continue
        parts = line.split("|")
        result.append((int(parts[0]), parts[1], parts[2]))
    return result


def query_max_block(database_url: str = DATABASE_URL, chain_id: int = CHAIN_ID) -> int:
    """Returns the highest block number in indexed_blocks."""
    row = _psql(
        database_url,
        f"SELECT COALESCE(MAX(block_number), 0) FROM indexed_blocks WHERE chain_id = {chain_id}",
    )
    return int(row) if row else 0


def truncate_all(database_url: str = DATABASE_URL) -> None:
    _psql(
        database_url,
        "TRUNCATE indexer_cursors, indexed_blocks, raw_events, event_ccaf_auction_created",
    )


# ---------------------------------------------------------------------------
# Wait helpers
# ---------------------------------------------------------------------------


def wait_for_cursor(
    target: int,
    database_url: str = DATABASE_URL,
    chain_id: int = CHAIN_ID,
    timeout: float = 30,
) -> tuple[int, str]:
    """Poll DB until cursor >= target. Returns final (block, hash)."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        block, hash_ = query_cursor(database_url, chain_id)
        if block >= target:
            return block, hash_
        time.sleep(0.5)
    raise TimeoutError(f"Cursor did not reach {target} within {timeout}s (currently at {query_cursor(database_url, chain_id)[0]})")


def wait_for_exit(proc: subprocess.Popen, timeout: float = 30) -> int:
    """Wait for process to exit. Returns exit code."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        code = proc.poll()
        if code is not None:
            return code
        time.sleep(0.5)
    raise TimeoutError(f"Process {proc.pid} did not exit within {timeout}s")


# ---------------------------------------------------------------------------
# Assertion helpers
# ---------------------------------------------------------------------------

_pass_count = 0
_fail_count = 0


def check(desc: str, condition: bool) -> None:
    global _pass_count, _fail_count
    if condition:
        print(f"  PASS: {desc}")
        _pass_count += 1
    else:
        print(f"  FAIL: {desc}")
        _fail_count += 1


def results() -> tuple[int, int]:
    return _pass_count, _fail_count


def reset_results() -> None:
    global _pass_count, _fail_count
    _pass_count = 0
    _fail_count = 0


def print_summary(scenario_name: str) -> int:
    p, f = results()
    print()
    if f > 0:
        print(f"  {scenario_name}: FAILED ({p} passed, {f} failed)")
    else:
        print(f"  {scenario_name}: PASSED ({p} passed)")
    return 1 if f > 0 else 0


# ---------------------------------------------------------------------------
# Internal HTTP helpers
# ---------------------------------------------------------------------------


def _http_get(url: str) -> str:
    req = urllib.request.Request(url)
    with urllib.request.urlopen(req, timeout=5) as resp:
        return resp.read().decode()


def _http_post(url: str, data: bytes = b"") -> str:
    req = urllib.request.Request(url, data=data, method="POST")
    with urllib.request.urlopen(req, timeout=5) as resp:
        return resp.read().decode()


def _wait_ready(check_fn, name: str, timeout: float = 10) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            check_fn()
            return
        except Exception:
            time.sleep(0.3)
    raise TimeoutError(f"{name} not ready within {timeout}s")
