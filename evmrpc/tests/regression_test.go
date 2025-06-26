package tests

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func Test0xf15bb88570910ae06a479a6e052bbadf23bcc8eaae1239025252d4b1afc8ea18(t *testing.T) {
	testTx(t,
		"0xf15bb88570910ae06a479a6e052bbadf23bcc8eaae1239025252d4b1afc8ea18",
		"0x8e2a8",
		"0x7fd13972",
		true,
	)
}

func Test0x01aaf9dd754d86f887c0c530cdd769da9202dfc1eb4a3e229050b417a4dc1adf(t *testing.T) {
	testTx(t,
		"0x01aaf9dd754d86f887c0c530cdd769da9202dfc1eb4a3e229050b417a4dc1adf",
		"0x950f2",
		"0x7b52656164466c61747d",
		true,
	)
}

func Test0x5174c3eae7c0d87dca905c42a0f1241c2d3834c1421594ad8f067e0f486808a8(t *testing.T) {
	testTx(t,
		"0x5174c3eae7c0d87dca905c42a0f1241c2d3834c1421594ad8f067e0f486808a8",
		"0x5208",
		"",
		false,
	)
}

func Test0xec0824ff522a4582f2fdbff355262afd5c7c2558fcb3aa5bcb7fb40b67418f97(t *testing.T) {
	testTx(t,
		"0xec0824ff522a4582f2fdbff355262afd5c7c2558fcb3aa5bcb7fb40b67418f97",
		"0x493e0",
		"",
		true,
	)
}

func Test0x4d9601c920c212e3c574a9362a562a7b399edbc125bd398850ea7848a50fee57(t *testing.T) {
	testTx(t,
		"0x4d9601c920c212e3c574a9362a562a7b399edbc125bd398850ea7848a50fee57",
		"0x6acfc0",
		"",
		true,
	)
}

func Test0x9c12fd7b7f1a7025b9ce2b65a10ebc1291249028ff13c7f292c304ef5dd4c8a0(t *testing.T) {
	testTx(t,
		"0x9c12fd7b7f1a7025b9ce2b65a10ebc1291249028ff13c7f292c304ef5dd4c8a0",
		"0x3b3a2",
		"",
		true,
	)
}

func Test0xb9ed69e95110e37fa4656be1cebe4049a36ff22d183acaf6829728ee8e2823d1(t *testing.T) {
	testTx(t,
		"0xb9ed69e95110e37fa4656be1cebe4049a36ff22d183acaf6829728ee8e2823d1",
		"0x30e21",
		"",
		true,
	)
}

func Test0x99d895ea71e5ce3a8b949ba7979a27c08080210a4ba9b46b0bb06f8126b6957d(t *testing.T) {
	testTx(t,
		"0x99d895ea71e5ce3a8b949ba7979a27c08080210a4ba9b46b0bb06f8126b6957d",
		"0x6acfc0",
		"",
		true,
	)
}

func Test0x78b377a6459b9ad6a0f64a858ea7afe90dc00a7bba0f0535758572ba1fe59e26(t *testing.T) {
	testTx(t,
		"0x78b377a6459b9ad6a0f64a858ea7afe90dc00a7bba0f0535758572ba1fe59e26",
		"0x18aaf",
		"0x000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000004563918244f40000000000000000000000000000000000000000000000000e9fc7a6844505c1bc07",
		false,
	)
}

func Test0x22ad57e8e59cc0f60c02bd3eb605eb570dcdc75168b136d576074591bfb7f105(t *testing.T) {
	testTx(t,
		"0x22ad57e8e59cc0f60c02bd3eb605eb570dcdc75168b136d576074591bfb7f105",
		"0x46071",
		"0x4e487b710000000000000000000000000000000000000000000000000000000000000011",
		true,
	)
}

func Test0x8efa322f7c17776fb839dfa882b4ee1f1605c6dc2108d563591bb3a099873506(t *testing.T) {
	testTx(t,
		"0x8efa322f7c17776fb839dfa882b4ee1f1605c6dc2108d563591bb3a099873506",
		"0x7f3d",
		"0x08c379a0000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000024243000000000000000000000000000000000000000000000000000000000000",
		true,
	)
}

func Test0x362af85584493ed225bf592606c989796ba5f0484f5c80b989b8573f85517ec1(t *testing.T) {
	testTx(t,
		"0x362af85584493ed225bf592606c989796ba5f0484f5c80b989b8573f85517ec1",
		"0x8214a",
		"0x7fd13972",
		true,
	)
}

func testTx(t *testing.T, txHash string, expectedGasUsed string, expectedOutput string, hasErr bool) {
	s := SetupMockPacificTestServer(func(a *app.App, mc *MockClient) sdk.Context {
		ctx := a.RPCContextProvider(evmrpc.LatestCtxHeight)
		blockHeight := mockStatesFromJsonFile(ctx, txHash, a, mc)
		return ctx.WithBlockHeight(blockHeight)
	})
	s.Run(
		func(port int) {
			raw := sendRequestWithNamespace(
				"debug", port, "traceTransaction",
				common.HexToHash(txHash).Hex(),
				map[string]interface{}{
					"tracer": "callTracer",
				},
			)
			res := raw["result"].(map[string]interface{})
			if hasErr {
				require.Contains(t, res, "error")
			}
			require.Equal(t, expectedGasUsed, res["gasUsed"])
			if expectedOutput != "" {
				require.Equal(t, expectedOutput, res["output"])
			}
		},
	)
}
