// Package preinstalls holds the standard singleton contracts gen-autobahn-config
// writes into an EVM-only chain's genesis alloc, grouped into levels.
package preinstalls

import (
	"bytes"
	"embed"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// Account is a contract at a fixed address from a chain's first block.
type Account struct {
	Name    string
	Address common.Address
	Nonce   uint64
	Code    []byte
	Storage map[common.Hash]common.Hash
}

// Levels, each holding the contracts of the one before it.
const (
	// None is no contracts.
	None = "none"
	// Factories is the two CREATE2 factories deployed by keyless transactions.
	Factories = "factories"
	// EOA is Factories and every other contract deployed by an account's
	// transaction rather than through a CREATE2 factory.
	EOA = "eoa"
	// Full is EOA and the contracts deployed through a CREATE2 factory.
	Full = "full"
)

var levels = map[string][]contract{
	None:      nil,
	Factories: factories,
	EOA:       slices.Concat(factories, eoa),
	Full:      slices.Concat(factories, eoa, create2),
}

// Levels returns every level name, smallest first.
func Levels() []string {
	return []string{None, Factories, EOA, Full}
}

// Accounts returns level's contracts for chainID, sorted by address.
func Accounts(level string, chainID *big.Int) ([]Account, error) {
	contracts, ok := levels[level]
	if !ok {
		return nil, fmt.Errorf("unknown preinstall level %q, want one of %v", level, Levels())
	}
	accounts := make([]Account, 0, len(contracts))
	for _, c := range contracts {
		accounts = append(accounts, Account{Name: c.name, Address: c.address, Nonce: c.nonce, Code: c.code(chainID), Storage: maps.Clone(c.storage)})
	}
	slices.SortFunc(accounts, func(a, b Account) int { return a.Address.Cmp(b.Address) })
	return accounts, nil
}

//go:embed bytecode/*.hex
var bytecode embed.FS

// contract is a preinstall whose code is read from bytecode/<name>.hex and,
// when immutables is set, patched for the chain it is installed on.
type contract struct {
	name       string
	address    common.Address
	nonce      uint64
	storage    map[common.Hash]common.Hash
	immutables []immutable
}

// immutable is a 32-byte word of runtime code a constructor derived from the
// chain ID.
type immutable struct {
	offset int
	value  func(chainID *big.Int, address common.Address) common.Hash
}

// mainnetChainID is the chain the embedded runtime code was taken from.
var mainnetChainID = big.NewInt(1)

// Every contract's code, nonce and storage match its deployment on Ethereum
// mainnet, and its code matches Sei mainnet's once patched for chain 1329.
var (
	// factories are the CREATE2 factories deployed by keyless transactions
	// without replay protection.
	factories = []contract{
		{name: "ArachnidCreate2Proxy", address: common.HexToAddress("0x4e59b44847b379578588920cA78FbF26c0B4956C"), nonce: 1},
		{name: "ERC2470SingletonFactory", address: common.HexToAddress("0xce0042B868300000d44A59004Da54A005ffdcf9f"), nonce: 1},
	}
	// eoa are deployed by a transaction from a key their maintainers hold.
	eoa = []contract{
		{name: "SafeSingletonFactory", address: common.HexToAddress("0x914d7Fec6aaC8cd542e72Bca78B30650d45643d7"), nonce: 1},
		{name: "CreateX", address: common.HexToAddress("0xba5Ed099633D3B313e4D5F7bdc1305d3c28ba5Ed"), nonce: 1},
		{name: "Multicall3", address: common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"), nonce: 1},
	}
	// create2 are deployed through a CREATE2 factory, with the contracts their
	// constructors create.
	create2 = []contract{
		{name: "ImmutableCreate2Factory", address: common.HexToAddress("0x0000000000FFe8B47B3e2130213B802212439497"), nonce: 1},
		{name: "Permit2", address: common.HexToAddress("0x000000000022D473030F116dDEE9F6B43aC78BA3"), nonce: 1, immutables: []immutable{
			{offset: 6945, value: chainIDWord},
			{offset: 6983, value: permit2DomainSeparator},
		}},
		// Each EntryPoint's constructor deployed its SenderCreator with CREATE, which took its nonce to 2.
		{name: "EntryPointV06", address: common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"), nonce: 2, storage: reentrancyGuardNotEntered},
		{name: "SenderCreatorV06", address: common.HexToAddress("0x7fc98430eAEdbb6070B35B39D798725049088348"), nonce: 1},
		{name: "EntryPointV07", address: common.HexToAddress("0x0000000071727De22E5E9d8BAf0edAc6f37da032"), nonce: 2, storage: reentrancyGuardNotEntered},
		{name: "SenderCreatorV07", address: common.HexToAddress("0xEFC2c1444eBCC4Db75e7613d20C6a62fF67A167C"), nonce: 1},
		{name: "EntryPointV08", address: common.HexToAddress("0x4337084D9E255Ff0702461CF8895CE9E3b5Ff108"), nonce: 2, immutables: []immutable{
			{offset: 13585, value: entryPointV08DomainSeparator},
			{offset: 13774, value: chainIDWord},
		}},
		{name: "SenderCreatorV08", address: common.HexToAddress("0x449ED7C3e6Fee6a97311d4b55475DF59C44AdD33"), nonce: 1},
	}
)

// reentrancyGuardNotEntered is the storage EntryPoint v0.6's and v0.7's
// constructors write: OpenZeppelin's ReentrancyGuard status, in slot 2, set to
// not entered.
var reentrancyGuardNotEntered = map[common.Hash]common.Hash{common.BigToHash(big.NewInt(2)): common.BigToHash(big.NewInt(1))}

// code returns c's runtime code with each immutable set for chainID. It panics
// when the embedded code does not hold an immutable's mainnet value.
func (c contract) code(chainID *big.Int) []byte {
	code := readBytecode(c.name)
	for _, imm := range c.immutables {
		word := code[imm.offset : imm.offset+common.HashLength]
		if mainnet := imm.value(mainnetChainID, c.address); !bytes.Equal(word, mainnet[:]) {
			panic(fmt.Sprintf("preinstall %s: word at %d is %x, not its mainnet value %x", c.name, imm.offset, word, mainnet))
		}
		patched := imm.value(chainID, c.address)
		copy(word, patched[:])
	}
	return code
}

func readBytecode(name string) []byte {
	raw, err := bytecode.ReadFile("bytecode/" + name + ".hex")
	if err != nil {
		panic(fmt.Sprintf("preinstall %s: %v", name, err))
	}
	code, err := hexutil.Decode(strings.TrimSpace(string(raw)))
	if err != nil {
		panic(fmt.Sprintf("preinstall %s: %v", name, err))
	}
	return code
}

func chainIDWord(chainID *big.Int, _ common.Address) common.Hash {
	return common.BigToHash(chainID)
}

var (
	// Permit2 hashes its domain without a version.
	permit2DomainTypeHash = crypto.Keccak256Hash([]byte("EIP712Domain(string name,uint256 chainId,address verifyingContract)"))
	permit2NameHash       = crypto.Keccak256Hash([]byte("Permit2"))

	eip712DomainTypeHash     = crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	entryPointV08NameHash    = crypto.Keccak256Hash([]byte("ERC4337"))
	entryPointV08VersionHash = crypto.Keccak256Hash([]byte("1"))
)

func permit2DomainSeparator(chainID *big.Int, address common.Address) common.Hash {
	return crypto.Keccak256Hash(permit2DomainTypeHash[:], permit2NameHash[:], common.BigToHash(chainID).Bytes(), common.BytesToHash(address[:]).Bytes())
}

func entryPointV08DomainSeparator(chainID *big.Int, address common.Address) common.Hash {
	return crypto.Keccak256Hash(eip712DomainTypeHash[:], entryPointV08NameHash[:], entryPointV08VersionHash[:], common.BigToHash(chainID).Bytes(), common.BytesToHash(address[:]).Bytes())
}
