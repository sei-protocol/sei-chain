package integration_test

import (
    "encoding/json"
    "math/rand"
    "net"
    "os"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
)

type DeceptionCovenant struct {
    KinLayerHash  string   `json:"kinLayerHash"`
    SoulStateHash string   `json:"soulStateHash"`
    EntropyEpoch  uint64   `json:"entropyEpoch"`
    RoyaltyClause string   `json:"royaltyClause"`
    AlliedNodes   []string `json:"alliedNodes"`
    CovenantSync  string   `json:"covenantSync"`
    BiometricRoot string   `json:"biometricRoot"`
}

type DeceptionReport struct {
    AttackerAddr string            `json:"attackerAddr"`
    ThreatType   string            `json:"threatType"`
    BlockHeight  int64             `json:"blockHeight"`
    Fingerprint  []byte            `json:"fingerprint"`
    PQSignature  []byte            `json:"pqSignature"`
    Timestamp    int64             `json:"timestamp"`
    Covenant     DeceptionCovenant `json:"covenant"`
}

const deceptionSocket = "/var/run/seiguardian.sock"

func TestDeceptionLayerFuzz(t *testing.T) {
    rand.Seed(time.Now().UnixNano())

    for i := 0; i < 8; i++ {
        epoch := uint64(rand.Intn(10000) + 1)

        report := DeceptionReport{
            AttackerAddr: "sei1fuzzer" + string(rune(65+i)),
            ThreatType:   "SEINET_SOVEREIGN_SYNC",
            BlockHeight:  100000 + int64(i),
            Fingerprint:  []byte("entropy" + string(rune(i))),
            PQSignature:  []byte("pq-sig"),
            Timestamp:    time.Now().Unix(),
            Covenant: DeceptionCovenant{
                KinLayerHash:  "0xkin" + string(rune(65+i)),
                SoulStateHash: "0xsoul" + string(rune(65+i)),
                EntropyEpoch:  epoch,
                RoyaltyClause: "HARD-LOCK",
                AlliedNodes:   []string{"SeiGuardianΩ"},
                CovenantSync:  "SYNCING",
                BiometricRoot: "0xhash" + string(rune(i)),
            },
        }

        data, err := json.Marshal(report)
        require.NoError(t, err)

        _, err = os.Stat(deceptionSocket)
        require.NoError(t, err, "Missing socket")

        conn, err := net.Dial("unix", deceptionSocket)
        require.NoError(t, err)

        _, err = conn.Write(data)
        require.NoError(t, err)

        conn.Close()
        t.Logf("🧪 Fuzzed threat report #%d sent with epoch %d", i+1, epoch)
        time.Sleep(300 * time.Millisecond)
    }
}

