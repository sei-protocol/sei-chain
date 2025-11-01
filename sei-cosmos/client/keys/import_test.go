package keys

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func Test_runImportCmd(t *testing.T) {
	testCases := []struct {
		name           string
		keyringBackend string
		userInput      string
		expectError    bool
	}{
		{
			name:           "test backend success",
			keyringBackend: keyring.BackendTest,
			// key armor passphrase
			userInput: "123456789\n",
		},
		{
			name:           "test backend fail with wrong armor pass",
			keyringBackend: keyring.BackendTest,
			userInput:      "987654321\n",
			expectError:    true,
		},
		{
			name:           "file backend success",
			keyringBackend: keyring.BackendFile,
			// key armor passphrase + keyring password x2
			userInput: "123456789\n12345678\n12345678\n",
		},
		{
			name:           "file backend fail with wrong armor pass",
			keyringBackend: keyring.BackendFile,
			userInput:      "987654321\n12345678\n12345678\n",
			expectError:    true,
		},
		{
			name:           "file backend fail with wrong keyring pass",
			keyringBackend: keyring.BackendFile,
			userInput:      "123465789\n12345678\n87654321\n",
			expectError:    true,
		},
		{
			name:           "file backend fail with no keyring pass",
			keyringBackend: keyring.BackendFile,
			userInput:      "123465789\n",
			expectError:    true,
		},
	}

	armoredKey := `-----BEGIN TENDERMINT PRIVATE KEY-----
kdf: bcrypt
salt: 0A2203634D31B7BAFD938564E19A173A
type: secp256k1

OqpHBjYDPl5/jfm3LqzatY1Re19JvYvZ5fCAKGj/VUk1PZFj0bDjdYfr7CLDqduS
mBtElE9j21pFaLf9gt8hVPTMDXWPtbIL
=ctQV
-----END TENDERMINT PRIVATE KEY-----
`

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := ImportKeyCommand()
			cmd.Flags().AddFlagSet(Commands("home").PersistentFlags())
			mockIn := testutil.ApplyMockIODiscardOutErr(cmd)

			// Now add a temporary keybase
			kbHome := t.TempDir()
			kb, err := keyring.New(sdk.KeyringServiceName(), tc.keyringBackend, kbHome, nil)

			clientCtx := client.Context{}.
				WithKeyringDir(kbHome).
				WithKeyring(kb).
				WithInput(mockIn)
			ctx := context.WithValue(context.Background(), client.ClientContextKey, &clientCtx)

			require.NoError(t, err)
			t.Cleanup(func() {
				kb.Delete("keyname1") // nolint:errcheck
			})

			keyfile := filepath.Join(kbHome, "key.asc")

			require.NoError(t, ioutil.WriteFile(keyfile, []byte(armoredKey), 0644))

			defer func() {
				_ = os.RemoveAll(kbHome)
			}()

			mockIn.Reset(tc.userInput)
			cmd.SetArgs([]string{
				"keyname1", keyfile,
				fmt.Sprintf("--%s=%s", flags.FlagKeyringBackend, tc.keyringBackend),
			})

			err = cmd.ExecuteContext(ctx)
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
