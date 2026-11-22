# FeatherEngine

FeatherEngine is the off-chain matching and liquidation engine for BrokerFi's
on-chain options protocol. It follows the `Orderbook` contract, keeps a
price-time-priority orderbook in memory, and submits batched `bulkMatch`
transactions when bids and asks cross. A separate liquidator loop watches
tracked traders for liquidatable or expired positions.

> **Status: testnet-only, unaudited reference software.**
> This code has not been audited and is not production-ready. Run it against a
> testnet with a dedicated hot wallet only. Use at your own risk.

## Architecture

```
Orderbook events ──> ingestion ──> in-memory book ──> matcher ──> bulkMatch tx
                                         ^
MarginAccount events ──> event pusher ───┘

MarginAccount positions ──> liquidator ──> liquidatePosition / settlePosition
```

- **Ingestion** (`internal/ingestion`) subscribes to `LimitOrderCreated` and
  `LimitOrderCancelled` over WebSocket, fetches the order and series data from
  chain, and updates the book. The subscription reconnects with exponential
  backoff.
- **In-memory orderbook** (`internal/orderbook`) keeps one book per series.
  Each side uses a B-tree of price levels plus FIFO lists, with O(log n)
  insertion and O(1) cancellation by order ID.
- **Matcher** (`internal/matcher`) is triggered by book updates, with a
  periodic fallback ticker. It finds crossing orders, preflights `bulkMatch`
  with `eth_call`, and submits EIP-1559 transactions in batches of
  `MAX_MATCHES_PER_TX`.
- **Event pusher** (`internal/pusher`) subscribes to `PositionSettled` and
  `PositionLiquidated`. It currently logs these events; on-chain state remains
  the source of truth.
- **Liquidator** (`internal/matcher/liquidator.go`) scans tracked traders,
  settles expired positions, calls `isLiquidatable`, and submits liquidation or
  settlement transactions.
- **Event replay** replays historical order and settlement events on startup so
  the in-memory book is not empty after a restart.

State is held in memory and rebuilt from the replay window on every start.

## Requirements

- Go 1.24 or newer.
- An Arbitrum (or compatible) WebSocket RPC endpoint that supports
  `eth_getLogs`.
- A funded hot wallet for gas. `CHAIN_ID` defaults to `421614`
  (Arbitrum Sepolia).

## Configuration

Copy `.env.example` to `.env` and fill in the values. `godotenv` loads `.env`
on a best-effort basis; variables already set in the environment also work.
Never commit `.env`.

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PRIVATE_KEY` | yes | — | 32-byte hex private key for the hot wallet that signs match/liquidation transactions (no `0x` prefix) |
| `ALCHEMY_WS_URL` | yes | — | WebSocket RPC URL |
| `OPTION_MARKET_ADDRESS` | yes | — | `OptionMarket` contract address |
| `ORDERBOOK_ADDRESS` | yes | — | `Orderbook` contract address |
| `MARGIN_ACCOUNT_ADDRESS` | yes | — | `MarginAccount` contract address |
| `CHAIN_ID` | no | `421614` | Expected chain ID; startup fails on a mismatch |
| `MATCH_INTERVAL_MS` | no | `500` | Fallback matching interval in milliseconds |
| `LIQUIDATION_INTERVAL_MS` | no | `5000` | Liquidation scan interval in milliseconds |
| `MAX_MATCHES_PER_TX` | no | `50` | Maximum matches per `bulkMatch` transaction |
| `GAS_LIMIT` | no | `3000000` | Gas limit for submitted transactions |
| `MAX_GAS_PRICE_GWEI` | no | `5` | Maximum gas price in gwei |
| `START_BLOCK` | no | `0` | First block to replay. `0`/unset derives it from `REPLAY_LOOKBACK_BLOCKS` |
| `REPLAY_LOOKBACK_BLOCKS` | no | `345600` | Blocks to replay when `START_BLOCK` is `0`; roughly 24h on Arbitrum |

### Startup replay and the 24h window

When `START_BLOCK` is unset, FeatherEngine only replays the last
`REPLAY_LOOKBACK_BLOCKS` blocks (`345600`, roughly 24 hours at 250ms block
time). Orders and positions opened before that window are not loaded. The
liquidator only tracks traders it has seen, so **it can miss older positions
that are still open**.

For full coverage, set `START_BLOCK` to the `Orderbook` deployment block (or an
earlier block). Very large log ranges may exceed your RPC provider's
`eth_getLogs` limits; chunked replay is not implemented.

### Liquidation price limitation

The liquidator currently sends an empty `priceUpdateData` array to
`liquidatePosition` and `settlePosition`. The contracts therefore fall back to
cached Pyth prices (`Oracle.getPriceNoUpdate`). Cached prices can be stale,
which may cause liquidations to revert or execute at an incorrect price during
volatile markets.

**Production deployments must fetch fresh Pyth update data (for example from
Hermes) and pass it to these calls.** FeatherEngine does not include a Pyth
updater.

## Build and run

```bash
cp .env.example .env
# edit .env with your RPC URL, contract addresses, and hot-wallet key

go build -o featherengine ./cmd/engine
./featherengine
```

Or run directly:

```bash
go run ./cmd/engine
```

## Tests and checks

```bash
go test ./...
go vet ./...
gofmt -l .
```

`go test ./... -short` skips the long-running orderbook stress tests. The
20-million-order stress test is opt-in even without `-short`; run it with
`FEATHER_STRESS_TEST=1 go test ./internal/orderbook -run TestUltimate20MillionOrderStressTest -v`
(it needs several minutes and roughly 4-8 GB of RAM).

## Security: hot-wallet private key

FeatherEngine loads a private key from `PRIVATE_KEY` and signs transactions
with it. Treat that key as a hot wallet:

- Use a dedicated wallet with only the gas balance it needs. Never use a
  treasury, deployer, or mainnet key.
- Keep `.env` out of version control (it is gitignored) and restrict access to
  the host and process environment.
- Prefer a secret manager or an external signer in any real deployment.
- Rotate the key immediately if it may have been exposed.

See [SECURITY.md](SECURITY.md) for vulnerability reporting.

## Limitations

- Unaudited and intended for testnets only.
- Single process with in-memory state; there is no persistence or HA.
- No Pyth price-update fetcher; liquidation and settlement depend on cached
  prices.
- Historical replay is limited by `START_BLOCK` / `REPLAY_LOOKBACK_BLOCKS` and
  RPC log-range limits.
- The event pusher is minimal and relies on on-chain state as the source of
  truth.

## License

MIT. See [LICENSE](LICENSE).
