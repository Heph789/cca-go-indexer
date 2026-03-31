"""Shared helpers for clearing price QA simulations.

Extends the production helpers with GraphQL query support and
checkpoint-specific database queries.
"""

import json
import os
import subprocess
import sys
import time
import urllib.request
import urllib.error

# Re-export from production helpers (which re-exports from resilience helpers).
import importlib.util as _ilu

_production_path = os.path.join(os.path.dirname(__file__), "..", "production", "helpers.py")
_spec = _ilu.spec_from_file_location("production_helpers", _production_path)
_ph = _ilu.module_from_spec(_spec)
_spec.loader.exec_module(_ph)

# Also load resilience helpers directly for chain manipulation functions
# that production helpers do not re-export.
_resilience_path = os.path.join(os.path.dirname(__file__), "..", "resilience", "helpers.py")
_rspec = _ilu.spec_from_file_location("resilience_helpers", _resilience_path)
_rh = _ilu.module_from_spec(_rspec)
_rspec.loader.exec_module(_rh)

REPO_ROOT = _ph.REPO_ROOT
DATABASE_URL = _ph.DATABASE_URL
ANVIL_PORT = _ph.ANVIL_PORT
API_PORT = _ph.API_PORT
CHAIN_ID = _ph.CHAIN_ID
cleanup = _ph.cleanup
register = _ph.register
start_anvil = _ph.start_anvil
deploy_contracts = _ph.deploy_contracts
truncate_all_base = _ph.truncate_all
start_indexer = _ph.start_indexer
start_api = _ph.start_api
wait_for_cursor = _ph.wait_for_cursor
http_request = _ph.http_request
check = _ph.check
reset_results = _ph.reset_results
print_summary = _ph.print_summary
mine_blocks = _rh.mine_blocks
get_block_number = _rh.get_block_number

CHAIN_DIR = os.path.join(os.path.dirname(__file__), "..", "chain")
PRIVATE_KEY = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"


# ---------------------------------------------------------------------------
# Database helpers
# ---------------------------------------------------------------------------


def _psql(query: str, database_url: str = DATABASE_URL) -> str:
    result = subprocess.run(
        ["psql", database_url, "-t", "-A", "-c", query],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if result.returncode != 0:
        raise RuntimeError(f"psql failed: {result.stderr}")
    return result.stdout.strip()


def truncate_all(database_url: str = DATABASE_URL) -> None:
    """Truncate all tables including checkpoint and bid tables.

    Also resets schema_migrations to the latest version to prevent
    stale migration state from blocking the indexer startup.
    Handles missing tables gracefully (e.g., on older branches).
    """
    # Core tables that always exist.
    _psql(
        "TRUNCATE indexer_cursors, indexed_blocks, raw_events, "
        "event_ccaf_auction_created, watched_contracts",
        database_url,
    )
    # Checkpoint and bid tables may not exist on older branches.
    for table in ("event_cca_checkpoint_updated", "event_cca_bid_submitted"):
        try:
            _psql(f"TRUNCATE {table}", database_url)
        except RuntimeError:
            pass  # Table doesn't exist on this branch.

    # Determine the latest migration version by counting migration files.
    # This adapts to whichever branch we're running on.
    import glob as globmod
    migration_dir = os.path.join(REPO_ROOT, "internal", "store", "migrations")
    up_files = globmod.glob(os.path.join(migration_dir, "*.up.sql"))
    version = len(up_files)
    _psql(
        f"DELETE FROM schema_migrations; "
        f"INSERT INTO schema_migrations (version, dirty) VALUES ({version}, false)",
        database_url,
    )


def query_checkpoints(auction_address: str, database_url: str = DATABASE_URL) -> list[dict]:
    """Query all checkpoint rows for a given auction address.

    Returns an empty list if the checkpoint table does not exist
    (e.g., on older branches without migration 3).
    """
    try:
        rows = _psql(
            f"SELECT chain_id, auction_address, block_number, clearing_price_q96, "
            f"cumulative_mps, tx_block_number, tx_hash, log_index "
            f"FROM event_cca_checkpoint_updated "
            f"WHERE auction_address = lower('{auction_address}') "
            f"ORDER BY block_number",
            database_url,
        )
    except RuntimeError:
        return []
    result = []
    for line in rows.splitlines():
        if not line.strip():
            continue
        parts = line.split("|")
        result.append({
            "chain_id": int(parts[0]),
            "auction_address": parts[1],
            "block_number": int(parts[2]),
            "clearing_price_q96": parts[3],
            "cumulative_mps": int(parts[4]),
            "tx_block_number": int(parts[5]),
            "tx_hash": parts[6],
            "log_index": int(parts[7]),
        })
    return result


def count_checkpoints(auction_address: str, database_url: str = DATABASE_URL) -> int:
    """Count checkpoint rows for a given auction address.

    Returns 0 if the checkpoint table does not exist.
    """
    try:
        row = _psql(
            f"SELECT COUNT(*) FROM event_cca_checkpoint_updated "
            f"WHERE auction_address = lower('{auction_address}')",
            database_url,
        )
        return int(row) if row else 0
    except RuntimeError:
        return 0


# ---------------------------------------------------------------------------
# GraphQL helpers
# ---------------------------------------------------------------------------


def graphql_query(query: str, variables: dict | None = None, port: int = API_PORT) -> dict:
    """Send a GraphQL query to the API and return the parsed response.

    Returns a dict with 'data' and optionally 'errors'. Does NOT raise
    on GraphQL-level errors -- callers should inspect the response.
    Returns {"errors": [{"message": <msg>}]} on HTTP-level failures
    so callers can treat all failures uniformly.
    """
    url = f"http://127.0.0.1:{port}/graphql"
    payload = json.dumps({"query": query, "variables": variables or {}}).encode()
    req = urllib.request.Request(
        url,
        data=payload,
        method="POST",
        headers={"Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        body = e.read()
        try:
            return json.loads(body)
        except Exception:
            return {"errors": [{"message": f"HTTP {e.code}: {body.decode()}"}]}


# ---------------------------------------------------------------------------
# Bid submission (triggers CheckpointUpdated)
# ---------------------------------------------------------------------------


def submit_bid(rpc_url: str, auction_address: str) -> str:
    """Submit a bid on the auction via the SubmitBid Forge script.

    This triggers a CheckpointUpdated event on chain.
    Returns the stdout from the forge script.
    """
    print(f"    Submitting bid on auction {auction_address}...")
    env = {
        **os.environ,
        "RPC_URL": rpc_url,
        "AUCTION_ADDRESS": auction_address,
    }
    result = subprocess.run(
        [
            "forge", "script",
            "script/SubmitBid.s.sol:SubmitBid",
            "--rpc-url", rpc_url,
            "--private-key", PRIVATE_KEY,
            "--broadcast",
        ],
        capture_output=True,
        text=True,
        env=env,
        cwd=CHAIN_DIR,
    )
    output = result.stdout + "\n" + result.stderr
    if result.returncode != 0:
        print(f"    Bid submission output:\n{output}")
        raise RuntimeError(f"SubmitBid.s.sol failed (exit {result.returncode})")
    print(f"    Bid submitted successfully")
    return output


def mine_to_start_block(rpc_url: str, start_block: int) -> None:
    """Mine blocks on Anvil until block.number >= start_block."""
    current = get_block_number(rpc_url)
    needed = start_block - current
    if needed > 0:
        print(f"    Mining {needed} blocks to reach startBlock {start_block}...")
        mine_blocks(rpc_url, needed)
        print(f"    Now at block {get_block_number(rpc_url)}")
