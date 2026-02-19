package client_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/assert"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	rpccore "github.com/sei-protocol/sei-chain/sei-tendermint/internal/rpc/core"
	tmjson "github.com/sei-protocol/sei-chain/sei-tendermint/libs/json"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	tmmath "github.com/sei-protocol/sei-chain/sei-tendermint/libs/math"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	rpchttp "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/http"
	rpclocal "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/jsonrpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func getHTTPClient(t *testing.T, logger log.Logger, conf *config.Config) *rpchttp.HTTP {
	t.Helper()

	rpcAddr := conf.RPC.ListenAddress
	c, err := rpchttp.NewWithClient(rpcAddr, http.DefaultClient)
	require.NoError(t, err)
	ctx := t.Context()
	require.NoError(t, c.Start(ctx))

	c.Logger = logger
	t.Cleanup(func() {
		require.NoError(t, c.Stop())
	})

	return c
}

func getHTTPClientWithTimeout(t *testing.T, logger log.Logger, conf *config.Config, timeout time.Duration) *rpchttp.HTTP {
	t.Helper()

	rpcAddr := conf.RPC.ListenAddress

	tclient := &http.Client{Timeout: timeout}
	c, err := rpchttp.NewWithClient(rpcAddr, tclient)
	require.NoError(t, err)
	ctx := t.Context()
	require.NoError(t, c.Start(ctx))

	c.Logger = logger
	t.Cleanup(func() {
		require.NoError(t, c.Stop())
	})

	return c
}

// GetClients returns a slice of clients for table-driven tests
func GetClients(t *testing.T, ns service.Service, conf *config.Config) []client.Client {
	t.Helper()

	node, ok := ns.(rpclocal.NodeService)
	require.True(t, ok)

	logger := log.NewTestingLogger(t)
	ncl, err := rpclocal.New(logger, node)
	require.NoError(t, err)

	return []client.Client{
		ncl,
		getHTTPClient(t, logger, conf),
	}
}

func TestClientOperations(t *testing.T) {
	ctx := t.Context()

	logger := log.NewTestingLogger(t)

	_, conf := NodeSuite(ctx, t, logger)

	t.Run("NilCustomHTTPClient", func(t *testing.T) {
		_, err := rpchttp.NewWithClient("http://example.com", nil)
		require.Error(t, err)

		_, err = rpcclient.NewWithHTTPClient("http://example.com", nil)
		require.Error(t, err)
	})
	t.Run("ParseInvalidAddress", func(t *testing.T) {
		// should remove trailing /
		invalidRemote := conf.RPC.ListenAddress + "/"
		_, err := rpchttp.New(invalidRemote)
		require.NoError(t, err)
	})
	t.Run("CustomHTTPClient", func(t *testing.T) {
		remote := conf.RPC.ListenAddress
		c, err := rpchttp.NewWithClient(remote, http.DefaultClient)
		require.NoError(t, err)
		status, err := c.Status(ctx)
		require.NoError(t, err)
		require.NotNil(t, status)
	})
	t.Run("CorsEnabled", func(t *testing.T) {
		origin := conf.RPC.CORSAllowedOrigins[0]
		remote := strings.ReplaceAll(conf.RPC.ListenAddress, "tcp", "http")

		req, err := http.NewRequestWithContext(ctx, "GET", remote, nil)
		require.NoError(t, err, "%+v", err)
		req.Header.Set("Origin", origin)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, "%+v", err)
		defer resp.Body.Close()

		require.Equal(t, resp.Header.Get("Access-Control-Allow-Origin"), origin)
	})
	t.Run("Batching", func(t *testing.T) {
		t.Run("JSONRPCCalls", func(t *testing.T) {
			logger := log.NewTestingLogger(t)
			c := getHTTPClient(t, logger, conf)
			testBatchedJSONRPCCalls(ctx, c)
		})
		t.Run("JSONRPCCallsCancellation", func(t *testing.T) {
			_, _, tx1 := MakeTxKV()
			_, _, tx2 := MakeTxKV()

			logger := log.NewTestingLogger(t)
			c := getHTTPClient(t, logger, conf)
			batch := c.NewBatch()
			_, err := batch.BroadcastTxCommit(ctx, tx1)
			require.NoError(t, err)
			_, err = batch.BroadcastTxCommit(ctx, tx2)
			require.NoError(t, err)
			// we should have 2 requests waiting
			require.Equal(t, 2, batch.Count())
			// we want to make sure we cleared 2 pending requests
			require.Equal(t, 2, batch.Clear())
			// now there should be no batched requests
			require.Equal(t, 0, batch.Count())
		})
		t.Run("SendingEmptyRequest", func(t *testing.T) {
			logger := log.NewTestingLogger(t)

			c := getHTTPClient(t, logger, conf)
			batch := c.NewBatch()
			_, err := batch.Send(ctx)
			require.Error(t, err, "sending an empty batch of JSON RPC requests should result in an error")
		})
		t.Run("ClearingEmptyRequest", func(t *testing.T) {
			logger := log.NewTestingLogger(t)

			c := getHTTPClient(t, logger, conf)
			batch := c.NewBatch()
			require.Zero(t, batch.Clear(), "clearing an empty batch of JSON RPC requests should result in a 0 result")
		})
		t.Run("ConcurrentJSONRPC", func(t *testing.T) {
			logger := log.NewTestingLogger(t)

			var wg sync.WaitGroup
			c := getHTTPClient(t, logger, conf)
			for range 50 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					testBatchedJSONRPCCalls(ctx, c)
				}()
			}
			wg.Wait()
		})
	})
}

