package tracers

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	pbeth "github.com/sei-protocol/sei-chain/pb/sf/ethereum/type/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestFirehoseCallStack_Push(t *testing.T) {
	type actionRunner func(t *testing.T, s *CallStack)

	push := func(call *pbeth.Call) actionRunner { return func(_ *testing.T, s *CallStack) { s.Push(call) } }
	pop := func() actionRunner { return func(_ *testing.T, s *CallStack) { s.Pop() } }
	check := func(r actionRunner) actionRunner { return func(t *testing.T, s *CallStack) { r(t, s) } }

	tests := []struct {
		name    string
		actions []actionRunner
	}{
		{
			"push/pop emtpy", []actionRunner{
				push(&pbeth.Call{}),
				pop(),
				check(func(t *testing.T, s *CallStack) {
					require.Len(t, s.stack, 0)
				}),
			},
		},
		{
			"push/push/push", []actionRunner{
				push(&pbeth.Call{}),
				push(&pbeth.Call{}),
				push(&pbeth.Call{}),
				check(func(t *testing.T, s *CallStack) {
					require.Len(t, s.stack, 3)

					require.Equal(t, 1, int(s.stack[0].Index))
					require.Equal(t, 0, int(s.stack[0].ParentIndex))

					require.Equal(t, 2, int(s.stack[1].Index))
					require.Equal(t, 1, int(s.stack[1].ParentIndex))

					require.Equal(t, 3, int(s.stack[2].Index))
					require.Equal(t, 2, int(s.stack[2].ParentIndex))
				}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewCallStack()

			for _, action := range tt.actions {
				action(t, s)
			}
		})
	}
}

func Test_validateKnownTransactionTypes(t *testing.T) {
	tests := []struct {
		name      string
		txType    byte
		knownType bool
		want      error
	}{
		{"legacy", 0, true, nil},
		{"access_list", 1, true, nil},
		{"inexistant", 255, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFirehoseKnownTransactionType(tt.txType, tt.knownType)
			if tt.want == nil && err != nil {
				t.Fatalf("Transaction of type %d expected to validate properly but received error %q", tt.txType, err)
			} else if tt.want != nil && err == nil {
				t.Fatalf("Transaction of type %d expected to validate improperly but generated no error", tt.txType)
			} else if tt.want != nil && err != nil && tt.want.Error() != err.Error() {
				t.Fatalf("Transaction of type %d expected to validate improperly but generated error %q does not match expected error %q", tt.txType, err, tt.want)
			}
		})
	}
}

var ignorePbFieldNames = map[string]bool{
	"Hash":            true,
	"TotalDifficulty": true,
	"state":           true,
	"unknownFields":   true,
	"sizeCache":       true,

	// This was a Polygon specific field that existed for a while and has since been
	// removed. It can be safely ignored in all protocols now.
	"TxDependency": true,
}

var pbFieldNameToGethMapping = map[string]string{
	"WithdrawalsRoot":  "WithdrawalsHash",
	"MixHash":          "MixDigest",
	"BaseFeePerGas":    "BaseFee",
	"StateRoot":        "Root",
	"ExtraData":        "Extra",
	"Timestamp":        "Time",
	"ReceiptRoot":      "ReceiptHash",
	"TransactionsRoot": "TxHash",
	"LogsBloom":        "Bloom",
}

var (
	pbHeaderType   = reflect.TypeOf((*pbeth.BlockHeader)(nil)).Elem()
	gethHeaderType = reflect.TypeOf((*types.Header)(nil)).Elem()
)

