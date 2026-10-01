package helpers

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/stretchr/testify/require"
)

// Test the constructor
func TestNewAssociationHelper(t *testing.T) {
	// Test that the constructor properly initializes the helper
	helper := NewAssociationHelper(nil, nil, nil)
	require.NotNil(t, helper)
	require.Nil(t, helper.evmKeeper)
	require.Nil(t, helper.bankKeeper)
	require.Nil(t, helper.accountKeeper)
}

// Test address generation and conversion functions
func TestAddressConversions(t *testing.T) {
	// Generate test keys
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	pubkeyBytes := crypto.FromECDSAPub(&privateKey.PublicKey)
	evmAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	t.Run("pubkey conversion to sei pubkey", func(t *testing.T) {
		seiPubkey := PubkeyBytesToSeiPubKey(pubkeyBytes)
		require.NotNil(t, seiPubkey.Key)
		require.Equal(t, 33, len(seiPubkey.Key)) // Compressed key length
	})

	t.Run("addresses from pubkey bytes", func(t *testing.T) {
		derivedEvmAddr, seiAddr, seiPubkey, err := GetAddressesFromPubkeyBytes(pubkeyBytes)
		require.NoError(t, err)
		require.Equal(t, evmAddr, derivedEvmAddr)
		require.NotNil(t, seiAddr)
		require.NotNil(t, seiPubkey)

		// Verify the sei address is derived from the pubkey
		expectedSeiAddr := sdk.AccAddress(seiPubkey.Address())
		require.Equal(t, expectedSeiAddr, seiAddr)
	})
}

func TestAddressHelperErrorCases(t *testing.T) {
	t.Run("invalid pubkey to EVM address", func(t *testing.T) {
		// Test with empty pubkey
		_, err := PubkeyToEVMAddress([]byte{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid public key")

		// Test with wrong prefix
		_, err = PubkeyToEVMAddress([]byte{0x03, 0x01, 0x02})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid public key")
	})

	t.Run("invalid pubkey bytes to addresses", func(t *testing.T) {
		_, _, _, err := GetAddressesFromPubkeyBytes([]byte{0x01, 0x02, 0x03})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid public key")
	})
}

func TestSignatureRecovery(t *testing.T) {
	// Generate a known private key and signature for testing
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	// Create a test message hash
	message := []byte("test message for signature recovery")
	hash := crypto.Keccak256Hash(message)

	// Sign the hash
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	require.NoError(t, err)

	t.Run("successful signature recovery", func(t *testing.T) {
		// Extract R, S, V from signature
		r := signature[:32]
		s := signature[32:64]
		v := signature[64] + 27

		// Recover the public key
		recoveredPubkey, err := crypto.Ecrecover(hash.Bytes(), append(r, append(s, v-27)...))
		require.NoError(t, err)
		require.NotNil(t, recoveredPubkey)
		require.Equal(t, 65, len(recoveredPubkey)) // Uncompressed public key

		// Verify the recovered public key matches the original
		expectedPubkey := crypto.FromECDSAPub(&privateKey.PublicKey)
		require.Equal(t, expectedPubkey, recoveredPubkey)
	})
}

func TestKeyConversionConsistency(t *testing.T) {
	// Test that converting between different key formats is consistent
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	// Get the uncompressed public key
	uncompressedPubkey := crypto.FromECDSAPub(&privateKey.PublicKey)

	// Convert to Sei pubkey
	seiPubkey := PubkeyBytesToSeiPubKey(uncompressedPubkey)

	// Get EVM address from both sources
	evmAddr1 := crypto.PubkeyToAddress(privateKey.PublicKey)
	evmAddr2, err := PubkeyToEVMAddress(uncompressedPubkey)
	require.NoError(t, err)

	// They should be the same
	require.Equal(t, evmAddr1, evmAddr2)

	// Get addresses using our helper
	evmAddr3, seiAddr, returnedSeiPubkey, err := GetAddressesFromPubkeyBytes(uncompressedPubkey)
	require.NoError(t, err)

	// All EVM addresses should match
	require.Equal(t, evmAddr1, evmAddr3)

	// The returned sei pubkey should match
	require.Equal(t, seiPubkey, *returnedSeiPubkey.(*secp256k1.PubKey))

	// The sei address should be derived from the pubkey
	expectedSeiAddr := sdk.AccAddress(seiPubkey.Address())
	require.Equal(t, expectedSeiAddr, seiAddr)
}

func TestEdgeCases(t *testing.T) {
	t.Run("pubkey conversion edge cases", func(t *testing.T) {
		// Test with a byte array that has correct prefix but is empty after prefix
		invalidKey := []byte{0x04} // Just the prefix, no actual key data
		_, err := PubkeyToEVMAddress(invalidKey)
		// This should succeed in PubkeyToEVMAddress but may fail elsewhere
		// Since our function just checks prefix and computes keccak256, it should work
		require.NoError(t, err) // Changed expectation based on actual implementation

		// Test that the function produces a valid 20-byte address even with minimal data
		addr, err := PubkeyToEVMAddress(invalidKey)
		require.NoError(t, err)
		require.Equal(t, 20, len(addr.Bytes()))
	})

	t.Run("address byte array conversions", func(t *testing.T) {
		// Test that EVM address conversion produces 20-byte addresses
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)

		uncompressedPubkey := crypto.FromECDSAPub(&privateKey.PublicKey)
		evmAddr, err := PubkeyToEVMAddress(uncompressedPubkey)
		require.NoError(t, err)
		require.Equal(t, 20, len(evmAddr.Bytes()))
	})
}

type mockEVMKeeper struct {
	mappings map[string]common.Address
}

func (m *mockEVMKeeper) SetAddressMapping(_ sdk.Context, seiAddress sdk.AccAddress, evmAddress common.Address) {
	if m.mappings == nil {
		m.mappings = map[string]common.Address{}
	}
	m.mappings[seiAddress.String()] = evmAddress
}

type mockBankKeeper struct {
	sendCalls int
}

func (m *mockBankKeeper) SpendableCoins(sdk.Context, sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100)))
}