// Make sure info is correct (we connect properly)
func TestClientMethodCalls(t *testing.T) {
	logger := log.NewTestingLogger(t)

	n, conf := NodeSuite(t.Context(), t, logger)

	// for broadcast tx tests
	pool := getMempool(t, n)

	// for evidence tests
	pv, err := privval.LoadOrGenFilePV(conf.PrivValidator.KeyFile(), conf.PrivValidator.StateFile())
	require.NoError(t, err)

	for i, c := range GetClients(t, n, conf) {
		t.Run(fmt.Sprintf("%T", c), func(t *testing.T) {
			t.Run("Status", func(t *testing.T) {
				status, err := c.Status(t.Context())
				require.NoError(t, err, "%d: %+v", i, err)
				require.Equal(t, conf.Moniker, status.NodeInfo.Moniker)
			})
			t.Run("Info", func(t *testing.T) {
				ctx := t.Context()
				info, err := c.ABCIInfo(ctx)
				require.NoError(t, err)

				status, err := c.Status(ctx)
				require.NoError(t, err)

				require.GreaterOrEqual(t, status.SyncInfo.LatestBlockHeight, info.Response.LastBlockHeight)
				require.True(t, strings.Contains(info.Response.Data, "size"))
			})
			t.Run("NetInfo", func(t *testing.T) {
				nc, ok := c.(client.NetworkClient)
				require.True(t, ok, "%d", i)
				netinfo, err := nc.NetInfo(t.Context())
				require.NoError(t, err, "%d: %+v", i, err)
				require.True(t, netinfo.Listening)
				require.Equal(t, 0, len(netinfo.Peers))
			})
			t.Run("DumpConsensusState", func(t *testing.T) {
				// FIXME: fix server so it doesn't panic on invalid input
				nc, ok := c.(client.NetworkClient)
				require.True(t, ok, "%d", i)
				cons, err := nc.DumpConsensusState(t.Context())
				require.NoError(t, err, "%d: %+v", i, err)
				require.NotEmpty(t, cons.RoundState)
				require.Empty(t, cons.Peers)
			})
			t.Run("ConsensusState", func(t *testing.T) {
				// FIXME: fix server so it doesn't panic on invalid input
				nc, ok := c.(client.NetworkClient)
				require.True(t, ok, "%d", i)
				cons, err := nc.ConsensusState(t.Context())
				require.NoError(t, err, "%d: %+v", i, err)
				require.NotEmpty(t, cons.RoundState)
			})
			t.Run("Health", func(t *testing.T) {
				nc, ok := c.(client.NetworkClient)
				require.True(t, ok, "%d", i)
				_, err := nc.Health(t.Context())
				require.NoError(t, err, "%d: %+v", i, err)
			})
			t.Run("GenesisAndValidators", func(t *testing.T) {
				ctx := t.Context()
				// make sure this is the right genesis file
				gen, err := c.Genesis(ctx)
				require.NoError(t, err, "%d: %+v", i, err)
				// get the genesis validator
				require.Equal(t, 1, len(gen.Genesis.Validators))
				gval := gen.Genesis.Validators[0]

				// get the current validators
				h := int64(1)
				vals, err := c.Validators(ctx, &h, nil, nil)
				require.NoError(t, err, "%d: %+v", i, err)
				require.Equal(t, 1, len(vals.Validators))
				require.Equal(t, 1, vals.Count)
				require.Equal(t, 1, vals.Total)
				val := vals.Validators[0]

				// make sure the current set is also the genesis set
				require.Equal(t, gval.Power, val.VotingPower)
				require.Equal(t, gval.PubKey, val.PubKey)
			})
			t.Run("GenesisChunked", func(t *testing.T) {
				ctx := t.Context()
				first, err := c.GenesisChunked(ctx, 0)
				require.NoError(t, err)

				decoded := make([]string, 0, first.TotalChunks)
				for i := 0; i < first.TotalChunks; i++ {
					chunk, err := c.GenesisChunked(ctx, uint(i))
					require.NoError(t, err)
					data, err := base64.StdEncoding.DecodeString(chunk.Data)
					require.NoError(t, err)
					decoded = append(decoded, string(data))

				}
				doc := []byte(strings.Join(decoded, ""))

				var out types.GenesisDoc
				require.NoError(t, tmjson.Unmarshal(doc, &out),
					"first: %+v, doc: %s", first, string(doc))
			})
			t.Run("ABCIQuery", func(t *testing.T) {
				ctx := t.Context()
				// write something
				k, v, tx := MakeTxKV()
				status, err := c.Status(ctx)
				require.NoError(t, err)
				_, err = c.BroadcastTxSync(ctx, tx)
				require.NoError(t, err, "%d: %+v", i, err)
				apph := status.SyncInfo.LatestBlockHeight + 2 // this is where the tx will be applied to the state

				// wait before querying
				err = client.WaitForHeight(ctx, c, apph, nil)
				require.NoError(t, err)
				res, err := c.ABCIQuery(ctx, "/key", k)
				qres := res.Response
				if assert.NoError(t, err) && assert.True(t, qres.IsOK()) {
					require.Equal(t, v, qres.Value)
				}
			})
			t.Run("AppCalls", func(t *testing.T) {
				ctx := t.Context()
				// get an offset of height to avoid racing and guessing
				s, err := c.Status(ctx)
				require.NoError(t, err)
				// sh is start height or status height
				sh := s.SyncInfo.LatestBlockHeight

				// look for the future
				h := sh + 20
				_, err = c.Block(ctx, &h)
				require.Error(t, err) // no block yet

				// write something
				k, v, tx := MakeTxKV()
				bres, err := c.BroadcastTxCommit(ctx, tx)
				require.NoError(t, err)
				require.True(t, bres.TxResult.IsOK())
				txh := bres.Height
				apph := txh + 1 // this is where the tx will be applied to the state

				// wait before querying
				err = client.WaitForHeight(ctx, c, apph, nil)
				require.NoError(t, err)

				_qres, err := c.ABCIQueryWithOptions(ctx, "/key", k, client.ABCIQueryOptions{Prove: false})
				require.NoError(t, err)
				qres := _qres.Response
				if assert.True(t, qres.IsOK()) {
					require.Equal(t, k, qres.Key)
					require.Equal(t, v, qres.Value)
				}

				// make sure we can lookup the tx with proof
				ptx, err := c.Tx(ctx, bres.Hash, true)
				require.NoError(t, err)
				require.Equal(t, txh, ptx.Height)
				require.Equal(t, tx, ptx.Tx)

				// and we can even check the block is added
				block, err := c.Block(ctx, &apph)
				require.NoError(t, err)
				appHash := block.Block.Header.AppHash
				require.True(t, len(appHash) > 0)
				require.Equal(t, apph, block.Block.Header.Height)

				blockByHash, err := c.BlockByHash(ctx, block.BlockID.Hash)
				require.NoError(t, err)
				require.Equal(t, block, blockByHash)

				// check that the header matches the block hash
				header, err := c.Header(ctx, &apph)
				require.NoError(t, err)
				require.Equal(t, block.Block.Header, *header.Header)

				headerByHash, err := c.HeaderByHash(ctx, block.BlockID.Hash)
				require.NoError(t, err)
				require.Equal(t, header, headerByHash)

				// now check the results
				blockResults, err := c.BlockResults(ctx, &txh)
				require.NoError(t, err, "%d: %+v", i, err)
				require.Equal(t, txh, blockResults.Height)
				if assert.Equal(t, 1, len(blockResults.TxsResults)) {
					// check success code
					require.Equal(t, 0, blockResults.TxsResults[0].Code)
				}

				// check blockchain info, now that we know there is info
				info, err := c.BlockchainInfo(ctx, apph, apph)
				require.NoError(t, err)
				require.True(t, info.LastHeight >= apph)
				if assert.Equal(t, 1, len(info.BlockMetas)) {
					lastMeta := info.BlockMetas[0]
					require.Equal(t, apph, lastMeta.Header.Height)
					blockData := block.Block
					require.Equal(t, blockData.Header.AppHash, lastMeta.Header.AppHash)
					require.Equal(t, block.BlockID, lastMeta.BlockID)
				}

				// and get the corresponding commit with the same apphash
				commit, err := c.Commit(ctx, &apph)
				require.NoError(t, err)
				cappHash := commit.Header.AppHash
				require.Equal(t, appHash, cappHash)
				require.NotNil(t, commit.Commit)

				// compare the commits (note Commit(2) has commit from Block(3))
				h = apph - 1
				commit2, err := c.Commit(ctx, &h)
				require.NoError(t, err)
				require.Equal(t, block.Block.LastCommitHash, commit2.Commit.Hash())

				// and we got a proof that works!
				_pres, err := c.ABCIQueryWithOptions(ctx, "/key", k, client.ABCIQueryOptions{Prove: true})
				require.NoError(t, err)
				pres := _pres.Response
				require.True(t, pres.IsOK())

				// XXX Test proof
			})
			t.Run("BlockchainInfo", func(t *testing.T) {
				ctx := t.Context()

				err := client.WaitForHeight(ctx, c, 10, nil)
				require.NoError(t, err)

				res, err := c.BlockchainInfo(ctx, 0, 0)
				require.NoError(t, err, "%d: %+v", i, err)
				require.True(t, res.LastHeight > 0)
				require.True(t, len(res.BlockMetas) > 0)

				res, err = c.BlockchainInfo(ctx, 1, 1)
				require.NoError(t, err, "%d: %+v", i, err)
				require.True(t, res.LastHeight > 0)
				require.True(t, len(res.BlockMetas) == 1)

				res, err = c.BlockchainInfo(ctx, 1, 10000)
				require.NoError(t, err, "%d: %+v", i, err)
				require.True(t, res.LastHeight > 0)
				require.True(t, len(res.BlockMetas) < 100)
				for _, m := range res.BlockMetas {
					require.NotNil(t, m)
				}

				res, err = c.BlockchainInfo(ctx, 10000, 1)
				require.Error(t, err)
				require.Nil(t, res)
				require.Contains(t, err.Error(), "can't be greater than max")
			})
			t.Run("BroadcastTxCommit", func(t *testing.T) {
				ctx := t.Context()
				_, _, tx := MakeTxKV()
				bres, err := c.BroadcastTxCommit(ctx, tx)
				require.NoError(t, err, "%d: %+v", i, err)
				require.True(t, bres.CheckTx.IsOK())
				require.True(t, bres.TxResult.IsOK())

				require.Equal(t, 0, pool.Size())
			})
			t.Run("BroadcastTxSync", func(t *testing.T) {
				ctx := t.Context()
				_, _, tx := MakeTxKV()
				initMempoolSize := pool.Size()
				bres, err := c.BroadcastTxSync(ctx, tx)
				require.NoError(t, err, "%d: %+v", i, err)
				require.Equal(t, bres.Code, abci.CodeTypeOK) // FIXME

				require.Equal(t, initMempoolSize+1, pool.Size())

				txs := pool.ReapMaxTxs(len(tx))
				require.Equal(t, tx, txs[0])
				pool.Flush()
			})
			t.Run("CheckTx", func(t *testing.T) {
				ctx := t.Context()
				_, _, tx := MakeTxKV()

				res, err := c.CheckTx(ctx, tx)
				require.NoError(t, err)
				require.Equal(t, abci.CodeTypeOK, res.Code)

				require.Equal(t, 0, pool.Size(), "mempool must be empty")
			})
			t.Run("Events", func(t *testing.T) {
				t.Run("Header", func(t *testing.T) {
					ctx, cancel := context.WithTimeout(t.Context(), waitForEventTimeout)
					defer cancel()
					query := types.QueryForEvent(types.EventNewBlockHeaderValue).String()
					evt, err := client.WaitForOneEvent(ctx, c, query)
					require.NoError(t, err, "%d: %+v", i, err)
					_, ok := evt.(types.EventDataNewBlockHeader)
					require.True(t, ok, "%d: %#v", i, evt)
					// TODO: more checks...
				})
				t.Run("Block", func(t *testing.T) {
					ctx := t.Context()
					const subscriber = "TestBlockEvents"

					eventCh, err := c.Subscribe(ctx, subscriber, types.QueryForEvent(types.EventNewBlockValue).String())
					require.NoError(t, err)
					t.Cleanup(func() {
						// At this point the ctx is cancelled, so the cleanup needs to run with a background context.
						if err := c.UnsubscribeAll(context.Background(), subscriber); err != nil {
							t.Error(err)
						}
					})

					var firstBlockHeight int64
					for i := range int64(3) {
						event := <-eventCh

						blockEvent, ok := event.Data.(types.LegacyEventDataNewBlock)
						if !ok {
							blockEventPtr, okPtr := event.Data.(*types.LegacyEventDataNewBlock)
							if okPtr {
								blockEvent = *blockEventPtr
							}
							ok = okPtr
						}
						require.True(t, ok)

						block := blockEvent.Block

						if firstBlockHeight == 0 {
							firstBlockHeight = block.Header.Height
						}

						require.Equal(t, firstBlockHeight+i, block.Header.Height)
					}
				})
				t.Run("BroadcastTxAsync", func(t *testing.T) {
					ctx := t.Context()
					testTxEventsSent(ctx, t, "async", c)
				})
				t.Run("BroadcastTxSync", func(t *testing.T) {
					ctx := t.Context()
					testTxEventsSent(ctx, t, "sync", c)
				})
			})
			t.Run("Evidence", func(t *testing.T) {
				t.Run("BroadcastDuplicateVote", func(t *testing.T) {
					ctx := t.Context()

					chainID := conf.ChainID()

					// make sure that the node has produced enough blocks
					waitForBlock(ctx, t, c, 2)
					evidenceHeight := int64(1)
					block, _ := c.Block(ctx, &evidenceHeight)
					ts := block.Block.Time
					correct, fakes := makeEvidences(t, pv, chainID, ts)

					result, err := c.BroadcastEvidence(ctx, correct)
					require.NoError(t, err, "BroadcastEvidence(%s) failed", correct)
					require.Equal(t, correct.Hash(), result.Hash, "expected result hash to match evidence hash")

					status, err := c.Status(ctx)
					require.NoError(t, err)
					err = client.WaitForHeight(ctx, c, status.SyncInfo.LatestBlockHeight+2, nil)
					require.NoError(t, err)

					result2, err := c.ABCIQuery(ctx, "/val", pv.Key.PubKey.Bytes())
					require.NoError(t, err)
					qres := result2.Response
					require.True(t, qres.IsOK())

					var v abci.ValidatorUpdate
					err = proto.Unmarshal(qres.Value, &v)
					require.NoError(t, err, "Error reading query result, value %v", qres.Value)

					pk, err := crypto.PubKeyFromProto(v.PubKey)
					require.NoError(t, err)

					require.Equal(t, pv.Key.PubKey, pk, "Stored PubKey not equal with expected, value %v", string(qres.Value))
					require.Equal(t, int64(9), v.Power, "Stored Power not equal with expected, value %v", string(qres.Value))

					for _, fake := range fakes {
						_, err := c.BroadcastEvidence(ctx, fake)
						require.Error(t, err, "BroadcastEvidence(%s) succeeded, but the evidence was fake", fake)
					}
				})
				t.Run("BroadcastEmpty", func(t *testing.T) {
					ctx := t.Context()
					_, err := c.BroadcastEvidence(ctx, nil)
					require.Error(t, err)
				})
			})
		})
	}
}