func Test_TypesHeader_AllConsensusFieldsAreKnown(t *testing.T) {
	// This exact hash varies from protocol to protocol and also sometimes from one version to the other.
	// When adding support for a new hard-fork that adds new block header fields, it's normal that this value
	// changes. If you are sure the two struct are the same, then you can update the expected hash below
	// to the new value.
	expectedHash := common.HexToHash("5341947c531e5c9cf38202784b16ac66484fe1838aa6e825436b22321b927296")

	gethHeaderValue := reflect.New(gethHeaderType)
	fillAllFieldsWithNonEmptyValues(t, gethHeaderValue, reflect.VisibleFields(gethHeaderType))
	gethHeader := gethHeaderValue.Interface().(*types.Header)

	// If you hit this assertion, it means that the fields `types.Header` of go-ethereum differs now
	// versus last time this test was edited.
	//
	// It's important to understand that in Ethereum Block Header (e.g. `*types.Header`), the `Hash` is
	// actually a computed value based on the other fields in the struct, so if you change any field,
	// the hash will change also.
	//
	// On hard-fork, it happens that new fields are added, this test serves as a way to "detect" in codde
	// that the expected fields of `types.Header` changed
	require.Equal(t, expectedHash, gethHeader.Hash(),
		"Geth Header Hash mistmatch, got %q but expecting %q on *types.Header:\n\nGeth Header (from fillNonDefault(new(*types.Header)))\n%s",
		gethHeader.Hash().Hex(),
		expectedHash,
		asIndentedJSON(t, gethHeader),
	)
}

func Test_FirehoseAndGethHeaderFieldMatches(t *testing.T) {
	pbFields := filter(reflect.VisibleFields(pbHeaderType), func(f reflect.StructField) bool {
		return !ignorePbFieldNames[f.Name]
	})

	gethFields := reflect.VisibleFields(gethHeaderType)

	pbFieldCount := len(pbFields)
	gethFieldCount := len(gethFields)

	pbFieldNames := extractStructFieldNames(pbFields)
	gethFieldNames := extractStructFieldNames(gethFields)

	// If you reach this assertion, it means that the fields count in the protobuf and go-ethereum are different.
	// It is super important that you properly update the mapping from pbeth.BlockHeader to go-ethereum/core/types.Header
	// that is done in `codecHeaderToGethHeader` function in `executor/provider_statedb.go`.
	require.Equal(
		t,
		pbFieldCount,
		gethFieldCount,
		fieldsCountMistmatchMessage(t, pbFieldNames, gethFieldNames))

	for pbFieldName := range pbFieldNames {
		pbFieldRenamedName, found := pbFieldNameToGethMapping[pbFieldName]
		if !found {
			pbFieldRenamedName = pbFieldName
		}

		assert.Contains(t, gethFieldNames, pbFieldRenamedName, "pbField.Name=%q (original %q) not found in gethFieldNames", pbFieldRenamedName, pbFieldName)
	}
}

func fillAllFieldsWithNonEmptyValues(t *testing.T, structValue reflect.Value, fields []reflect.StructField) {
	t.Helper()

	for _, field := range fields {
		fieldValue := structValue.Elem().FieldByName(field.Name)
		require.True(t, fieldValue.IsValid(), "field %q not found", field.Name)

		switch fieldValue.Interface().(type) {
		case []byte:
			fieldValue.Set(reflect.ValueOf([]byte{1}))
		case uint64:
			fieldValue.Set(reflect.ValueOf(uint64(1)))
		case *uint64:
			var mockValue uint64 = 1
			fieldValue.Set(reflect.ValueOf(&mockValue))
		case *common.Hash:
			var mockValue common.Hash = common.HexToHash("0x01")
			fieldValue.Set(reflect.ValueOf(&mockValue))
		case common.Hash:
			fieldValue.Set(reflect.ValueOf(common.HexToHash("0x01")))
		case common.Address:
			fieldValue.Set(reflect.ValueOf(common.HexToAddress("0x01")))
		case types.Bloom:
			fieldValue.Set(reflect.ValueOf(types.BytesToBloom([]byte{1})))
		case types.BlockNonce:
			fieldValue.Set(reflect.ValueOf(types.EncodeNonce(1)))
		case *big.Int:
			fieldValue.Set(reflect.ValueOf(big.NewInt(1)))
		case *pbeth.BigInt:
			fieldValue.Set(reflect.ValueOf(&pbeth.BigInt{Bytes: []byte{1}}))
		case *timestamppb.Timestamp:
			fieldValue.Set(reflect.ValueOf(&timestamppb.Timestamp{Seconds: 1}))
		default:
			// If you reach this panic in test, simply add a case above with a sane non-default
			// value for the type in question.
			t.Fatalf("unsupported type %T", fieldValue.Interface())
		}
	}
}

