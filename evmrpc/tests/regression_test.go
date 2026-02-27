package tests

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

func Test0xf15bb88570910ae06a479a6e052bbadf23bcc8eaae1239025252d4b1afc8ea18(t *testing.T) {
	testTx(t,
		"0xf15bb88570910ae06a479a6e052bbadf23bcc8eaae1239025252d4b1afc8ea18",
		"v6.0.5",
		"0x8e2a8",
		"0x7fd13972",
		true,
	)
}

func Test0x01aaf9dd754d86f887c0c530cdd769da9202dfc1eb4a3e229050b417a4dc1adf(t *testing.T) {
	testTx(t,
		"0x01aaf9dd754d86f887c0c530cdd769da9202dfc1eb4a3e229050b417a4dc1adf",
		"v6.0.0",
		"0x950f2",
		"0x7b52656164466c61747d",
		true,
	)
}

func Test0x5174c3eae7c0d87dca905c42a0f1241c2d3834c1421594ad8f067e0f486808a8(t *testing.T) {
	testTx(t,
		"0x5174c3eae7c0d87dca905c42a0f1241c2d3834c1421594ad8f067e0f486808a8",
		"v5.5.2",
		"0x5208",
		"",
		false,
	)
}

func Test0xec0824ff522a4582f2fdbff355262afd5c7c2558fcb3aa5bcb7fb40b67418f97(t *testing.T) {
	testTx(t,
		"0xec0824ff522a4582f2fdbff355262afd5c7c2558fcb3aa5bcb7fb40b67418f97",
		"v5.5.2",
		"0x493e0",
		"",
		true,
	)
}

func Test0x4d9601c920c212e3c574a9362a562a7b399edbc125bd398850ea7848a50fee57(t *testing.T) {
	testTx(t,
		"0x4d9601c920c212e3c574a9362a562a7b399edbc125bd398850ea7848a50fee57",
		"v5.5.2",
		"0x6acfc0",
		"",
		true,
	)
}

func Test0x9c12fd7b7f1a7025b9ce2b65a10ebc1291249028ff13c7f292c304ef5dd4c8a0(t *testing.T) {
	testTx(t,
		"0x9c12fd7b7f1a7025b9ce2b65a10ebc1291249028ff13c7f292c304ef5dd4c8a0",
		"v5.5.2",
		"0x3b3a2",
		"",
		true,
	)
}

func Test0xb9ed69e95110e37fa4656be1cebe4049a36ff22d183acaf6829728ee8e2823d1(t *testing.T) {
	testTx(t,
		"0xb9ed69e95110e37fa4656be1cebe4049a36ff22d183acaf6829728ee8e2823d1",
		"v5.5.2",
		"0x30e21",
		"",
		true,
	)
}

func Test0x99d895ea71e5ce3a8b949ba7979a27c08080210a4ba9b46b0bb06f8126b6957d(t *testing.T) {
	testTx(t,
		"0x99d895ea71e5ce3a8b949ba7979a27c08080210a4ba9b46b0bb06f8126b6957d",
		"v5.5.2",
		"0x6acfc0",
		"",
		true,
	)
}

func Test0x78b377a6459b9ad6a0f64a858ea7afe90dc00a7bba0f0535758572ba1fe59e26(t *testing.T) {
	testTx(t,
		"0x78b377a6459b9ad6a0f64a858ea7afe90dc00a7bba0f0535758572ba1fe59e26",
		"v5.5.2",
		"0x18aaf",
		"0x000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000004563918244f40000000000000000000000000000000000000000000000000e9fc7a6844505c1bc07",
		false,
	)
}

func Test0x22ad57e8e59cc0f60c02bd3eb605eb570dcdc75168b136d576074591bfb7f105(t *testing.T) {
	testTx(t,
		"0x22ad57e8e59cc0f60c02bd3eb605eb570dcdc75168b136d576074591bfb7f105",
		"v5.5.2",
		"0x46071",
		"0x4e487b710000000000000000000000000000000000000000000000000000000000000011",
		true,
	)
}

