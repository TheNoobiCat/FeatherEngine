# Security Policy

## Project status

FeatherEngine is unaudited, testnet-only reference software. It has not been
reviewed for production use. Do not deploy it to mainnet or use it with funds
you cannot afford to lose.

## Reporting a vulnerability

Please do not open a public GitHub issue for security problems.

Use GitHub's private vulnerability reporting for this repository:
**Security** tab → **Report a vulnerability**. Include:

- a description of the issue and its impact,
- steps or a proof of concept to reproduce it,
- affected files, functions, or commits if known.

Reports are handled on a best-effort basis. There is no bug-bounty program.

## Scope

Security-relevant areas include:

- private-key handling and transaction signing,
- matching and `bulkMatch` transaction construction,
- liquidation and settlement transaction construction,
- event replay and in-memory orderbook consistency,
- RPC input handling and reconnection logic.

## Operational security

- Run only with a dedicated hot wallet holding a small gas balance.
- Never commit `.env`, private keys, or RPC credentials. `.env` is gitignored.
- Restrict access to the host and to the process environment.
- Rotate the hot-wallet key immediately if it may have been exposed.
- Use fresh Pyth price update data for liquidations in any non-test
  deployment; see the README.
