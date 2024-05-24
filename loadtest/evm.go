package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"runtime"

	"math/big"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/sei-protocol/sei-chain/loadtest/contracts/evm/bindings/erc20"
	"github.com/sei-protocol/sei-chain/loadtest/contracts/evm/bindings/erc721"
	"github.com/sei-protocol/sei-chain/loadtest/contracts/evm/bindings/univ2_swapper"
)

var (
	DefaultPriorityFee = big.NewInt(2000000000) // 2 gwei, source: https://archive.ph/gOO0q#selection-1341.40-1341.164
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

	// eip-1559 base fee poller
	baseFeePollerStop chan struct{}
	baseFeePollerWg   sync.WaitGroup
	curBaseFee        *big.Int
	baseFeeMu         sync.RWMutex
}

func NewEvmTxClient(
	key cryptotypes.PrivKey,
	chainId *big.Int,
	gasPrice *big.Int,
	ethClients []*ethclient.Client,
	evmAddresses *EVMAddresses,
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

	// Launch persistent goroutine to continuously poll for base fee on a time interval
	go txClient.PollBaseFee()
	// Set finalizer to stop the base fee poller when txClient is garbage collected
	runtime.SetFinalizer(txClient, func(txc *EvmTxClient) {
		close(txc.baseFeePollerStop)
		txc.baseFeePollerWg.Wait() // Wait for goroutine to finish
		fmt.Println("Finalizer executed: base fee poller stopped")
	})
	return txClient
}

func (txClient *EvmTxClient) GetTxForMsgType(msgType string) *ethtypes.Transaction {
	switch msgType {
	case EVM:
		fmt.Println("Generating Send Funds Tx")
		return txClient.GenerateSendFundsTx()
	case ERC20:
		fmt.Println("Generating ERC20 Transfer Tx")
		return txClient.GenerateERC20TransferTx()
	case ERC721:
		fmt.Println("Generating ERC721 Mint Tx")
		return txClient.GenerateERC721Mint()
	case UNIV2:
		fmt.Println("Generating UniV2 Swap Tx")
		return txClient.GenerateUniV2SwapTx()
	default:
		panic("invalid message type")
	}
}

func randomValue() *big.Int {
	return big.NewInt(rand.Int63n(9000000) * 1000000000000)
}

// GenerateSendFundsTx returns a random send funds tx
//
//nolint:staticcheck
func (txClient *EvmTxClient) GenerateSendFundsTx() *ethtypes.Transaction {
	maxFee, err := txClient.GetMaxFee(DefaultPriorityFee)
	var tx *ethtypes.Transaction
	if err != nil {
		fmt.Errorf("Failed to get max fee, err: %v, defaulting to legacy txs", err)
		tx = ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    txClient.nextNonce(),
			GasPrice: txClient.gasPrice,
			Gas:      uint64(21000),
			To:       &txClient.accountAddress,
			Value:    randomValue(),
		})
	} else {
		dynamicTx := &ethtypes.DynamicFeeTx{
			ChainID:   txClient.chainId,
			Nonce:     txClient.nextNonce(),
			GasTipCap: big.NewInt(2000000000), // 2 gwei, source: https://archive.ph/gOO0q#selection-1341.40-1341.164
			GasFeeCap: maxFee,
			Gas:       uint64(21000),
			To:        &txClient.accountAddress,
			Value:     randomValue(),
		}
		tx = ethtypes.NewTx(dynamicTx)
		fmt.Printf("generated dynamic tx for send funds, tx = %+v\n", dynamicTx)
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

func (txClient *EvmTxClient) getTransactOpts() *bind.TransactOpts {
	fmt.Println("In getTransactOpts")
	auth, err := bind.NewKeyedTransactorWithChainID(txClient.privateKey, txClient.chainId)
	if err != nil {
		panic(fmt.Sprintf("Failed to create transactor: %v \n", err))
	}

	maxFee, err := txClient.GetMaxFee(DefaultPriorityFee)
	if err != nil {
		fmt.Errorf("Failed to get max fee, err: %v, defaulting to legacy txs", err)
		auth.GasPrice = txClient.gasPrice
	} else {
		fmt.Println("Setting max fee to ", maxFee, " and priority fee to ", DefaultPriorityFee)
		auth.GasFeeCap = maxFee
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

// Max Fee = (2 * baseFee) + priorityFee
// source: https://archive.ph/gOO0q#selection-1499.0-1573.61
func (txClient *EvmTxClient) GetMaxFee(priorityFee *big.Int) (*big.Int, error) {
	txClient.baseFeeMu.RLock()
	defer txClient.baseFeeMu.RUnlock()
	if txClient.curBaseFee == nil {
		return nil, fmt.Errorf("base fee not available")
	}
	return new(big.Int).Add(new(big.Int).Mul(big.NewInt(2), txClient.curBaseFee), priorityFee), nil
}

func (txClient *EvmTxClient) PollBaseFee() (*big.Int, error) {
	defer txClient.baseFeePollerWg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-txClient.baseFeePollerStop:
			fmt.Println("Base fee poller stopped")
		case <-ticker.C:
			gasPrice, err := GetNextEthClient(txClient.ethClients).SuggestGasPrice(context.Background())
			if err != nil {
				fmt.Println("Failed to get gas price", err)
				continue
			}
			fmt.Println("Pulled gas price of ", gasPrice)
			// // pull most recent base fee
			// block, err := GetNextEthClient(txClient.ethClients).BlockByNumber(context.Background(), nil)
			// if err != nil {
			// 	fmt.Println("Failed to get block", err)
			// 	continue
			// }
			// fmt.Println("Pulled a base fee of ", block.BaseFee())
			txClient.baseFeeMu.Lock()
			// fmt.Println("Pulled a base fee of ", block.BaseFee())
			// // take the max of 1gwei and the base fee
			// if txClient.curBaseFee == nil || txClient.curBaseFee.Cmp(big.NewInt(1000000000)) == -1 {
			// 	txClient.curBaseFee = big.NewInt(1000000000)
			// }
			txClient.curBaseFee = gasPrice
			fmt.Println("Set base fee to ", txClient.curBaseFee)
			txClient.baseFeeMu.Unlock()
		}
	}
}

func (txClient *EvmTxClient) sign(tx *ethtypes.Transaction) *ethtypes.Transaction {
	fmt.Println("Signing tx with with address = ", txClient.accountAddress.String())
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
		// We choose not to GetTxReceipt because we assume the EVM RPC would be running with broadcast mode = block
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
	rand.Seed(time.Now().Unix())
	return clients[rand.Int()%numClients]
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
