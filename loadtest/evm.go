package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"

	"math/big"
	mathrand "math/rand"
	"sync"
	"sync/atomic"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"strings"

	crand "crypto/rand"

	"github.com/sei-protocol/sei-chain/loadtest/contracts/evm/bindings/erc20"
	"github.com/sei-protocol/sei-chain/loadtest/contracts/evm/bindings/erc721"
	"github.com/sei-protocol/sei-chain/loadtest/contracts/evm/bindings/univ2_swapper"
)

var (
	DefaultPriorityFee = big.NewInt(1000000000)    // 1gwei
	DefaultMaxFee      = big.NewInt(1000000000000) // 1000gwei
)

type EvmTxClient struct {
	accountAddress common.Address
	nonce          atomic.Uint64
	chainId        *big.Int
	gasPrice       *big.Int
	ethClients     []*ethclient.Client
	mtx            sync.RWMutex
	privateKey     *ecdsa.PrivateKey
	evmAddresses   *EVMAddresses
	useEip1559     bool
}

func NewEvmTxClient(
	key cryptotypes.PrivKey,
	chainId *big.Int,
	gasPrice *big.Int,
	ethClients []*ethclient.Client,
	evmAddresses *EVMAddresses,
	useEip1559 bool,
) *EvmTxClient {
	if evmAddresses == nil {
		evmAddresses = &EVMAddresses{}
	}
	txClient := &EvmTxClient{
		chainId:      chainId,
		gasPrice:     gasPrice,
		ethClients:   ethClients,
		mtx:          sync.RWMutex{},
		evmAddresses: evmAddresses,
	}
	privKeyHex := hex.EncodeToString(key.Bytes())
	privateKey, err := crypto.HexToECDSA(privKeyHex)
	if err != nil {
		fmt.Printf("Failed to load private key: %v \n", err)
	}
	txClient.privateKey = privateKey

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		panic("Cannot assert type: publicKey is not of type *ecdsa.PublicKey \n")
	}

	// Set starting nonce
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	nextNonce, err := ethClients[0].PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		panic(err)
	}
	txClient.nonce.Store(nextNonce)
	txClient.accountAddress = fromAddress
	txClient.useEip1559 = useEip1559

	return txClient
}

func (txClient *EvmTxClient) GetTxForMsgType(msgType string) *ethtypes.Transaction {
	switch msgType {
	case EVM:
		return txClient.GenerateSendFundsTx()
	case ERC20:
		return txClient.GenerateERC20TransferTx()
	case ERC721:
		return txClient.GenerateERC721Mint()
	case UNIV2:
		return txClient.GenerateUniV2SwapTx()
	case DisperseETH:
		return txClient.GenerateDisperseEthTx()
	default:
		panic("invalid message type")
	}
}

func randomValue() *big.Int {
	return big.NewInt(mathrand.Int63n(9000000) * 1000000000000)
}

// GenerateSendFundsTx returns a random send funds tx
//
//nolint:staticcheck
func (txClient *EvmTxClient) GenerateSendFundsTx() *ethtypes.Transaction {
	var tx *ethtypes.Transaction
	if !txClient.useEip1559 {
		tx = ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    txClient.nextNonce(),
			GasPrice: DefaultMaxFee,
			Gas:      uint64(21000),
			To:       &txClient.accountAddress,
			Value:    randomValue(),
		})
	} else {
		dynamicTx := &ethtypes.DynamicFeeTx{
			ChainID:   txClient.chainId,
			Nonce:     txClient.nextNonce(),
			GasTipCap: DefaultPriorityFee,
			GasFeeCap: DefaultMaxFee,
			Gas:       uint64(21000),
			To:        &txClient.accountAddress,
			Value:     randomValue(),
		}
		tx = ethtypes.NewTx(dynamicTx)
	}
	return txClient.sign(tx)
}