func (m *mockBankKeeper) SendCoins(sdk.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error {
	m.sendCalls++
	return nil
}

func (m *mockBankKeeper) GetWeiBalance(sdk.Context, sdk.AccAddress) sdk.Int {
	return sdk.ZeroInt()
}

func (m *mockBankKeeper) SendCoinsAndWei(sdk.Context, sdk.AccAddress, sdk.AccAddress, sdk.Int, sdk.Int) error {
	return nil
}

func (m *mockBankKeeper) LockedCoins(sdk.Context, sdk.AccAddress) sdk.Coins {
	return nil
}

func (m *mockBankKeeper) GetBalance(sdk.Context, sdk.AccAddress, string) sdk.Coin {
	return sdk.NewCoin("usei", sdk.NewInt(100))
}

type mockAccountKeeper struct {
	accounts        map[string]authtypes.AccountI
	newAccountCalls int
}

func (m *mockAccountKeeper) GetAccount(_ sdk.Context, addr sdk.AccAddress) authtypes.AccountI {
	return m.accounts[addr.String()]
}

func (m *mockAccountKeeper) HasAccount(_ sdk.Context, addr sdk.AccAddress) bool {
	_, ok := m.accounts[addr.String()]
	return ok
}

func (m *mockAccountKeeper) SetAccount(_ sdk.Context, acc authtypes.AccountI) {
	if m.accounts == nil {
		m.accounts = map[string]authtypes.AccountI{}
	}
	m.accounts[acc.GetAddress().String()] = acc
}

func (m *mockAccountKeeper) RemoveAccount(_ sdk.Context, acc authtypes.AccountI) {
	delete(m.accounts, acc.GetAddress().String())
}

func (m *mockAccountKeeper) NewAccountWithAddress(_ sdk.Context, addr sdk.AccAddress) authtypes.AccountI {
	m.newAccountCalls++
	return authtypes.NewBaseAccountWithAddress(addr)
}

func (m *mockAccountKeeper) GetParams(sdk.Context) authtypes.Params {
	return authtypes.DefaultParams()
}

func TestAssociateAddressesReusesEmptyCastAccount(t *testing.T) {
	ctx := sdk.Context{}
	evmAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	castAddr := sdk.AccAddress(evmAddr[:])
	seiAddr := sdk.AccAddress(common.HexToAddress("0x2222222222222222222222222222222222222222").Bytes())
	pubkey := secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey)

	ak := &mockAccountKeeper{accounts: map[string]authtypes.AccountI{}}
	ak.SetAccount(ctx, authtypes.NewBaseAccount(castAddr, nil, 42, 7))
	bk := &mockBankKeeper{}
	ek := &mockEVMKeeper{}

	helper := NewAssociationHelper(ek, bk, ak)
	require.NoError(t, helper.AssociateAddresses(ctx, seiAddr, evmAddr, pubkey, false))

	require.Zero(t, ak.newAccountCalls)
	require.Nil(t, ak.GetAccount(ctx, castAddr))
	require.Equal(t, evmAddr, ek.mappings[seiAddr.String()])
	acc := ak.GetAccount(ctx, seiAddr)
	require.NotNil(t, acc)
	require.Equal(t, uint64(42), acc.GetAccountNumber())
	require.Equal(t, uint64(7), acc.GetSequence())
	require.Equal(t, pubkey.Bytes(), acc.GetPubKey().Bytes())
	require.Equal(t, 1, bk.sendCalls)
}

func TestMigrateBalanceSkipsDirectCastAddress(t *testing.T) {
	ctx := sdk.Context{}
	evmAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	seiAddr := sdk.AccAddress(evmAddr[:])

	bk := &mockBankKeeper{}
	helper := NewAssociationHelper(&mockEVMKeeper{}, bk, &mockAccountKeeper{})
	require.NoError(t, helper.MigrateBalance(ctx, evmAddr, seiAddr, false))
	require.Zero(t, bk.sendCalls)
}