func fieldsCountMistmatchMessage(t *testing.T, pbFieldNames map[string]bool, gethFieldNames map[string]bool) string {
	t.Helper()

	pbRemappedFieldNames := make(map[string]bool, len(pbFieldNames))
	for pbFieldName := range pbFieldNames {
		pbFieldRenamedName, found := pbFieldNameToGethMapping[pbFieldName]
		if !found {
			pbFieldRenamedName = pbFieldName
		}

		pbRemappedFieldNames[pbFieldRenamedName] = true
	}

	return fmt.Sprintf(
		"Field count mistmatch between `pbeth.BlockHeader` (has %d fields) and `*types.Header` (has %d fields)\n\n"+
			"Fields in `pbeth.Blockheader`:\n%s\n\n"+
			"Fields in `*types.Header`:\n%s\n\n"+
			"Missing in `pbeth.BlockHeader`:\n%s\n\n"+
			"Missing in `*types.Header`:\n%s",
		len(pbRemappedFieldNames),
		len(gethFieldNames),
		asIndentedJSON(t, maps.Keys(pbRemappedFieldNames)),
		asIndentedJSON(t, maps.Keys(gethFieldNames)),
		asIndentedJSON(t, missingInSet(gethFieldNames, pbRemappedFieldNames)),
		asIndentedJSON(t, missingInSet(pbRemappedFieldNames, gethFieldNames)),
	)
}

func asIndentedJSON(t *testing.T, v any) string {
	t.Helper()
	out, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)

	return string(out)
}

func missingInSet(a, b map[string]bool) []string {
	missing := make([]string, 0)
	for name := range a {
		if !b[name] {
			missing = append(missing, name)
		}
	}

	return missing
}

func extractStructFieldNames(fields []reflect.StructField) map[string]bool {
	result := make(map[string]bool, len(fields))
	for _, field := range fields {
		result[field.Name] = true
	}
	return result
}

func filter[S ~[]T, T any](s S, f func(T) bool) (out S) {
	out = make(S, 0, len(s)/4)
	for i, v := range s {
		if f(v) {
			out = append(out, s[i])
		}
	}

	return out
}

