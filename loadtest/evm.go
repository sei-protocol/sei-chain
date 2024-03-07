package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"

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
	"github.com/sei-protocol/sei-chain/loadtest/contracts/evm/bindings/univ2/univ2_router"
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
}

func NewEvmTxClient(
	key cryptotypes.PrivKey,
	chainId *big.Int,
	gasPrice *big.Int,
	ethClients []*ethclient.Client,
	evmAddresses *EVMAddresses,
) *EvmTxClient {
	txClient := &EvmTxClient{
		chainId:      chainId,
		gasPrice:     gasPrice,
		ethClients:   ethClients,
		mtx:          sync.RWMutex{},
		evmAddresses: evmAddresses,
	}
	// Set starting nonce
	fromAddress := GetEvmAddressFromKey(key)
	nextNonce, err := ethClients[0].PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		panic(err)
	}
	txClient.nonce.Store(nextNonce)
	txClient.accountAddress = fromAddress
	fmt.Println("In NewEvmTxClient, accountAddress: ", txClient.accountAddress.String())
	txClient.privateKey, err = crypto.GenerateKey()
	if err != nil {
		panic(err)
	}
	return txClient
}

func GetEvmAddressFromKey(key cryptotypes.PrivKey) common.Address {
	privKeyHex := hex.EncodeToString(key.Bytes())
	privateKey, err := crypto.HexToECDSA(privKeyHex)
	if err != nil {
		fmt.Printf("Failed to load private key: %v \n", err)
	}
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		panic("Cannot assert type: publicKey is not of type *ecdsa.PublicKey \n")
	}
	return crypto.PubkeyToAddress(*publicKeyECDSA)
}

func (txClient *EvmTxClient) GetTxForMsgType(msgType string) *ethtypes.Transaction {
	switch msgType {
	case EVM:
		return txClient.GenerateSendFundsTx()
	case ERC20:
		return txClient.GenerateERC20TransferTx()
	case UNIV2:
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
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    txClient.nextNonce(),
		GasPrice: txClient.gasPrice,
		Gas:      uint64(21000),
		To:       &txClient.accountAddress,
		Value:    randomValue(),
	})
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
	return tx
}

func (txClient *EvmTxClient) GenerateUniV2SwapTx() *ethtypes.Transaction {
	opts := txClient.getTransactOpts()
	opts.GasLimit = uint64(300000)
	univ2_pair, err := univ2_router.NewUniv2Router(txClient.evmAddresses.UniV2Router, GetNextEthClient(txClient.ethClients))
	if err != nil {
		panic(fmt.Sprintf("Failed to create UniV2Router contract: %v \n", err))
	}
	tx, err := univ2_pair.SwapExactTokensForTokens(
		opts,
		big.NewInt(100),
		big.NewInt(0),
		[]common.Address{txClient.evmAddresses.UniV2Token1, txClient.evmAddresses.UniV2Token2},
		txClient.accountAddress,
		big.NewInt(2009777555),
	)
	if err != nil {
		panic(fmt.Sprintf("Failed to create UniV2 swap: %v \n", err))
	}
	return tx
}

func (txClient *EvmTxClient) getTransactOpts() *bind.TransactOpts {
	fmt.Println("txClient: ", txClient)
	fmt.Println("txClient.privateKey: ", txClient.privateKey)
	fmt.Println("txClient.chainId: ", txClient.chainId)
	auth, err := bind.NewKeyedTransactorWithChainID(txClient.privateKey, txClient.chainId)
	if err != nil {
		panic(fmt.Sprintf("Failed to create transactor: %v \n", err))
	}
	auth.Nonce = big.NewInt(int64(txClient.nextNonce()))
	auth.Value = big.NewInt(0)
	auth.GasLimit = uint64(21000)
	auth.GasPrice = txClient.gasPrice
	auth.Context = context.Background()
	auth.From = txClient.accountAddress
	auth.NoSend = true
	return auth
}

func (txClient *EvmTxClient) sign(tx *ethtypes.Transaction) *ethtypes.Transaction {
	signedTx, err := ethtypes.SignTx(tx, ethtypes.NewEIP155Signer(txClient.chainId), txClient.privateKey)
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
	fmt.Println("In SendEvmTx, sent EVM tx, tx.hash: ", signedTx.Hash().Hex())
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
