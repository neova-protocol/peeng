package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const INACTIVE_QUERY = `SELECT peer_id FROM peers WHERE active = 0  and last_time_check < datetime('now', '-5 minutes') ORDER BY last_time_check ASC LIMIT 10`
const OLD_QUERY = `SELECT peer_id FROM peers WHERE last_time_check < datetime('now', '-30 minutes') ORDER BY last_time_check ASC LIMIT 5`

// Peer represents a peer entry in the database
type Peer struct {
	PeerID        string    `json:"peer_id"`
	LastTimeCheck time.Time `json:"last_time_check"`
	Active        bool      `json:"active"`
}

// HehojExisteRequest represents the request body for /hehojexiste
type HehojExisteRequest struct {
	PeerID     string `json:"peer_id"`
	AddressMap string `json:"address_map"`
}

var (
	db      *sql.DB
	dbMu    sync.Mutex // Mutex for database operations
	ipfsAPI string
)

func main() {
	var err error
	db, err = sql.Open("sqlite3", "./peers.db")
	if err != nil {
		logFatal("Failed to open database", err)
	}
	defer db.Close()

	ipfsAPI = os.Getenv("IPFS_API")
	if ipfsAPI == "" {
		ipfsAPI = "http://127.0.0.1:5001"
	}

	createTable()
	go workerLoop()

	http.HandleFunc("/peers", handlePeers)
	http.HandleFunc("/hehojexiste", handleHehojExiste)
	http.HandleFunc("/", handleHealth)
	log.Println("\x1b[1;32m[INFO]\x1b[0m API listening on :8080 …")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func createTable() {
	sqlStmt := `CREATE TABLE IF NOT EXISTS peers (
		peer_id TEXT PRIMARY KEY,
		last_time_check TIMESTAMP,
		active BOOLEAN
	);`
	if _, err := db.Exec(sqlStmt); err != nil {
		logFatal("Failed to create table", err)
	}
	log.Println("\x1b[1;34m[INFO]\x1b[0m Database table 'peers' ensured.")
}

func workerLoop() {
	for {
		log.Println("\x1b[3;33m[WORKER]\x1b[0m Skipping public DHT …")
		log.Println("\x1b[3;33m[WORKER]\x1b[0m Inactive peers loop started")
		pingPeersLoop(INACTIVE_QUERY, 1*time.Second)
		log.Println("\x1b[3;33m[WORKER]\x1b[0m Inactive peers loop finished")
		pingPeersLoop(OLD_QUERY, 1*time.Second)
	}
}

// pingPeersLoop pings a list of peers from a query and updates their status
func pingPeersLoop(query string, sleep time.Duration) {
	for _, pid := range getPeerIDs(query) {
		ok := pingPeer(pid)
		log.Printf("\x1b[3;33m[WORKER]\x1b[0m Pinged %s, result: %t", pid, ok)
		upsertPeer(pid, time.Now(), ok)
		time.Sleep(sleep)
	}
}

// getPeerIDs returns a slice of peer IDs from a query
func getPeerIDs(query string) []string {
	dbMu.Lock()
	rows, err := db.Query(query)
	dbMu.Unlock()
	if err != nil {
		logError("Failed to query peers for ping", err)
		return nil
	}
	defer rows.Close()
	var pids []string
	for rows.Next() {
		log.Printf("Debug: Scanning peer ID")
		var pid string
		if err := rows.Scan(&pid); err != nil {
			logError("Failed to scan peer ID", err)
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// Removed unused fetchSwarmPeers for clarity

// pingPeer pings a peer by ID
func pingPeer(peerID string) bool {
	return pingPeerWithAddress(peerID, "")
}

// pingPeerWithAddress pings a peer with an optional address
func pingPeerWithAddress(peerID, addressMap string) bool {
	const count = 4
	const timeout = 30 * time.Second
	log.Printf("\x1b[1;35m[PING]\x1b[0m Attempting to ping: %s (Address: %s)", peerID, addressMap)

	arg := peerID
	if addressMap != "" {
		arg = fmt.Sprintf("%s/p2p/%s", addressMap, peerID)
	}
	url := fmt.Sprintf("%s/api/v0/ping?arg=%s&count=%d", ipfsAPI, arg, count)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		logError("Failed to create HTTP request", err)
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logError("HTTP request failed", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logError(fmt.Sprintf("Ping returned HTTP status %d", resp.StatusCode), nil)
		return false
	}

	pongCount, totalLatency := parsePingResponse(resp.Body, peerID)
	if pongCount > 0 {
		avgLatencyMs := float64(totalLatency) / float64(pongCount) / 1e6
		log.Printf("\x1b[1;32m[PING]\x1b[0m %s average latency: %.2f ms (%d/%d pongs)", peerID, avgLatencyMs, pongCount, count)
		return true
	}
	logError(fmt.Sprintf("No pongs received from %s", peerID), nil)
	return false
}

// parsePingResponse parses the ping response and returns pong count and total latency
func parsePingResponse(body io.Reader, peerID string) (pongCount int, totalLatency int64) {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Bytes()
		var msg struct {
			Success bool   `json:"Success"`
			Text    string `json:"Text"`
			Time    int64  `json:"Time"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Printf("\x1b[3;33m[WARN]\x1b[0m Failed to parse line: %s", line)
			continue
		}
		log.Printf("\x1b[3;90m[DEBUG]\x1b[0m %+v", msg)
		if msg.Time > 0 && msg.Success {
			latencyMs := float64(msg.Time) / 1e6
			log.Printf("\x1b[1;32m[PING]\x1b[0m %s pong in %.2f ms", peerID, latencyMs)
			pongCount++
			totalLatency += msg.Time
			break
		} else if !msg.Success && strings.Contains(strings.ToLower(msg.Text), "failed") {
			logError("Ping failed: "+msg.Text, nil)
		}
	}
	if err := scanner.Err(); err != nil {
		logError("Error reading response", err)
	}
	return
}

// upsertPeer inserts or updates a peer in the database
func upsertPeer(peerID string, checkTime time.Time, active bool) {
	dbMu.Lock()
	defer dbMu.Unlock()
	_, err := db.Exec(`
		INSERT INTO peers (peer_id, last_time_check, active)
		VALUES (?, ?, ?)
		ON CONFLICT(peer_id) DO UPDATE SET
			last_time_check = excluded.last_time_check,
			active = excluded.active
	`, peerID, checkTime, active)
	if err != nil {
		logError(fmt.Sprintf("DB upsert error for %s", peerID), err)
	} else {
		log.Printf("\x1b[1;36m[DB]\x1b[0m Upserted peer %s (Active: %t).", peerID, active)
	}
}

// handlePeers serves the /peers endpoint
func handlePeers(w http.ResponseWriter, r *http.Request) {
	dbMu.Lock()
	rows, err := db.Query("SELECT peer_id, last_time_check, active FROM peers ORDER BY last_time_check DESC")
	dbMu.Unlock()
	if err != nil {
		http.Error(w, fmt.Sprintf("\x1b[1;31m[ERROR]\x1b[0m Failed to query peers from DB: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var peers []Peer
	for rows.Next() {
		var p Peer
		var ts string
		if err := rows.Scan(&p.PeerID, &ts, &p.Active); err != nil {
			logError("Failed to scan peer row", err)
			continue
		}
		p.LastTimeCheck, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			logError(fmt.Sprintf("Failed to parse timestamp '%s' for peer %s", ts, p.PeerID), err)
			p.LastTimeCheck = time.Time{}
		}
		peers = append(peers, p)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(peers); err != nil {
		logError("Failed to encode peers JSON", err)
	}
	log.Println("\x1b[1;32m[API]\x1b[0m /peers endpoint served.")
}

// handleHehojExiste serves the /hehojexiste endpoint
func handleHehojExiste(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "\x1b[1;31m[ERROR]\x1b[0m Only POST method is allowed for /hehojexiste", http.StatusMethodNotAllowed)
		return
	}
	var req HehojExisteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("\x1b[1;31m[ERROR]\x1b[0m Failed to decode request body: %v", err), http.StatusBadRequest)
		return
	}
	if req.PeerID == "" {
		http.Error(w, "\x1b[1;31m[ERROR]\x1b[0m 'peer_id' is required", http.StatusBadRequest)
		return
	}
	log.Printf("\x1b[1;3;35m[API]\x1b[0m Received /hehojexiste request for PeerID: %s, AddressMap: %s", req.PeerID, req.AddressMap)
	pingOK := pingPeerWithAddress(req.PeerID, req.AddressMap)
	upsertPeer(req.PeerID, time.Now(), pingOK)
	response := map[string]bool{"ping_successful": pingOK}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError("Failed to encode /hehojexiste response", err)
	}
	log.Printf("\x1b[1;32m[API]\x1b[0m /hehojexiste endpoint served for PeerID %s.", req.PeerID)
}

// handleHealth serves the root health endpoint
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK - version 1.2.0"))
	log.Println("\x1b[1;32m[API]\x1b[0m / endpoint served.")
}

// logError logs an error with a message
func logError(msg string, err error) {
	// Bold red for errors
	if err != nil {
		log.Printf("\x1b[1;31m[ERROR]\x1b[0m %s: %v", msg, err)
	} else {
		log.Printf("\x1b[1;31m[ERROR]\x1b[0m %s", msg)
	}
}

func logFatal(msg string, err error) {
	// Bold red for fatal errors
	if err != nil {
		log.Fatalf("\x1b[1;31m[ERROR]\x1b[0m %s: %v", msg, err)
	} else {
		log.Fatalf("\x1b[1;31m[ERROR]\x1b[0m %s", msg)
	}
}
