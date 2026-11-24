package statesyncproof

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// verifyRoad is the first road of epoch 0 of the registries that verifyTest builds.
const verifyRoad types.RoadHeight = 5

// verifyTest is the genesis epoch registry of a chain and the keys of its committee.
type verifyTest struct {
	rng      utils.Rng
	keys     []types.SecretKey
	registry *types.EpochRegistry
}

func newVerifyTest(t *testing.T, rng utils.Rng) *verifyTest {
	t.Helper()
	keys := make([]types.SecretKey, 4)
	weights := make(map[types.PublicKey]uint64, len(keys))
	for i := range keys {
		keys[i] = types.GenSecretKey(rng)
		weights[keys[i].Public()] = 1
	}
	registry, err := types.GenesisRegistry(weights, verifyRoad, 10)
	require.NoError(t, err)
	return &verifyTest{rng: rng, keys: keys, registry: registry}
}

// proof returns a state sync proof file of verifyRoad that the committee signs for appHash.
func (v *verifyTest) proof(t *testing.T, appHash []byte) []byte {
	t.Helper()
	data, err := EncodeWithKeysForTesting(v.rng, v.registry.Epochs()[0], v.keys, appHash)
	require.NoError(t, err)
	return data
}

func TestDecodeAndVerifyReturnsTheProofAndTheSignedAppHash(t *testing.T) {
	v := newVerifyTest(t, utils.TestRng())
	appHash := [32]byte{0xa1, 0x5e}

	proof, got, err := DecodeAndVerify(v.registry, v.proof(t, appHash[:]), verifyRoad)
	require.NoError(t, err)
	require.Equal(t, appHash, got)
	require.Equal(t, verifyRoad, proof.Height())
}

func TestDecodeAndVerifyRejectsAProofOfAnotherCommittee(t *testing.T) {
	rng := utils.TestRng()
	v := newVerifyTest(t, rng)
	other := newVerifyTest(t, rng)
	appHash := [32]byte{1}

	_, _, err := DecodeAndVerify(v.registry, other.proof(t, appHash[:]), verifyRoad)
	require.ErrorContains(t, err, "verify the state sync proof file")
}

func TestDecodeAndVerifyRejectsARoadOutsideTheRegistry(t *testing.T) {
	v := newVerifyTest(t, utils.TestRng())
	appHash := [32]byte{1}

	_, _, err := DecodeAndVerify(v.registry, v.proof(t, appHash[:]), verifyRoad-1)
	require.ErrorContains(t, err, "holds no epoch of road")
}

func TestDecodeAndVerifyRejectsAFileOfAnotherRoad(t *testing.T) {
	v := newVerifyTest(t, utils.TestRng())
	appHash := [32]byte{1}

	_, _, err := DecodeAndVerify(v.registry, v.proof(t, appHash[:]), verifyRoad+1)
	require.ErrorContains(t, err, "decode the state sync proof file")
}

func TestDecodeAndVerifyRejectsAnAppHashThatIsNot32Bytes(t *testing.T) {
	v := newVerifyTest(t, utils.TestRng())

	_, _, err := DecodeAndVerify(v.registry, v.proof(t, []byte{1, 2, 3}), verifyRoad)
	require.ErrorContains(t, err, "want 32")
}
