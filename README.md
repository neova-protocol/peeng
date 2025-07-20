# IPFS Peer Monitor

This project is a simple Go-based monitoring tool for tracking peer connectivity in a private IPFS cluster. It records which peers are connected, how long they stay connected, and provides an API for querying and updating peer status. The goal is to enable fair reward distribution to the most connected users in your IPFS network.

## Features

- **Automatic Peer Discovery:** Periodically fetches the list of connected peers from the local IPFS node.
- **Peer Status Tracking:** Stores peer connection status and last check time in a SQLite database.
- **Active Monitoring:** Pings all known peers to verify their connectivity and updates their status.
- **REST API:**
  - `GET /peers` — Returns the list of all known peers with their last check time and active status.
  - `POST /hehojexiste` — Allows external checks/pings for a specific peer and records the result.
- **Concurrency Safe:** Uses mutexes to avoid database locking issues.
- **Colorful Logging:** Console output is color-coded for easy monitoring.

## Use Case

This tool is designed for private IPFS clusters where you want to:
- Monitor which users (peers) are most reliably connected.
- Track connection durations and activity.
- Use the data to distribute rewards or incentives to the most active/connected users.

## Requirements

- Go 1.18+
- IPFS node running locally (API at `127.0.0.1:5001`)
- SQLite3

## Setup

1. **Clone the repository:**
   ```fish
   git clone <your-repo-url>
   cd ping
   ```
2. **Install dependencies:**
   ```fish
   go mod tidy
   ```
3. **Run the monitor:**
   ```fish
   go run main.go
   ```
   The API will listen on `:8080` by default.

## API Endpoints

### `GET /peers`
Returns a JSON array of all known peers:
```json
[
  {
    "peer_id": "Qm...",
    "last_time_check": "2025-07-21T12:34:56.789Z",
    "active": true
  },
  ...
]
```

### `POST /hehojexiste`
Checks and records the connectivity of a specific peer.
- **Request Body:**
  ```json
  {
    "peer_id": "Qm...",
    "address_map": "/ip4/1.2.3.4/tcp/4001" // optional
  }
  ```
- **Response:**
  ```json
  { "ping_successful": true }
  ```

## How It Works

- The monitor fetches the current swarm peers from the local IPFS node every few seconds.
- It pings each peer to check if they are reachable.
- Peer status and last check time are stored in a SQLite database (`peers.db`).
- The `/peers` endpoint provides a snapshot of all known peers and their status.
- The `/hehojexiste` endpoint allows external systems to trigger a ping and update for a specific peer.

## Reward Distribution

By analyzing the data in the `peers` table, you can determine which users are most reliably connected and distribute rewards accordingly.

## License

MIT License