// GenerateERC20TransferTx returns a random ERC20 send
// the contract it interacts with needs no funding (infinite balances)
func (txClient *EvmTxClient) GenerateERC20TransferTx() *ethtypes.Transaction {
	opts := txClient.getTransactOpts()
	// override gas limit for an ERC20 transfer
	opts.GasLimit = uint64(100000)
	tokenAddress := txClient.evmAddresses.ERC20
	token, err := erc20.NewErc20(tokenAddress, GetNextEthClient(txClient.ethClients))
	if err != nil {
		panic(fmt.Sprintf("Failed to create ERC20 contract: %v \n", err))
	}
	tx, err := token.Transfer(opts, txClient.accountAddress, randomValue())
	if err != nil {
		panic(fmt.Sprintf("Failed to create ERC20 transfer: %v \n", err))
	}
	return txClient.sign(tx)
}

func (txClient *EvmTxClient) GenerateUniV2SwapTx() *ethtypes.Transaction {
	opts := txClient.getTransactOpts()
	opts.GasLimit = uint64(200000)
	univ2Swapper, err := univ2_swapper.NewUniv2Swapper(txClient.evmAddresses.UniV2Swapper, GetNextEthClient(txClient.ethClients))
	if err != nil {
		panic(fmt.Sprintf("Failed to create univ2 swapper contract: %v \n", err))
	}
	tx, err := univ2Swapper.Swap(opts)
	if err != nil {
		panic(fmt.Sprintf("Failed to create univ2 swap: %v \n", err))
	}
	return txClient.sign(tx)
}

func (txClient *EvmTxClient) GenerateERC721Mint() *ethtypes.Transaction {
	opts := txClient.getTransactOpts()
	// override gas limit for an ERC20 transfer
	opts.GasLimit = uint64(100000)
	tokenAddress := txClient.evmAddresses.ERC721
	token, err := erc721.NewErc721(tokenAddress, GetNextEthClient(txClient.ethClients))
	if err != nil {
		panic(fmt.Sprintf("Failed to create ERC721 contract: %v \n", err))
	}
	tx, err := token.Mint(opts, txClient.accountAddress, randomValue())
	if err != nil {
		panic(fmt.Sprintf("Failed to create ERC20 transfer: %v \n", err))
	}
	return tx
}

func (txClient *EvmTxClient) GenerateDisperseEthTx() *ethtypes.Transaction {
	// Check that we have a valid disperse contract address
	if txClient.evmAddresses.DisperseETH.Cmp(common.Address{}) == 0 {
		panic("DisperseETH contract address is not set - deployment may have failed")
	}

	// Build a disperse transaction that calls the disperseEther(recipients, values) payable method
	// We create 10-100 random recipients and send 1 wei to each so that the total value is recipients*1 wei.
	numRecipients := mathrand.Intn(91) + 10 // 10-100

	recipients := make([]common.Address, numRecipients)
	values := make([]*big.Int, numRecipients)
	totalValue := big.NewInt(0)
	for i := 0; i < numRecipients; i++ {
		var addrBytes [20]byte
		_, _ = crand.Read(addrBytes[:])
		recipients[i] = common.BytesToAddress(addrBytes[:])
		values[i] = big.NewInt(1) // 1 wei each
		totalValue.Add(totalValue, values[i])
	}

	// Prepare ABI-encoded calldata
	disperseABI, err := abi.JSON(strings.NewReader(`[{"inputs":[{"internalType":"address[]","name":"recipients","type":"address[]"},{"internalType":"uint256[]","name":"values","type":"uint256[]"}],"name":"disperseEther","outputs":[],"stateMutability":"payable","type":"function"}]`))
	if err != nil {
		panic(fmt.Sprintf("Failed to parse disperse ABI: %v \n", err))
	}
	data, err := disperseABI.Pack("disperseEther", recipients, values)
	if err != nil {
		panic(fmt.Sprintf("Failed to pack disperse calldata: %v \n", err))
	}

	var tx *ethtypes.Transaction
	if !txClient.useEip1559 {
		tx = ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    txClient.nextNonce(),
			GasPrice: DefaultMaxFee,
			Gas:      uint64(1000000), // target ~1M gas
			To:       &txClient.evmAddresses.DisperseETH,
			Value:    totalValue,
			Data:     data,
		})
	} else {
		dynamicTx := &ethtypes.DynamicFeeTx{
			ChainID:   txClient.chainId,
			Nonce:     txClient.nextNonce(),
			GasTipCap: DefaultPriorityFee,
			GasFeeCap: DefaultMaxFee,
			Gas:       uint64(1000000),
			To:        &txClient.evmAddresses.DisperseETH,
			Value:     totalValue,
			Data:      data,
		}
		tx = ethtypes.NewTx(dynamicTx)
	}
	return txClient.sign(tx)
}

