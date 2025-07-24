package main

import (
	   "database/sql"
	   "encoding/json"
	   "bufio"
	   "fmt"
	   "strings"
	   "context"
	   "log"
	   "net/http"
	   "os"
	   "sync"
	   "time"

	   _ "github.com/mattn/go-sqlite3"
)

// Peer représente une entrée dans la DB
type Peer struct {
	PeerID        string    `json:"peer_id"`
	LastTimeCheck time.Time `json:"last_time_check"`
	Active        bool      `json:"active"`
}

// SwarmPeersResponse : réponse JSON de swarm/peers
type SwarmPeersResponse struct {
	Peers []struct {
		Peer string `json:"Peer"`
	} `json:"Peers"`
}

// HehojExisteRequest représente le corps de la requête pour /hehojexiste
type HehojExisteRequest struct {
	PeerID    string `json:"peer_id"`
	AddressMap string `json:"address_map"`
}


var db *sql.DB
var dbMu sync.Mutex // Mutex for database operations to prevent "database is locked" errors

var ipfsAPI string

func main() {
	   var err error
	   db, err = sql.Open("sqlite3", "./peers.db")
	   if err != nil {
			   log.Fatalf("\x1b[31m[ERROR]\x1b[0m Failed to open database: %v\n", err) // Red color for error
	   }
	   defer db.Close()

	   ipfsAPI = os.Getenv("IPFS_API")
	   if ipfsAPI == "" {
			   ipfsAPI = "http://127.0.0.1:5001" // Default fallback
	   }

	   createTable()

	   go workerLoop()

	   http.HandleFunc("/peers", handlePeers)
	   http.HandleFunc("/hehojexiste", handleHehojExiste)
	   http.HandleFunc("/", handleHealth)
	   log.Println("\x1b[32m[INFO]\x1b[0m API listening on :8080 …") // Green color for info
	   log.Fatal(http.ListenAndServe(":8080", nil))
}

func createTable() {
	sqlStmt := `
	CREATE TABLE IF NOT EXISTS peers (
		peer_id TEXT PRIMARY KEY,
		last_time_check TIMESTAMP,
		active BOOLEAN
	);
	`
	_, err := db.Exec(sqlStmt)
	if err != nil {
		log.Fatalf("\x1b[31m[ERROR]\x1b[0m %q: %s\n", err, sqlStmt) // Red color for error
	}
	log.Println("\x1b[34m[INFO]\x1b[0m Database table 'peers' ensured.") // Blue color for info
}

func workerLoop() {
	for {
		log.Println("\x1b[33m[WORKER]\x1b[0m Skipping public DHT …") // Yellow color for worker
		// log.Println("\x1b[33m[WORKER]\x1b[0m Checking swarm peers …") // Yellow color for worker
		// peers := fetchSwarmPeers()
		// now := time.Now()

		// // Upsert newly fetched peers
		// for _, peerID := range peers {
		// 	upsertPeer(peerID, now, true)
		// }

		// Ping all known peers and update their status
		dbMu.Lock() // Acquire lock before querying
		rows, err := db.Query(`
			SELECT peer_id FROM peers
			ORDER BY last_time_check ASC
			LIMIT 5
		`)
		if err != nil {
			log.Printf("\x1b[31m[ERROR]\x1b[0m Failed to query peers for ping: %v\n", err) // Red color for error
			dbMu.Unlock() // Release lock on error
			time.Sleep(30 * time.Second) // Still sleep to prevent tight loop
			continue
		}

		var pidsToPing []string
		for rows.Next() {
			var pid string
			if err := rows.Scan(&pid); err != nil {
				log.Printf("\x1b[31m[ERROR]\x1b[0m Failed to scan peer ID during worker loop: %v\n", err) // Red color for error
				continue
			}
			pidsToPing = append(pidsToPing, pid)
		}
		rows.Close() // Close the rows BEFORE releasing the lock, but after scanning
		dbMu.Unlock() // Release lock after reading all peer IDs

		// Ping peers outside the critical section to avoid holding the lock during HTTP calls
		for _, pid := range pidsToPing {
			ok := pingPeer(pid) // This calls pingPeerWithAddress
			log.Printf("\x1b[33m[WORKER]\x1b[0m Pinged %s, result: %t\n", pid, ok) // Yellow color for worker
			upsertPeer(pid, time.Now(), ok) // This will acquire its own lock
		}
	}
}

func fetchSwarmPeers() []string {
	   log.Println("\x1b[34m[INFO]\x1b[0m Fetching swarm peers from IPFS API.") // Blue color for info
	   url := ipfsAPI + "/api/v0/swarm/peers"
	   resp, err := http.Post(url, "application/x-www-form-urlencoded", nil)

	if err != nil {
		log.Printf("\x1b[31m[ERROR]\x1b[0m Failed to get swarm peers: %v\n", err) // Red color for error
		return nil
	}
	defer resp.Body.Close()

	var result SwarmPeersResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		log.Printf("\x1b[31m[ERROR]\x1b[0m Failed to decode swarm response: %v\n", err) // Red color for error
		return nil
	}

	var peers []string
	for _, p := range result.Peers {
		peers = append(peers, p.Peer)
	}
	log.Printf("\x1b[34m[INFO]\x1b[0m Found %d swarm peers.\n", len(peers)) // Blue color for info
	return peers
}

func pingPeer(peerID string) bool {
	return pingPeerWithAddress(peerID, "") // Use the specific pingPeerWithAddress for regular pings
}


