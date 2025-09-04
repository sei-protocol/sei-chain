package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

var (
	nodeURL       = flag.String("node", "http://localhost:26657", "Tendermint RPC address")
	socketPath    = flag.String("socket", "/var/run/qacis.sock", "QACIS Unix socket path")
	pollInterval  = flag.Duration("interval", 5*time.Second, "Polling interval")
	riskThreshold = flag.Int("risk", 204, "Risk threshold for reporting (0-255)")
	sentinelID    = flag.String("sentinel", "guardian-0", "Sentinel identifier")
	rotateEvery   = flag.Duration("pq-rotate", 10*time.Minute, "PQ key rotation interval")
)

var pqKey []byte

type ThreatReport struct {
	AttackerAddr string `json:"attackerAddr"`
	ThreatType   string `json:"threatType"`
	BlockHeight  int64  `json:"blockHeight"`
	Fingerprint  []byte `json:"fingerprint"`
	PQSignature  []byte `json:"pqSignature"`
	GuardianNode string `json:"guardianNode"`
	RiskScore    uint8  `json:"riskScore"`
	Timestamp    int64  `json:"timestamp"`
}

func main() {
	flag.Parse()
	pqKey = generatePQKey()
	pollTicker := time.NewTicker(*pollInterval)
	rotateTicker := time.NewTicker(*rotateEvery)
	defer pollTicker.Stop()
	defer rotateTicker.Stop()

	for {
		select {
		case <-rotateTicker.C:
			pqKey = generatePQKey()
			log.Printf("rotated PQ key")
		case <-pollTicker.C:
			height := queryBlockHeight()
			inspectMempool(height)
		}
	}
}

func queryBlockHeight() int64 {
	resp, err := http.Get(fmt.Sprintf("%s/status", *nodeURL))
	if err != nil {
		log.Printf("status query failed: %v", err)
		return 0
	}
	defer resp.Body.Close()
	var r struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight string `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		log.Printf("decode status: %v", err)
		return 0
	}
	height, _ := strconv.ParseInt(r.Result.SyncInfo.LatestBlockHeight, 10, 64)
	return height
}

func inspectMempool(height int64) {
	resp, err := http.Get(fmt.Sprintf("%s/unconfirmed_txs?limit=10", *nodeURL))
	if err != nil {
		log.Printf("mempool query failed: %v", err)
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("read mempool: %v", err)
		return
	}
	var r struct {
		Result struct {
			Txs []string `json:"txs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		log.Printf("decode mempool: %v", err)
		return
	}
	for _, tx := range r.Result.Txs {
		score := scoreTx(tx)
		if score >= uint8(*riskThreshold) {
			fp := []byte(tx)
			sig := pqSign(fp)
			report := ThreatReport{
				AttackerAddr: "unknown",
				ThreatType:   "MEMPOOL_SCAN",
				BlockHeight:  height,
				Fingerprint:  fp,
				PQSignature:  sig,
				GuardianNode: *sentinelID,
				RiskScore:    score,
				Timestamp:    time.Now().Unix(),
			}
			if err := sendThreat(report); err != nil {
				log.Printf("send threat: %v", err)
			} else {
				log.Printf("threat reported at height %d with score %d", height, score)
			}
		}
	}
}

func scoreTx(tx string) uint8 {
	h := sha256.Sum256([]byte(tx))
	// use first byte as pseudo score
	return h[0]
}

func pqSign(data []byte) []byte {
	h := sha256.New()
	h.Write(pqKey)
	h.Write(data)
	return []byte(hex.EncodeToString(h.Sum(nil)))
}

func generatePQKey() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return []byte("default-pq-key")
	}
	return b
}

func sendThreat(report ThreatReport) error {
	conn, err := net.Dial("unix", *socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}
