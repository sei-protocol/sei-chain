package integration_test

import (
    "encoding/json"
    "net"
    "os"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
)

type SyncCovenant struct {
    KinLayerHash  string   `json:"kinLayerHash"`
    SoulStateHash string   `json:"soulStateHash"`
    EntropyEpoch  uint64   `json:"entropyEpoch"`
    RoyaltyClause string   `json:"royaltyClause"`
    AlliedNodes   []string `json:"alliedNodes"`
    CovenantSync  string   `json:"covenantSync"`
    BiometricRoot string   `json:"biometricRoot"`
}

type SyncReport struct {
    AttackerAddr string       `json:"attackerAddr"`
    ThreatType   string       `json:"threatType"`
    BlockHeight  int64        `json:"blockHeight"`
    Fingerprint  []byte       `json:"fingerprint"`
    PQSignature  []byte       `json:"pqSignature"`
    Timestamp    int64        `json:"timestamp"`
    Covenant     SyncCovenant `json:"covenant"`
}

func TestSovereignEpochTrigger(t *testing.T) {
    epochs := []uint64{9973, 19946, 39946, 12345, 7777, 19946 * 2}

    for _, epoch := range epochs {
        report := SyncReport{
            AttackerAddr: "sei1sovereign" + time.Now().Format("150405"),
            ThreatType:   "SEINET_SOVEREIGN_SYNC",
            BlockHeight:  100777,
            Fingerprint:  []byte("sovereign-ping"),
            PQSignature:  []byte("OmegaSig"),
            Timestamp:    time.Now().Unix(),
            Covenant: SyncCovenant{
                KinLayerHash:  "0xkin9973",
                SoulStateHash: "0xsoul777",
                EntropyEpoch:  epoch,
                RoyaltyClause: "ENFORCED",
                AlliedNodes:   []string{"ΩValidator"},
                CovenantSync:  "LOCKED",
                BiometricRoot: "0xbiom9973",
            },
        }

        data, err := json.Marshal(report)
        require.NoError(t, err)

        conn, err := net.Dial("unix", deceptionSocket)
        require.NoError(t, err)

        _, err = conn.Write(data)
        require.NoError(t, err)
        conn.Close()

        t.Logf("🧬 Sent epoch-triggered report with epoch %d", epoch)
        time.Sleep(1 * time.Second)
    }
}

