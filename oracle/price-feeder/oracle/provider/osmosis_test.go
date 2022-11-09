package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestOsmosisProvider_GetTickerPrices(t *testing.T) {
	p := NewOsmosisProvider(config.ProviderEndpoint{})

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			require.Equal(t, "/tokens/v2/all", req.URL.String())
			resp := `[
				{
					"price": 100.22,
					"denom": "ibc/0EF15DF2F02480ADE0BB6E85D9EBB5DAEA2836D3860E9F97F9AADE4F57A31AA0",
					"symbol": "LUNA",
					"liquidity": 56928301.60178607,
					"volume_24h": 7047660.837452592,
					"name": "Luna"
				},
				{
					"price": 28.52,
					"denom": "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2",
					"symbol": "ATOM",
					"liquidity": 189672157.83693966,
					"volume_24h": 17006018.613512218,
					"name": "Cosmos"
				}
			]
			`
			rw.Write([]byte(resp))
		}))
		defer server.Close()

		p.client = server.Client()
		p.baseURL = server.URL

		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, sdk.MustNewDecFromStr("28.52"), prices["ATOMUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr("17006018.613512218"), prices["ATOMUSDT"].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			require.Equal(t, "/tokens/v2/all", req.URL.String())
			resp := `[
				{
					"price": 100.22,
					"denom": "ibc/0EF15DF2F02480ADE0BB6E85D9EBB5DAEA2836D3860E9F97F9AADE4F57A31AA0",
					"symbol": "LUNA",
					"liquidity": 56928301.60178607,
					"volume_24h": 7047660.837452592,
					"name": "Luna"
				},
				{
					"price": 28.52,
					"denom": "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2",
					"symbol": "ATOM",
					"liquidity": 189672157.83693966,
					"volume_24h": 17006018.613512218,
					"name": "Cosmos"
				}
			]
			`
			rw.Write([]byte(resp))
		}))
		defer server.Close()

		p.client = server.Client()
		p.baseURL = server.URL

		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "LUNA", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, sdk.MustNewDecFromStr("28.52"), prices["ATOMUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr("17006018.613512218"), prices["ATOMUSDT"].Volume)
		require.Equal(t, sdk.MustNewDecFromStr("100.22"), prices["LUNAUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr("7047660.837452592"), prices["LUNAUSDT"].Volume)
	})

	t.Run("invalid_request_bad_response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			require.Equal(t, "/tokens/v2/all", req.URL.String())
			rw.Write([]byte(`FOO`))
		}))
		defer server.Close()

		p.client = server.Client()
		p.baseURL = server.URL

		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.Error(t, err)
		require.Nil(t, prices)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			require.Equal(t, "/tokens/v2/all", req.URL.String())
			resp := `[
				{
					"price": 100.22,
					"denom": "ibc/0EF15DF2F02480ADE0BB6E85D9EBB5DAEA2836D3860E9F97F9AADE4F57A31AA0",
					"symbol": "LUNA",
					"liquidity": 56928301.60178607,
					"volume_24h": 7047660.837452592,
					"name": "Luna"
				},
				{
					"price": 28.52,
					"denom": "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2",
					"symbol": "ATOM",
					"liquidity": 189672157.83693966,
					"volume_24h": 17006018.613512218,
					"name": "Cosmos"
				}
			]
			`
			rw.Write([]byte(resp))
		}))
		defer server.Close()

		p.client = server.Client()
		p.baseURL = server.URL

		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.Error(t, err)
		require.Nil(t, prices)
	})

	t.Run("check_redirect", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			http.Redirect(rw, r, p.baseURL, http.StatusTemporaryRedirect)
		}))
		defer server.Close()

		server.Client().CheckRedirect = preventRedirect
		p.client = server.Client()
		p.baseURL = server.URL

		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.Error(t, err)
		require.Nil(t, prices)
	})
}

func TestOsmosisProvider_GetAvailablePairs(t *testing.T) {
	p := NewOsmosisProvider(config.ProviderEndpoint{})
	p.GetAvailablePairs()

	t.Run("valid_available_pair", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			require.Equal(t, "/pairs/v1/summary", req.URL.String())
			resp := `{
				"data": [
					{
						"base_symbol": "ATOM",
						"quote_symbol": "OSMO"
					},
					{
						"base_symbol": "ION",
						"quote_symbol": "OSMO"
					}
				]
			}`
			rw.Write([]byte(resp))
		}))
		defer server.Close()

		p.client = server.Client()
		p.baseURL = server.URL

		availablePairs, err := p.GetAvailablePairs()
		require.Nil(t, err)

		_, exist := availablePairs["ATOMOSMO"]
		require.True(t, exist)

		_, exist = availablePairs["IONOSMO"]
		require.True(t, exist)
	})
}