func TestFirehose_reorderIsolatedTransactionsAndOrdinals(t *testing.T) {
	addCall := func(tracer *Firehose, returnData []byte, oldBalance, newBalance int64) {
		tracer.OnCallEnter(0, byte(vm.CALL), from, to, nil, 0, nil)
		tracer.OnBalanceChange(empty, b(oldBalance), b(newBalance), tracing.BalanceChangeTransfer)
		tracer.OnCallExit(0, returnData, 0, nil, false)
	}
	addTransaction := func(tracer *Firehose, index uint, hashHex string, returnData []byte, oldBalance, newBalance int64) {
		tracer.onTxStart(txEvent(), hex2Hash(hashHex), from, to)
		addCall(tracer, returnData, oldBalance, newBalance)
		tracer.OnTxEnd(txReceiptEvent(index), nil)
	}
	addSystemCall := func(tracer *Firehose, returnData []byte, oldBalance, newBalance int64) {
		tracer.OnSystemCallStart()
		addCall(tracer, returnData, oldBalance, newBalance)
		tracer.OnSystemCallEnd()
	}

	tests := []struct {
		name              string
		populate          func(t *Firehose)
		expectedBlockFile string
	}{
		{
			name: "transaction only",
			populate: func(tracer *Firehose) {
				tracer.OnBlockStart(blockEvent(1))

				ttCC := tracer.newIsolatedTransactionTracer("CC")
				addTransaction(ttCC, 2, "CC", nil, 1, 2)

				ttAA := tracer.newIsolatedTransactionTracer("AA")
				addTransaction(ttAA, 0, "AA", nil, 1, 2)

				ttBB := tracer.newIsolatedTransactionTracer("BB")
				addTransaction(ttBB, 1, "BB", nil, 1, 2)

				tracer.addIsolatedTransaction(ttAA.transientTransaction)
				tracer.addIsolatedTransaction(ttBB.transientTransaction)
				tracer.addIsolatedTransaction(ttCC.transientTransaction)

				// No OnBlockEnd, it would reset the current state
			},
			expectedBlockFile: "testdata/firehose/reorder-ordinals-transaction-only.golden.json",
		},
		{
			name: "system calls only",
			populate: func(tracer *Firehose) {
				tracer.OnBlockStart(blockEvent(1))

				// Simulate call before executing transactions
				addSystemCall(tracer, hex2Bytes("FF"), 1, 2)

				ttCC := tracer.newIsolatedTransactionTracer("CC")
				addSystemCall(ttCC, hex2Bytes("CC"), 1, 2)

				ttAA := tracer.newIsolatedTransactionTracer("AA")
				addSystemCall(ttAA, hex2Bytes("AA"), 1, 2)

				ttBB := tracer.newIsolatedTransactionTracer("BB")
				addSystemCall(ttBB, hex2Bytes("BB"), 1, 2)

				tracer.addIsolatedSystemCalls(ttAA.transientSystemCalls)
				tracer.addIsolatedSystemCalls(ttBB.transientSystemCalls)
				tracer.addIsolatedSystemCalls(ttCC.transientSystemCalls)

				// Simulate call after executing transactions
				addSystemCall(tracer, hex2Bytes("EE"), 1, 2)

				// Block level balance change
				tracer.OnBalanceChange(empty, b(4), b(5), tracing.BalanceIncreaseRewardMineBlock)

				// No OnBlockEnd, it would reset the current state
			},
			expectedBlockFile: "testdata/firehose/reorder-ordinals-system-calls-only.golden.json",
		},
		{
			name: "mixed full",
			populate: func(tracer *Firehose) {
				tracer.OnBlockStart(blockEvent(1))

				// Simulate call before executing transactions
				addSystemCall(tracer, hex2Bytes("FF"), 1, 2)

				ttCC := tracer.newIsolatedTransactionTracer("CC")
				addSystemCall(ttCC, hex2Bytes("CC"), 1, 2)

				ttDD := tracer.newIsolatedTransactionTracer("DD")
				addTransaction(ttDD, 1, "DD", nil, 1, 2)

				ttAA := tracer.newIsolatedTransactionTracer("AA")
				addTransaction(ttAA, 0, "AA", nil, 1, 2)

				ttBB := tracer.newIsolatedTransactionTracer("BB")
				addSystemCall(ttBB, hex2Bytes("BB01"), 1, 2)
				addSystemCall(ttBB, hex2Bytes("BB02"), 1, 2)

				tracer.addIsolatedTransaction(ttAA.transientTransaction)
				tracer.addIsolatedSystemCalls(ttBB.transientSystemCalls)
				tracer.addIsolatedSystemCalls(ttCC.transientSystemCalls)
				tracer.addIsolatedTransaction(ttDD.transientTransaction)

				// Simulate call after executing transactions
				addSystemCall(tracer, hex2Bytes("EE"), 1, 2)

				// Block level balance change
				tracer.OnBalanceChange(empty, b(4), b(5), tracing.BalanceIncreaseRewardMineBlock)

				// No OnBlockEnd, it would reset the current state
			},
			expectedBlockFile: "testdata/firehose/reorder-ordinals-mixed-full.golden.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewFirehose(&FirehoseConfig{
				ApplyBackwardCompatibility: ptr(false),
			})
			f.OnBlockchainInit(params.AllEthashProtocolChanges)

			tt.populate(f)

			goldenUpdate := os.Getenv("GOLDEN_UPDATE") == "true"
			goldenPath := tt.expectedBlockFile

			if !goldenUpdate && !fileExits(t, goldenPath) {
				t.Fatalf("the golden file %q does not exist, re-run with 'GOLDEN_UPDATE=true go test ./... -run %q' to generate the initial version", goldenPath, t.Name())
			}

			content, err := protojson.MarshalOptions{Indent: "  "}.Marshal(f.block)
			require.NoError(t, err)

			if goldenUpdate {
				require.NoError(t, os.WriteFile(goldenPath, content, os.ModePerm))
			}

			expected, err := os.ReadFile(goldenPath)
			require.NoError(t, err)

			expectedBlock := &pbeth.Block{}
			protojson.Unmarshal(expected, expectedBlock)

			if !proto.Equal(expectedBlock, f.block) {
				assert.Equal(t, expectedBlock, f.block, "Run 'GOLDEN_UPDATE=true go test ./... -run %q' to update golden file", t.Name())
			}

			seenOrdinals := make(map[uint64]int)

			walkChanges(f.block.BalanceChanges, seenOrdinals)
			walkChanges(f.block.CodeChanges, seenOrdinals)
			walkCalls(f.block.SystemCalls, seenOrdinals)

			for _, trx := range f.block.TransactionTraces {
				seenOrdinals[trx.BeginOrdinal] = seenOrdinals[trx.BeginOrdinal] + 1
				seenOrdinals[trx.EndOrdinal] = seenOrdinals[trx.EndOrdinal] + 1
				walkCalls(trx.Calls, seenOrdinals)
			}

			// No ordinal should be seen more than once
			for ordinal, count := range seenOrdinals {
				assert.Equal(t, 1, count, "Ordinal %d seen %d times", ordinal, count)
			}

			ordinals := maps.Keys(seenOrdinals)
			slices.Sort(ordinals)

			// All ordinals should be in strictly increasing order
			prev := -1
			for _, ordinal := range ordinals {
				if prev != -1 {
					assert.Equal(t, prev+1, int(ordinal), "Ordinal %d is not in sequence, we jumped from %d to %d, expected %d to %d", ordinal, prev, ordinal, prev, prev+1)
				}

				prev = int(ordinal)
			}
		})
	}
}

