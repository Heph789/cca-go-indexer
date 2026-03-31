# CCA Go Indexer

An indexer and API for Uniswap's Continuous Clearing Auction (CCA) token sale contracts, built in Go. README written by AI. You can see my iteration on the AI workflow on [process-design branch](https://github.com/Heph789/cca-go-indexer/tree/process-design)

## Why This Project

CCA token sales are a real, deployed protocol with meaningful on-chain activity. This project indexes that data and serves it via an API. But the indexer itself is also the point — it's a vehicle for two learning goals:

### 1. Production-grade backend engineering

Build a system that reflects real-world backend standards: clean architecture, proper observability, resilient RPC handling, reorg-safe indexing, database migrations, integration tests, graceful shutdown, and deployment-ready packaging. The kind of code you'd ship on a team, not a weekend hack.

### 2. Measurable improvements to LLM-assisted development

Use this project as a controlled environment to develop and evaluate LLM workflows for real team software. The bar is higher than "LLM wrote code that works" — the output has to be reviewable, testable, and maintainable by humans. That means:

- Code that passes PR review (clear intent, consistent style, no unnecessary complexity)
- Tests that actually validate behavior (not just coverage theater)
- Commits that tell a coherent story
- Architecture that a new contributor can navigate

The goal is to find objective, repeatable process improvements — not just vibes about whether the LLM helped.

## What It Does

- Tracks the CCA factory contract across multiple chains to discover new token sale auctions
- Indexes all auction events: bids, clearing price updates, checkpoints, exits, claims
- Stores indexed data in PostgreSQL with full reorg safety
- Serves the data via a REST API with filtering, pagination, and per-chain context