func pingPeerWithAddress(peerID, addressMap string) bool {
	const count = 4
	const timeout = 30 * time.Second

	log.Printf("\x1b[35m[PING]\x1b[0m Attempting to ping: %s (Address: %s)", peerID, addressMap)

	// Construire l’URL
	var arg string
	if addressMap != "" {
		arg = fmt.Sprintf("%s/p2p/%s", addressMap, peerID)
	} else {
		arg = peerID
	}
	url := fmt.Sprintf("%s/api/v0/ping?arg=%s&count=%d", ipfsAPI, arg, count)

	// Créer un client HTTP avec timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		log.Printf("\x1b[31m[ERROR]\x1b[0m Failed to create HTTP request: %v", err)
		return false
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("\x1b[31m[ERROR]\x1b[0m HTTP request failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("\x1b[31m[ERROR]\x1b[0m Ping returned HTTP status %d", resp.StatusCode)
		return false
	}

	// Scanner les lignes JSON
	scanner := bufio.NewScanner(resp.Body)
	var pongCount int
	var totalLatency int64

	for scanner.Scan() {
		line := scanner.Bytes()
		var msg struct {
			Success bool   `json:"Success"`
			Text    string `json:"Text"`
			Time    int64  `json:"Time"` // ns
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Printf("\x1b[33m[WARN]\x1b[0m Failed to parse line: %s", line)
			continue
		}

		// Debug : log tout
		log.Printf("\x1b[90m[DEBUG]\x1b[0m %+v", msg)

		if msg.Time > 0 && msg.Success {
			latencyMs := float64(msg.Time) / 1e6
			log.Printf("\x1b[32m[PING]\x1b[0m %s pong in %.2f ms", peerID, latencyMs)
			pongCount++
			totalLatency += msg.Time
			break
		} else if !msg.Success && strings.Contains(strings.ToLower(msg.Text), "failed") {
			log.Printf("\x1b[31m[ERROR]\x1b[0m Ping failed: %s", msg.Text)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("\x1b[31m[ERROR]\x1b[0m Error reading response: %v", err)
		return false
	}

	if pongCount > 0 {
		avgLatencyMs := float64(totalLatency) / float64(pongCount) / 1e6
		log.Printf("\x1b[32m[PING]\x1b[0m %s average latency: %.2f ms (%d/%d pongs)", peerID, avgLatencyMs, pongCount, count)
		return true
	}

	log.Printf("\x1b[31m[ERROR]\x1b[0m No pongs received from %s", peerID)
	return false
}

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
		log.Printf("\x1b[31m[ERROR]\x1b[0m DB upsert error for %s: %v\n", peerID, err) // Red color for error
	} else {
		log.Printf("\x1b[36m[DB]\x1b[0m Upserted peer %s (Active: %t).\n", peerID, active) // Cyan color for DB operations
	}
}

func handlePeers(w http.ResponseWriter, r *http.Request) {
	dbMu.Lock()
	rows, err := db.Query("SELECT peer_id, last_time_check, active FROM peers ORDER BY last_time_check DESC")
	dbMu.Unlock()
	if err != nil {
		http.Error(w, fmt.Sprintf("\x1b[31m[ERROR]\x1b[0m Failed to query peers from DB: %v\n", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var peers []Peer
	for rows.Next() {
		var p Peer
		var ts string // To read the timestamp as string first
		if err := rows.Scan(&p.PeerID, &ts, &p.Active); err != nil {
			log.Printf("\x1b[31m[ERROR]\x1b[0m Failed to scan peer row: %v\n", err) // Red color for error
			continue
		}
		// Parse the timestamp string to time.Time
		// *** CHANGE THIS LINE ***
		p.LastTimeCheck, err = time.Parse(time.RFC3339Nano, ts) // Use RFC3339Nano for precise parsing
		// *************************
		if err != nil {
			log.Printf("\x1b[31m[ERROR]\x1b[0m Failed to parse timestamp '%s' for peer %s: %v\n", ts, p.PeerID, err)
			// Fallback or skip this entry if parsing fails
			p.LastTimeCheck = time.Time{} // Set to zero time if parsing fails
		}

		peers = append(peers, p)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(peers); err != nil {
		log.Printf("\x1b[31m[ERROR]\x1b[0m Failed to encode peers JSON: %v\n", err) // Red color for error
	}
	log.Println("\x1b[32m[API]\x1b[0m /peers endpoint served.") // Green color for API
}

func handleHehojExiste(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "\x1b[31m[ERROR]\x1b[0m Only POST method is allowed for /hehojexiste", http.StatusMethodNotAllowed)
		return
	}

	var req HehojExisteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("\x1b[31m[ERROR]\x1b[0m Failed to decode request body: %v\n", err), http.StatusBadRequest)
		return
	}

	if req.PeerID == "" {
		http.Error(w, "\x1b[31m[ERROR]\x1b[0m 'peer_id' is required", http.StatusBadRequest)
		return
	}

	log.Printf("\x1b[35m[API]\x1b[0m Received /hehojexiste request for PeerID: %s, AddressMap: %s\n", req.PeerID, req.AddressMap) // Magenta for API
	pingOK := pingPeerWithAddress(req.PeerID, req.AddressMap)

	// --- IMPORTANT CHANGE HERE ---
	// Record the ping result in the database
	upsertPeer(req.PeerID, time.Now(), pingOK)
	// --- END IMPORTANT CHANGE ---

	response := map[string]bool{"ping_successful": pingOK}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("\x1b[31m[ERROR]\x1b[0m Failed to encode /hehojexiste response: %v\n", err) // Red color for error
	}
	log.Printf("\x1b[32m[API]\x1b[0m /hehojexiste endpoint served for PeerID %s.\n", req.PeerID) // Green color for API
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK - version 1.1.3"))
	log.Println("\x1b[32m[API]\x1b[0m / endpoint served.") // Green color for API
}