func Test0x8efa322f7c17776fb839dfa882b4ee1f1605c6dc2108d563591bb3a099873506(t *testing.T) {
	testTx(t,
		"0x8efa322f7c17776fb839dfa882b4ee1f1605c6dc2108d563591bb3a099873506",
		"v6.1.0",
		"0x7f3d",
		"0x08c379a0000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000024243000000000000000000000000000000000000000000000000000000000000",
		true,
	)
}

func Test0x362af85584493ed225bf592606c989796ba5f0484f5c80b989b8573f85517ec1(t *testing.T) {
	testTx(t,
		"0x362af85584493ed225bf592606c989796ba5f0484f5c80b989b8573f85517ec1",
		"v6.0.3",
		"0x8214a",
		"0x7fd13972",
		true,
	)
}

func Test0xd09db4e79993c42eda67b45ca2fd5ac1e4cc60284a03335a08bb91d5b3800d84(t *testing.T) {
	testTx(t,
		"0xd09db4e79993c42eda67b45ca2fd5ac1e4cc60284a03335a08bb91d5b3800d84",
		"v5.8.0",
		"0x2691cb",
		"0x00000000000000000000000028e7fa339ec0ef4f71febfa92d3964ebd41fce2c",
		false,
	)
}

func Test169638844(t *testing.T) {
	testBlock(
		t,
		169638844,
		"v6.1.4",
		"0xfb2fd",
	)
}

func Test169750823(t *testing.T) {
	testBlock(
		t,
		169750823,
		"v6.1.4",
		"0x29e240",
	)
}

func Test0x5bc4f251122bb01d6313916634dc9a20dcf4407aabda394ed5fc442d7224fb52(t *testing.T) {
	testTx(t,
		"0x5bc4f251122bb01d6313916634dc9a20dcf4407aabda394ed5fc442d7224fb52",
		"v6.1.0",
		"0x1da69",
		"",
		true,
	)
}

func Test0x8802ea697fd2ba69c429b54890f89d85f37a05995c4900aeb4da7f84a74e8d9e(t *testing.T) {
	testTx(t,
		"0x8802ea697fd2ba69c429b54890f89d85f37a05995c4900aeb4da7f84a74e8d9e",
		"v5.5.2",
		"0x6d14b",
		"0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000000",
		false,
	)
}

func Test0x778b844513ebd18292231bb9081e5448b2ede52214b17b4c25ddf15fdb8182fc(t *testing.T) {
	testTx(t,
		"0x778b844513ebd18292231bb9081e5448b2ede52214b17b4c25ddf15fdb8182fc",
		"v5.5.2",
		"0x6d28",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		false,
	)
}

func Test0x13790ab0421b6c6a56d15e67762941010f1a91e04b5e9736d1611b8ebb52974c(t *testing.T) {
	testTx(t,
		"0x13790ab0421b6c6a56d15e67762941010f1a91e04b5e9736d1611b8ebb52974c",
		"v5.5.5",
		"0x2138b",
		"0x00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000000",
		false,
	)
}

func Test0x57a22aa37c87e584bf9ccdb3da4dea159fbc6755e81ae8ae5ee863a934b87b7d(t *testing.T) {
	testTx(t,
		"0x57a22aa37c87e584bf9ccdb3da4dea159fbc6755e81ae8ae5ee863a934b87b7d",
		"v5.5.5",
		"0x6d28",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		false,
	)
}

func Test0xf3aac6d7df54ffc869e3e754b76135b032509e6d7ae4990adb51a21b611ef281(t *testing.T) {
	testTx(t,
		"0xf3aac6d7df54ffc869e3e754b76135b032509e6d7ae4990adb51a21b611ef281",
		"v5.6.2",
		"0x4b453",
		"0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000000",
		false,
	)
}

func Test0x29ba97054dd18474533d0cfa77c0a5188e3b5dd10759521846f116b0b0020f69(t *testing.T) {
	testTx(t,
		"0x29ba97054dd18474533d0cfa77c0a5188e3b5dd10759521846f116b0b0020f69",
		"v5.6.2",
		"0x6d28",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		false,
	)
}

