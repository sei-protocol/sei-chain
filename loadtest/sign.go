package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	clienttx "github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/legacy"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	xauthsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

type AccountInfo struct {
	Address  string `json:"address"`
	Mnemonic string `json:"mnemonic"`
}

type SignerInfo struct {
	AccountNumber  uint64
	SequenceNumber uint64
	mutex          *sync.Mutex
}

func NewSignerInfo(accountNumber uint64, sequenceNumber uint64) *SignerInfo {
	return &SignerInfo{
		AccountNumber:  accountNumber,
		SequenceNumber: sequenceNumber,
		mutex:          &sync.Mutex{},
	}
}

func (si *SignerInfo) IncrementAccountNumber() {
	si.mutex.Lock()
	defer si.mutex.Unlock()
	si.AccountNumber++
}

type SignerClient struct {
	CachedAccountSeqNum *sync.Map
	CachedAccountKey    *sync.Map
	NodeURI             string
}

func NewSignerClient(nodeURI string) *SignerClient {
	return &SignerClient{
		CachedAccountSeqNum: &sync.Map{},
		CachedAccountKey:    &sync.Map{},
		NodeURI:             nodeURI,
	}
}

func (sc *SignerClient) GetTestAccountsKeys(maxAccounts int) []cryptotypes.PrivKey {
	userHomeDir, _ := os.UserHomeDir()
	files, _ := os.ReadDir(filepath.Join(userHomeDir, "test_accounts"))
	var testAccountsKeys = make([]cryptotypes.PrivKey, int(math.Min(float64(len(files)), float64(maxAccounts))))
	var wg sync.WaitGroup
	fmt.Printf("Loading accounts\n")
	for i, file := range files {
		if i >= maxAccounts {
			break
		}
		wg.Add(1)
		go func(i int, fileName string) {
			defer wg.Done()
			key := sc.GetKey(fmt.Sprint(i), "test", filepath.Join(userHomeDir, "test_accounts", fileName))
			testAccountsKeys[i] = key
		}(i, file.Name())
	}
	wg.Wait()
	fmt.Printf("Finished loading %d accounts \n", len(testAccountsKeys))

	return testAccountsKeys
}

func (sc *SignerClient) GetAdminAccountKeyPath() string {
	userHomeDir, _ := os.UserHomeDir()
	return filepath.Join(userHomeDir, ".sei", "config", "admin_key.json")
}

func (sc *SignerClient) GetAdminKey() cryptotypes.PrivKey {
	return sc.GetKey("admin", "os", sc.GetAdminAccountKeyPath())
}

func (sc *SignerClient) GetKey(accountID, backend, accountKeyFilePath string) cryptotypes.PrivKey {
	if val, ok := sc.CachedAccountKey.Load(accountID); ok {
		privKey := val.(cryptotypes.PrivKey)
		return privKey
	}
	userHomeDir, _ := os.UserHomeDir()
	jsonFile, err := os.Open(filepath.Clean(accountKeyFilePath))
	if err != nil {
		panic(err)
	}
	var accountInfo AccountInfo
	byteVal, err := io.ReadAll(jsonFile)
	if err != nil {
		panic(err)
	}
	if err := jsonFile.Close(); err != nil {
		panic(err)
	}
	if err := json.Unmarshal(byteVal, &accountInfo); err != nil {
		panic(err)
	}
	kr, _ := keyring.New(sdk.KeyringServiceName(), backend, filepath.Join(userHomeDir, ".sei"), os.Stdin)
	keyringAlgos, _ := kr.SupportedAlgorithms()
	algoStr := string(hd.Secp256k1Type)
	algo, _ := keyring.NewSigningAlgoFromString(algoStr, keyringAlgos)
	hdpath := hd.CreateHDPath(sdk.GetConfig().GetCoinType(), 0, 0).String()
	derivedPriv, _ := algo.Derive()(accountInfo.Mnemonic, "", hdpath)
	privKey := algo.Generate()(derivedPriv)

	// Cache this so we don't need to regenerate it
	sc.CachedAccountKey.Store(accountID, privKey)
	return privKey
}