func (txClient *EvmTxClient) getTransactOpts() *bind.TransactOpts {
	auth, err := bind.NewKeyedTransactorWithChainID(txClient.privateKey, txClient.chainId)
	if err != nil {
		panic(fmt.Sprintf("Failed to create transactor: %v \n", err))
	}
	if !txClient.useEip1559 {
		auth.GasPrice = DefaultMaxFee
	} else {
		auth.GasFeeCap = DefaultMaxFee
		auth.GasTipCap = DefaultPriorityFee
	}
	auth.Nonce = big.NewInt(int64(txClient.nextNonce()))
	auth.Value = big.NewInt(0)
	auth.GasLimit = uint64(21000)
	auth.Context = context.Background()
	auth.From = txClient.accountAddress
	auth.NoSend = true
	return auth
}

func (txClient *EvmTxClient) sign(tx *ethtypes.Transaction) *ethtypes.Transaction {
	signedTx, err := ethtypes.SignTx(tx, ethtypes.NewLondonSigner(txClient.chainId), txClient.privateKey)
	if err != nil {
		// this should not happen
		panic(err)
	}
	return signedTx
}

func (txClient *EvmTxClient) nextNonce() uint64 {
	txClient.mtx.RLock()
	defer txClient.mtx.RUnlock()
	return txClient.nonce.Add(1) - 1
}

// SendEvmTx takes any signed evm tx and send it out
func (txClient *EvmTxClient) SendEvmTx(signedTx *ethtypes.Transaction, onSuccess func()) {
	err := GetNextEthClient(txClient.ethClients).SendTransaction(context.Background(), signedTx)
	if err != nil {
		fmt.Printf("Failed to send evm transaction: %v \n", err)
	} else {
		onSuccess()
	}
}

// GetNextEthClient return the next available eth client randomly
//
//nolint:staticcheck
func GetNextEthClient(clients []*ethclient.Client) *ethclient.Client {
	numClients := len(clients)
	if numClients <= 0 {
		panic("There's no ETH client available, make sure your connection are valid")
	}
	mathrand.Seed(time.Now().Unix())
	return clients[mathrand.Int()%numClients]
}

// GetTxReceipt query the transaction receipt to check if the tx succeed or not
func (txClient *EvmTxClient) GetTxReceipt(txHash common.Hash) error {
	_, err := GetNextEthClient(txClient.ethClients).TransactionReceipt(context.Background(), txHash)
	if err != nil {
		return err
	}
	return nil
}

// check receipt success
func (txClient *EvmTxClient) EnsureTxSuccess(txHash common.Hash) {
	receipt, err := GetNextEthClient(txClient.ethClients).TransactionReceipt(context.Background(), txHash)
	if err != nil {
		panic(fmt.Sprintf("Failed to get receipt for tx %v: %v \n", txHash.Hex(), err))
	}
	if receipt.Status != 1 {
		panic(fmt.Sprintf("Tx %v failed with status %v \n", txHash.Hex(), receipt.Status))
	}
}

// ResetNonce need to be called when tx failed
func (txClient *EvmTxClient) ResetNonce() error {
	txClient.mtx.Lock()
	defer txClient.mtx.Unlock()
	client := GetNextEthClient(txClient.ethClients)
	newNonce, err := client.PendingNonceAt(context.Background(), txClient.accountAddress)
	if err != nil {
		return err
	}
	txClient.nonce.Store(newNonce)
	fmt.Printf("Resetting nonce to %d for addr: %s\n ", newNonce, txClient.accountAddress.String())
	return nil
}