func Test0xc2da776841226a9837bc3bb506fcf487e75a7e7787ab1084896251114d6bd7f9(t *testing.T) {
	testTx(t,
		"0xc2da776841226a9837bc3bb506fcf487e75a7e7787ab1084896251114d6bd7f9",
		"v5.7.5",
		"0x9226e",
		"0x00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000000",
		false,
	)
}

func Test0x077d674cc32479b006c41155210afc57e6d389bd8e0509fd2468e69ce03823a6(t *testing.T) {
	testTx(t,
		"0x077d674cc32479b006c41155210afc57e6d389bd8e0509fd2468e69ce03823a6",
		"v5.7.5",
		"0x6d28",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		false,
	)
}

func Test0x29372ab96fa5530c164e62b6c51f46ad660eb28cf84c1d77d233819f9deb5187(t *testing.T) {
	testTx(t,
		"0x29372ab96fa5530c164e62b6c51f46ad660eb28cf84c1d77d233819f9deb5187",
		"v5.8.0",
		"0x90dde",
		"0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000000",
		false,
	)
}

func Test0xeadd6230c2d8209d660c3bd36599e87cbd189392ddf775b04b6ae284d64a3fef(t *testing.T) {
	testTx(t,
		"0xeadd6230c2d8209d660c3bd36599e87cbd189392ddf775b04b6ae284d64a3fef",
		"v5.8.0",
		"0x6d28",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		false,
	)
}

func Test0x4b454a53a0c8757c2f4fc18881652931e9b35deaeb386432f893ac7718f4f140(t *testing.T) {
	testTx(t,
		"0x4b454a53a0c8757c2f4fc18881652931e9b35deaeb386432f893ac7718f4f140",
		"v6.0.0",
		"0x34665",
		"0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000000",
		false,
	)
}

func Test0xfdd190eac0ec35a8d52149d1b056ab07221fae6418b1592d12004e773adc83f6(t *testing.T) {
	testTx(t,
		"0xfdd190eac0ec35a8d52149d1b056ab07221fae6418b1592d12004e773adc83f6",
		"v6.0.0",
		"0xba5f",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		false,
	)
}

func Test0xd30881600c458683d6140ac1b52a6f626f56481b1e157b6b9516049e8bd9a99a(t *testing.T) {
	testTx(t,
		"0xd30881600c458683d6140ac1b52a6f626f56481b1e157b6b9516049e8bd9a99a",
		"v6.0.1",
		"0x345a5",
		"0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000000",
		false,
	)
}

func Test0x1b9ceaabadfc635aa8eb5e6d4a66ee60c826980805fa93af3913872f7b565586(t *testing.T) {
	testTx(t,
		"0x1b9ceaabadfc635aa8eb5e6d4a66ee60c826980805fa93af3913872f7b565586",
		"v6.0.1",
		"0xb8d9",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		false,
	)
}

func Test0x29b9a9764a27ba65139ccf06ef51e18358a63dfe143bfa6a16a891127cfc8ab6(t *testing.T) {
	testTx(t,
		"0x29b9a9764a27ba65139ccf06ef51e18358a63dfe143bfa6a16a891127cfc8ab6",
		"v6.0.2",
		"0x91fcd",
		"0x00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000000",
		false,
	)
}

func Test0xb8bce587144c674e7c12cbfc69266be7436dbbfdf6d7ac186582c0735954cc37(t *testing.T) {
	testTx(t,
		"0xb8bce587144c674e7c12cbfc69266be7436dbbfdf6d7ac186582c0735954cc37",
		"v6.0.2",
		"0xb88e",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		false,
	)
}

func Test0x534a7d8dac27858aff75623792fee542dd7cec2539c2ddac92749e85bdc08e84(t *testing.T) {
	testTx(t,
		"0x534a7d8dac27858aff75623792fee542dd7cec2539c2ddac92749e85bdc08e84",
		"v6.0.3",
		"0x9180a",
		"0x00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000000",
		false,
	)
}