func getMempool(t *testing.T, srv service.Service) mempool.Mempool {
	t.Helper()
	n, ok := srv.(interface {
		RPCEnvironment() *rpccore.Environment
	})
	require.True(t, ok)
	return n.RPCEnvironment().Mempool
}

// these cases are roughly the same as the TestClientMethodCalls, but
// they have to loop over their clients in the individual test cases,
// so making a separate suite makes more sense, though isn't strictly
// speaking desirable.
func TestClientMethodCallsAdvanced(t *testing.T) {
	ctx := t.Context()

	logger := log.NewTestingLogger(t)

	n, conf := NodeSuite(ctx, t, logger)
	pool := getMempool(t, n)

	t.Run("UnconfirmedTxs", func(t *testing.T) {
		// populate mempool with 5 tx
		txs := make([]types.Tx, 5)
		ch := make(chan error, 5)
		for i := range 5 {
			_, _, tx := MakeTxKV()

			txs[i] = tx
			err := pool.CheckTx(ctx, tx, func(_ *abci.ResponseCheckTx) { ch <- nil }, mempool.TxInfo{})

			require.NoError(t, err)
		}
		// wait for tx to arrive in mempoool.
		for range 5 {
			select {
			case <-ch:
			case <-time.After(5 * time.Second):
				t.Error("Timed out waiting for CheckTx callback")
			}
		}
		close(ch)

		for _, c := range GetClients(t, n, conf) {
			for i := 1; i <= 2; i++ {
				mc := c.(client.MempoolClient)
				page, perPage := i, 3
				res, err := mc.UnconfirmedTxs(ctx, &page, &perPage)
				require.NoError(t, err)

				if i == 2 {
					perPage = 2
				}
				assert.Equal(t, perPage, res.Count)
				assert.Equal(t, 5, res.Total)
				assert.Equal(t, pool.SizeBytes(), res.TotalBytes)
				for _, tx := range res.Txs {
					assert.Contains(t, txs, tx)
				}
			}
		}

		pool.Flush()
	})
	t.Run("NumUnconfirmedTxs", func(t *testing.T) {
		ch := make(chan struct{})

		pool := getMempool(t, n)

		_, _, tx := MakeTxKV()

		err := pool.CheckTx(ctx, tx, func(_ *abci.ResponseCheckTx) { close(ch) }, mempool.TxInfo{})
		require.NoError(t, err)

		// wait for tx to arrive in mempoool.
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Error("Timed out waiting for CheckTx callback")
		}

		mempoolSize := pool.Size()
		for i, c := range GetClients(t, n, conf) {
			mc, ok := c.(client.MempoolClient)
			require.True(t, ok, "%d", i)
			res, err := mc.NumUnconfirmedTxs(ctx)
			require.NoError(t, err, "%d: %+v", i, err)

			assert.Equal(t, mempoolSize, res.Count)
			assert.Equal(t, mempoolSize, res.Total)
			assert.Equal(t, pool.SizeBytes(), res.TotalBytes)
		}

		pool.Flush()
	})
	t.Run("Tx", func(t *testing.T) {
		logger := log.NewTestingLogger(t)

		c := getHTTPClient(t, logger, conf)

		// first we broadcast a tx
		_, _, tx := MakeTxKV()
		bres, err := c.BroadcastTxCommit(ctx, tx)
		require.NoError(t, err, "%+v", err)

		txHeight := bres.Height
		txHash := bres.Hash

		anotherTxHash := types.Tx("a different tx").Hash()

		cases := []struct {
			valid bool
			prove bool
			hash  []byte
		}{
			// only valid if correct hash provided
			{true, false, txHash},
			{true, true, txHash},
			{false, false, anotherTxHash},
			{false, true, anotherTxHash},
			{false, false, nil},
			{false, true, nil},
		}

		for _, c := range GetClients(t, n, conf) {
			t.Run(fmt.Sprintf("%T", c), func(t *testing.T) {
				for j, tc := range cases {
					t.Run(fmt.Sprintf("Case%d", j), func(t *testing.T) {
						// now we query for the tx.
						// since there's only one tx, we know index=0.
						ptx, err := c.Tx(ctx, tc.hash, tc.prove)

						if !tc.valid {
							require.Error(t, err)
						} else {
							require.NoError(t, err, "%+v", err)
							require.Equal(t, txHeight, ptx.Height)
							require.Equal(t, tx, ptx.Tx)
							require.Zero(t, ptx.Index)
							require.True(t, ptx.TxResult.IsOK())
							require.Equal(t, txHash, ptx.Hash)

							// time to verify the proof
							proof := ptx.Proof
							if tc.prove {
								require.Equal(t, tx, proof.Data)
								require.NoError(t, proof.Proof.Verify(proof.RootHash, txHash))
							}
						}
					})
				}
			})
		}
	})
	t.Run("TxSearchWithTimeout", func(t *testing.T) {
		logger := log.NewTestingLogger(t)

		timeoutClient := getHTTPClientWithTimeout(t, logger, conf, 10*time.Second)

		_, _, tx := MakeTxKV()
		_, err := timeoutClient.BroadcastTxCommit(ctx, tx)
		require.NoError(t, err)

		// query using a compositeKey (see kvstore application)
		result, err := timeoutClient.TxSearch(ctx, "app.creator='Cosmoshi Netowoko'", false, nil, nil, "asc")
		require.NoError(t, err)
		require.Greater(t, len(result.Txs), 0, "expected a lot of transactions")
	})
	t.Run("TxSearch", func(t *testing.T) {
		t.Skip("Test Asserts Non-Deterministic Results")
		logger := log.NewTestingLogger(t)

		c := getHTTPClient(t, logger, conf)

		// first we broadcast a few txs
		for range 10 {
			_, _, tx := MakeTxKV()
			_, err := c.BroadcastTxSync(ctx, tx)
			require.NoError(t, err)
		}

		// since we're not using an isolated test server, we'll have lingering transactions
		// from other tests as well
		result, err := c.TxSearch(ctx, "tx.height >= 0", true, nil, nil, "asc")
		require.NoError(t, err)
		txCount := len(result.Txs)

		// pick out the last tx to have something to search for in tests
		find := result.Txs[len(result.Txs)-1]
		anotherTxHash := types.Tx("a different tx").Hash()

		for _, c := range GetClients(t, n, conf) {
			t.Run(fmt.Sprintf("%T", c), func(t *testing.T) {
				// now we query for the tx.
				result, err := c.TxSearch(ctx, fmt.Sprintf("tx.hash='%v'", find.Hash), true, nil, nil, "asc")
				require.NoError(t, err)
				require.Len(t, result.Txs, 1)
				require.Equal(t, find.Hash, result.Txs[0].Hash)

				ptx := result.Txs[0]
				require.Equal(t, find.Height, ptx.Height)
				require.Equal(t, find.Tx, ptx.Tx)
				require.Zero(t, ptx.Index)
				require.True(t, ptx.TxResult.IsOK())
				require.Equal(t, find.Hash, ptx.Hash)

				// time to verify the proof
				if assert.Equal(t, find.Tx, ptx.Proof.Data) {
					require.NoError(t, ptx.Proof.Proof.Verify(ptx.Proof.RootHash, find.Hash))
				}

				// query by height
				result, err = c.TxSearch(ctx, fmt.Sprintf("tx.height=%d", find.Height), true, nil, nil, "asc")
				require.NoError(t, err)
				require.Len(t, result.Txs, 1)

				// query for non existing tx
				result, err = c.TxSearch(ctx, fmt.Sprintf("tx.hash='%X'", anotherTxHash), false, nil, nil, "asc")
				require.NoError(t, err)
				require.Len(t, result.Txs, 0)

				// query using a compositeKey (see kvstore application)
				result, err = c.TxSearch(ctx, "app.creator='Cosmoshi Netowoko'", false, nil, nil, "asc")
				require.NoError(t, err)
				require.Greater(t, len(result.Txs), 0, "expected a lot of transactions")

				// query using an index key
				result, err = c.TxSearch(ctx, "app.index_key='index is working'", false, nil, nil, "asc")
				require.NoError(t, err)
				require.Greater(t, len(result.Txs), 0, "expected a lot of transactions")

				// query using an noindex key
				result, err = c.TxSearch(ctx, "app.noindex_key='index is working'", false, nil, nil, "asc")
				require.NoError(t, err)
				require.Equal(t, len(result.Txs), 0, "expected a lot of transactions")

				// query using a compositeKey (see kvstore application) and height
				result, err = c.TxSearch(ctx,
					"app.creator='Cosmoshi Netowoko' AND tx.height<10000", true, nil, nil, "asc")
				require.NoError(t, err)
				require.Greater(t, len(result.Txs), 0, "expected a lot of transactions")

				// query a non existing tx with page 1 and txsPerPage 1
				perPage := 1
				result, err = c.TxSearch(ctx, "app.creator='Cosmoshi Neetowoko'", true, nil, &perPage, "asc")
				require.NoError(t, err)
				require.Len(t, result.Txs, 0)

				// check sorting
				result, err = c.TxSearch(ctx, "tx.height >= 1", false, nil, nil, "asc")
				require.NoError(t, err)
				for k := 0; k < len(result.Txs)-1; k++ {
					require.LessOrEqual(t, result.Txs[k].Height, result.Txs[k+1].Height)
					require.LessOrEqual(t, result.Txs[k].Index, result.Txs[k+1].Index)
				}

				result, err = c.TxSearch(ctx, "tx.height >= 1", false, nil, nil, "desc")
				require.NoError(t, err)
				for k := 0; k < len(result.Txs)-1; k++ {
					require.GreaterOrEqual(t, result.Txs[k].Height, result.Txs[k+1].Height)
					require.GreaterOrEqual(t, result.Txs[k].Index, result.Txs[k+1].Index)
				}
				// check pagination
				perPage = 3
				var (
					seen      = map[int64]bool{}
					maxHeight int64
					pages     = int(math.Ceil(float64(txCount) / float64(perPage)))
				)

				for page := 1; page <= pages; page++ {
					page := page
					result, err := c.TxSearch(ctx, "tx.height >= 1", false, &page, &perPage, "asc")
					require.NoError(t, err)
					if page < pages {
						require.Len(t, result.Txs, perPage)
					} else {
						require.LessOrEqual(t, len(result.Txs), perPage)
					}
					require.Equal(t, txCount, result.TotalCount)
					for _, tx := range result.Txs {
						require.False(t, seen[tx.Height],
							"Found duplicate height %v in page %v", tx.Height, page)
						require.Greater(t, tx.Height, maxHeight,
							"Found decreasing height %v (max seen %v) in page %v", tx.Height, maxHeight, page)
						seen[tx.Height] = true
						maxHeight = tx.Height
					}
				}
				require.Len(t, seen, txCount)
			})
		}
	})
}

