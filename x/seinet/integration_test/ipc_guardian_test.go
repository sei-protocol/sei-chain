// ipc_guardian_test.go — Omega Guardian → SeiNet IPC Integration

package integration_test

import (
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	GuardianSocketPath = "/var/run/seiguardian.sock"
)

type TestCovenant struct {
	KinLayerHash  string   `json:"kinLayerHash"`
	SoulStateHash string   `json:"soulStateHash"`
	EntropyEpoch  uint64   `json:"entropyEpoch"`
	RoyaltyClause string   `json:"royaltyClause"`
	AlliedNodes   []string `json:"alliedNodes"`
	CovenantSync  string   `json:"covenantSync"`
	BiometricRoot string   `json:"biometricRoot"`
}

type TestThreatReport struct {
	AttackerAddr string       `json:"attackerAddr"`
	ThreatType   string       `json:"threatType"`
	BlockHeight  int64        `json:"blockHeight"`
	Fingerprint  []byte       `json:"fingerprint"`
	PQSignature  []byte       `json:"pqSignature"`
	Timestamp    int64        `json:"timestamp"`
	Covenant     TestCovenant `json:"covenant"`
}

func TestGuardianIPC(t *testing.T) {
	// Prepare fake report
	report := TestThreatReport{
		AttackerAddr: "sei1hackerxxxxxxx",
		ThreatType:   "SEINET_SOVEREIGN_SYNC",
		BlockHeight:  123456,
		Fingerprint:  []byte("test-fp-omega"),
		PQSignature:  []byte("sig-1234"), // Acceptable stub
		Timestamp:    time.Now().Unix(),
		Covenant: TestCovenant{
			KinLayerHash:  "0xkinabc123",
			SoulStateHash: "0xsoulxyz456",
			EntropyEpoch:  19946,
			RoyaltyClause: "CLAUSE_Ω11",
			AlliedNodes:   []string{"sei-guardian-Ω"},
			CovenantSync:  "PENDING",
			BiometricRoot: "0xfacefeed",
		},
	}

	data, err := json.Marshal(report)
	require.NoError(t, err)

	// Ensure socket exists
	_, err = os.Stat(GuardianSocketPath)
	require.NoError(t, err, "Socket not found — is Guardian IPC listener running?")

	conn, err := net.Dial("unix", GuardianSocketPath)
	require.NoError(t, err, "Failed to connect to Guardian socket")

	_, err = conn.Write(data)
	require.NoError(t, err, "Failed to write threat report")

	conn.Close()
	t.Log("🧬 Threat report sent — check keeper state for final_covenant KV")
}