func Test0x2cbbf34f930076000024953b87da7dc119a04f71fc4734a4bfabbe60558a49c6(t *testing.T) {
	testTx(t,
		"0x2cbbf34f930076000024953b87da7dc119a04f71fc4734a4bfabbe60558a49c6",
		"v6.0.3",
		"0xba5f",
		"0x0000000000000000000000000000000000000000000000000000000000000001",
		false,
	)
}

func testTx(t *testing.T, txHash string, version string, expectedGasUsed string, expectedOutput string, hasErr bool) {
	s := SetupMockPacificTestServer(t, func(a *app.App, mc *MockClient) sdk.Context {
		ctx := a.RPCContextProvider(evmrpc.LatestCtxHeight).WithClosestUpgradeName(version)
		ctx = setLegacySstoreIfNeeded(ctx, a, version)
		blockHeight := mockStatesFromTxJson(ctx, txHash, a, mc)
		ctx = setLegacySstoreIfNeeded(ctx, a, version)
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
			res, ok := raw["result"].(map[string]interface{})
			if !ok {
				t.Logf("raw: %v", raw)
				require.Fail(t, "raw could not be converted to map[string]interface{}")
			}
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

func testBlock(
	t *testing.T, blockNumber uint64, version string, expectedGasUsed string,
) {
	s := SetupMockPacificTestServer(
		t,
		func(a *app.App, mc *MockClient) sdk.Context {
			ctx := a.RPCContextProvider(evmrpc.LatestCtxHeight).WithClosestUpgradeName(version)
			ctx = setLegacySstoreIfNeeded(ctx, a, version)
			blockHeight := mockStatesFromBlockJson(
				ctx, blockNumber, a, mc,
			)
			ctx = setLegacySstoreIfNeeded(ctx, a, version)
			return ctx.WithBlockHeight(blockHeight)
		},
	)
	s.Run(
		func(port int) {
			raw := sendRequestWithNamespace(
				"eth", port, "getBlockByNumber",
				hexutil.EncodeUint64(blockNumber),
				false,
			)
			res := raw["result"].(map[string]interface{})
			require.Equal(t, expectedGasUsed, res["gasUsed"])
		},
	)
}

const legacySstoreGas = uint64(20000)

func setLegacySstoreIfNeeded(ctx sdk.Context, a *app.App, version string) sdk.Context {
	if !isVersionLessOrEqual(version, "v6.2.0") {
		return ctx
	}
	params := a.EvmKeeper.GetParams(ctx)
	if params.SeiSstoreSetGasEip2200 == legacySstoreGas {
		return ctx
	}
	params.SeiSstoreSetGasEip2200 = legacySstoreGas
	params.RegisterPointerDisabled = false
	a.EvmKeeper.SetParams(ctx, params)
	return ctx
}

func isVersionLessOrEqual(version, target string) bool {
	// Remove 'v' prefix if present
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}
	if len(target) > 0 && target[0] == 'v' {
		target = target[1:]
	}

	// Split version strings into parts
	versionParts := strings.Split(version, ".")
	targetParts := strings.Split(target, ".")

	// Pad shorter version with zeros
	maxLen := len(versionParts)
	if len(targetParts) > maxLen {
		maxLen = len(targetParts)
	}

	for len(versionParts) < maxLen {
		versionParts = append(versionParts, "0")
	}
	for len(targetParts) < maxLen {
		targetParts = append(targetParts, "0")
	}

	// Compare each part
	for i := 0; i < maxLen; i++ {
		vPart, err1 := strconv.Atoi(versionParts[i])
		tPart, err2 := strconv.Atoi(targetParts[i])

		if err1 != nil || err2 != nil {
			// If parsing fails, fall back to string comparison
			if versionParts[i] < targetParts[i] {
				return true
			} else if versionParts[i] > targetParts[i] {
				return false
			}
			continue
		}

		if vPart < tPart {
			return true
		} else if vPart > tPart {
			return false
		}
	}

	return true // versions are equal
}