// WARNING: this function is called in subgoroutines to it shouldn't use testing.T.
func testBatchedJSONRPCCalls(ctx context.Context, c *rpchttp.HTTP) {
	k1, _, tx1 := MakeTxKV()
	k2, _, tx2 := MakeTxKV()

	batch := c.NewBatch()
	r1 := utils.OrPanic1(batch.BroadcastTxCommit(ctx, tx1))
	r2 := utils.OrPanic1(batch.BroadcastTxCommit(ctx, tx2))
	utils.OrPanic(utils.TestDiff(2, batch.Count()))
	bresults := utils.OrPanic1(batch.Send(ctx))
	utils.OrPanic(utils.TestDiff(bresults, []any{r1, r2}))
	utils.OrPanic(utils.TestDiff(0, batch.Count()))
	apph := tmmath.MaxInt64(r1.Height, r2.Height) + 1

	utils.OrPanic(client.WaitForHeight(ctx, c, apph, nil))

	q1 := utils.OrPanic1(batch.ABCIQuery(ctx, "/key", k1))
	q2 := utils.OrPanic1(batch.ABCIQuery(ctx, "/key", k2))
	utils.OrPanic(utils.TestDiff(2, batch.Count()))
	qresults := utils.OrPanic1(batch.Send(ctx))
	utils.OrPanic(utils.TestDiff(qresults, []any{q1, q2}))
}