func (sc *SignerClient) GetValKeys() ([]cryptotypes.PrivKey, error) {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("os.UserHomeDir: %w", err)
	}
	valKeysFilePath := filepath.Join(userHomeDir, "exported_keys")
	files, err := os.ReadDir(valKeysFilePath)
	if err != nil {
		return nil, fmt.Errorf("os.ReadDir(%s): %w", valKeysFilePath, err)
	}

	valKeys := make([]cryptotypes.PrivKey, 0, len(files))
	for _, fn := range files {
		if fn.IsDir() {
			continue
		}
		// we dont expect subdirectories, so we can just handle files
		valKeyFile := filepath.Join(valKeysFilePath, fn.Name())
		privKeyBz, err := os.ReadFile(filepath.Clean(valKeyFile))
		if err != nil {
			return nil, fmt.Errorf("os.ReadFile(%s): %w", valKeyFile, err)
		}

		privKeyBytes, algo, err := crypto.UnarmorDecryptPrivKey(string(privKeyBz), "12345678") //nolint:gosec // used for testing only
		if err != nil {
			return nil, fmt.Errorf("UnarmorDecryptPrivKey: %w", err)
		}

		if algo != string(hd.Secp256k1Type) {
			return nil, fmt.Errorf("unsupported validator key type: %s", algo)
		}
		privKey, err := legacy.PrivKeyFromBytes(privKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("PrivKeyFromBytes: %w", err)
		}

		valKeys = append(valKeys, privKey)
	}
	return valKeys, nil
}

func (sc *SignerClient) SignTx(chainID string, txBuilder *client.TxBuilder, privKey cryptotypes.PrivKey, seqDelta uint64) error {
	signerInfo := sc.GetAccountNumberSequenceNumber(privKey)
	accountNum := signerInfo.AccountNumber
	seqNum := signerInfo.SequenceNumber

	seqNum += seqDelta
	sigV2 := signing.SignatureV2{
		PubKey: privKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  TestConfig.TxConfig.SignModeHandler().DefaultMode(),
			Signature: nil,
		},
		Sequence: seqNum,
	}
	if err := (*txBuilder).SetSignatures(sigV2); err != nil {
		return fmt.Errorf("SetSignatures (placeholder): %w", err)
	}
	signerData := xauthsigning.SignerData{
		ChainID:       chainID,
		AccountNumber: accountNum,
		Sequence:      seqNum,
	}
	sigV2, err := clienttx.SignWithPrivKey(
		TestConfig.TxConfig.SignModeHandler().DefaultMode(),
		signerData,
		*txBuilder,
		privKey,
		TestConfig.TxConfig,
		seqNum,
	)
	if err != nil {
		return fmt.Errorf("signWithPrivKey: %w", err)
	}
	if err := (*txBuilder).SetSignatures(sigV2); err != nil {
		return fmt.Errorf("setSignatures (final): %w", err)
	}
	return nil
}

func (sc *SignerClient) GetAccountNumberSequenceNumber(privKey cryptotypes.PrivKey) SignerInfo {
	if val, ok := sc.CachedAccountSeqNum.Load(privKey); ok {
		signerinfo := val.(SignerInfo)
		return signerinfo
	}

	hexAccount := privKey.PubKey().Address()
	address, err := sdk.AccAddressFromHex(hexAccount.String())
	if err != nil {
		panic(err)
	}
	accountRetriever := authtypes.AccountRetriever{}
	cl, err := client.NewClientFromNode(sc.NodeURI)
	if err != nil {
		panic(err)
	}
	context := client.Context{}
	context = context.WithNodeURI(sc.NodeURI)
	context = context.WithClient(cl)
	context = context.WithInterfaceRegistry(TestConfig.InterfaceRegistry)
	userHomeDir, _ := os.UserHomeDir()
	kr, _ := keyring.New(sdk.KeyringServiceName(), "test", filepath.Join(userHomeDir, ".sei"), os.Stdin)
	context = context.WithKeyring(kr)
	account, seq, err := accountRetriever.GetAccountNumberSequence(context, address)
	if err != nil {
		time.Sleep(5 * time.Second)
		// retry once after 5 seconds
		account, seq, err = accountRetriever.GetAccountNumberSequence(context, address)
		if err != nil {
			panic(err)
		}
	}

	signerInfo := *NewSignerInfo(account, seq)
	sc.CachedAccountSeqNum.Store(privKey, signerInfo)
	return signerInfo
}
