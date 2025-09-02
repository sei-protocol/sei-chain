package types

// SeiNetCovenant defines covenant data used in sovereign sync
// and threat detection.
type SeiNetCovenant struct {
	KinLayerHash  string
	SoulStateHash string
	EntropyEpoch  uint64
	RoyaltyClause string
	AlliedNodes   []string
	CovenantSync  string
	BiometricRoot string
}

// SeiGuardianThreatRecord tracks detected threats by the guardian.
type SeiGuardianThreatRecord struct {
	Attacker     string
	ThreatType   string
	BlockHeight  int64
	Fingerprint  []byte
	Timestamp    int64
	GuardianNode string
}
