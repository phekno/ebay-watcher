# ebay-watcher

Watches eBay for listings matching your search queries under a price threshold, sends Discord alerts, and serves a dark-mode dashboard.

## Features

- eBay Browse API polling on a configurable interval
- Price-threshold filtering — only alerts when a listing is under `MAX_PRICE`
- Discord webhook notifications with rich embeds
- Dark-mode web UI at port `8080` with:
  - Stats cards (total tracked, notified, threshold, poll interval)
  - Price history chart (30-day trend per query)
  - Notified listings table with condition badges
- SQLite persistence (tracks all seen listings + full price history)
- Single static binary — no external dependencies at runtime

## Configuration

All config via environment variables:

| Variable             | Required | Default         | Description                              |
|----------------------|----------|-----------------|------------------------------------------|
| `EBAY_CLIENT_ID`     | ✅       | —               | eBay developer app Client ID             |
| `EBAY_CLIENT_SECRET` | ✅       | —               | eBay developer app Client Secret         |
| `DISCORD_WEBHOOK_URL`| ✅       | —               | Discord channel webhook URL              |
| `POLL_INTERVAL`      | ❌       | `1h`            | How often to poll (Go duration string)   |
| `DATABASE_PATH`      | ❌       | `/data/seen.db` | SQLite database path                     |
| `LISTEN_ADDR`        | ❌       | `:8080`         | Address for the web UI                   |

Search queries and price thresholds are managed through the web UI as **watches** — each watch has its own query and max price.

## eBay API Setup

1. Create a developer account at https://developer.ebay.com
2. Create an application → get your **Client ID** and **Client Secret**
3. The app uses the Browse API with `client_credentials` OAuth — no user login required

## Local Development

```bash
export EBAY_CLIENT_ID=your-id
export EBAY_CLIENT_SECRET=your-secret
export DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
export POLL_INTERVAL=5m   # short for testing

go run .
# UI: http://localhost:8080
# Add watches (search queries + price thresholds) via the web UI
```

## Kubernetes (Flux)

Copy the manifests from `home-ops-manifests/` into your home-ops repo:

```
kubernetes/apps/utilities/ebay-watcher/
├── app/
│   ├── externalsecret.yaml   # pulls creds from 1Password
│   ├── helmrelease.yaml      # bjw-s app-template
│   ├── httproute.yaml        # envoy-internal gateway
│   ├── kustomization.yaml
│   └── namespace.yaml
└── ks.yaml                   # Flux Kustomization
```

Add the 1Password item `ebay-watcher` with fields: `client_id`, `client_secret`, `discord_webhook_url`.

Register the Kustomization in your cluster's apps Kustomization by adding a reference to `ks.yaml`.