func walkCalls(calls []*pbeth.Call, ordinals map[uint64]int) {
	for _, call := range calls {
		walkCall(call, ordinals)
	}
}

func walkCall(call *pbeth.Call, ordinals map[uint64]int) {
	ordinals[call.BeginOrdinal] = ordinals[call.BeginOrdinal] + 1
	ordinals[call.EndOrdinal] = ordinals[call.EndOrdinal] + 1

	walkChanges(call.BalanceChanges, ordinals)
	walkChanges(call.CodeChanges, ordinals)
	walkChanges(call.Logs, ordinals)
	walkChanges(call.StorageChanges, ordinals)
	walkChanges(call.NonceChanges, ordinals)
	walkChanges(call.GasChanges, ordinals)
}

func walkChanges[T any](changes []T, ordinals map[uint64]int) {
	for _, change := range changes {
		var x any = change
		if v, ok := x.(interface{ GetOrdinal() uint64 }); ok {
			ordinals[v.GetOrdinal()] = ordinals[v.GetOrdinal()] + 1
		}
	}
}

var b = big.NewInt
var empty, from, to = common.HexToAddress("00"), common.HexToAddress("01"), common.HexToAddress("02")
var emptyHash = common.Hash{}
var hex2Hash = common.HexToHash
var hex2Bytes = common.Hex2Bytes

func fileExits(t *testing.T, path string) bool {
	t.Helper()
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

func txEvent() *types.Transaction {
	return types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      1,
		To:       &to,
		Value:    big.NewInt(1),
		Data:     nil,
		V:        big.NewInt(1),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
}

func txReceiptEvent(txIndex uint) *types.Receipt {
	return &types.Receipt{
		Status:           1,
		TransactionIndex: txIndex,
	}
}

func blockEvent(height uint64) tracing.BlockEvent {
	return tracing.BlockEvent{
		Block: types.NewBlock(&types.Header{
			Number: big.NewInt(int64(height)),
		}, nil, nil, nil, nil),
		TD: b(1),
	}
}
