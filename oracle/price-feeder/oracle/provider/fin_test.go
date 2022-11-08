package provider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestFinProvider_GetTickerPrices(t *testing.T) {
	p := NewFinProvider(config.ProviderEndpoint{})

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			require.Equal(t, "/api/coingecko/tickers", req.URL.String())
			resp := `{
				"tickers": [{
					"ask":"0.9640000000",
					"base_currency":"KUJI",
					"base_volume":"544225.3735890000",
					"bid":"0.9450000000",
					"high":"0.9830000001",
					"last_price":"0.9640001379",
					"low":"0.7712884676",
					"pool_id":"kujira14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sl4e867",
					"target_currency":"axlUSDC",
					"target_volume":"480430.1470840000",
					"ticker_id":"KUJI_axlUSDC"
				}]
			}`
			rw.Write([]byte(resp))
		}))
		defer server.Close()
		p.client = server.Client()
		p.baseURL = server.URL
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "KUJI", Quote: "AXLUSDC"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, sdk.MustNewDecFromStr("0.9640001379"), prices["KUJIAXLUSDC"].Price)
		require.Equal(t, sdk.MustNewDecFromStr("544225.3735890000"), prices["KUJIAXLUSDC"].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			require.Equal(t, "/api/coingecko/tickers", req.URL.String())
			resp := `{
				"tickers": [{
					"ask":"0.9640000000",
					"base_currency":"KUJI",
					"base_volume":"544225.3735890000",
					"bid":"0.9450000000",
					"high":"0.9830000001",
					"last_price":"0.9640001379",
					"low":"0.7712884676",
					"pool_id":"kujira14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sl4e867",
					"target_currency":"axlUSDC",
					"target_volume":"480430.1470840000",
					"ticker_id":"KUJI_axlUSDC"
				}, {
					"ask":"1.8750000000",
					"base_currency":"EVMOS",
					"base_volume":"122.0077927374",
					"bid":"1.5110000000",
					"high":"1.8650000000",
					"last_price":"1.8650000000",
					"low":"1.5087335000",
					"pool_id":"kujira182nff4ttmvshn6yjlqj5czapfcav9434l2qzz8aahf5pxnyd33tsz30aw6",
					"target_currency":"axlUSDC",
					"target_volume":"211.3908830000",
					"ticker_id":"EVMOS_axlUSDC"
				}]
			}`
			rw.Write([]byte(resp))
		}))
		defer server.Close()
		p.client = server.Client()
		p.baseURL = server.URL
		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "KUJI", Quote: "AXLUSDC"},
			types.CurrencyPair{Base: "EVMOS", Quote: "AXLUSDC"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, sdk.MustNewDecFromStr("0.9640001379"), prices["KUJIAXLUSDC"].Price)
		require.Equal(t, sdk.MustNewDecFromStr("544225.3735890000"), prices["KUJIAXLUSDC"].Volume)
		require.Equal(t, sdk.MustNewDecFromStr("1.8650000000"), prices["EVMOSAXLUSDC"].Price)
		require.Equal(t, sdk.MustNewDecFromStr("122.0077927374"), prices["EVMOSAXLUSDC"].Volume)
	})

	t.Run("invalid_request_bad_response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			require.Equal(t, "/api/coingecko/tickers", req.URL.String())
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
			require.Equal(t, "/api/coingecko/tickers", req.URL.String())
			resp := `{
				"tickers": [{
					"ask":"0.9640000000",
					"base_currency":"KUJI",
					"base_volume":"544225.3735890000",
					"bid":"0.9450000000",
					"high":"0.9830000001",
					"last_price":"0.9640001379",
					"low":"0.7712884676",
					"pool_id":"kujira14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sl4e867",
					"target_currency":"axlUSDC",
					"target_volume":"480430.1470840000",
					"ticker_id":"KUJI_axlUSDC"
				}]
			}`
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

func TestFinProvideR_GetCandlePrices(t *testing.T) {
	p := NewFinProvider(config.ProviderEndpoint{})

	t.Run("valid_request_single_candle", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			requestUrl := req.URL.String()
			resp := ""
			if requestUrl == "/api/coingecko/pairs" {
				resp = `{
					"pairs": [
						{"base":"KUJI","pool_id":"kujiraTESTADDRESS","target":"axlUSDC","ticker_id":"KUJI_axlUSDC"}
					]
				}`
			} else {
				require.Equal(t, "/api/trades/candles?contract=kujiraTESTADDRESS", strings.Split(requestUrl, "&")[0])
				resp = `{
					"candles": [
						{"bin":"2022-08-07T14:05:00.000000Z","close":"0.65600004530055243509","high":"0.65600004530055243509","low":"0.65600004530055243509","open":"0.65600004530055243509","volume":"7646000"},
						{"bin":"2022-08-07T14:10:00.000000Z","close":"0.65600004530055243509","high":"0.65600004530055243509","low":"0.65600004530055243509","open":"0.65600004530055243509","volume":"0"},
						{"bin":"2022-08-07T14:15:00.000000Z","close":"0.65928507215810458196","high":"0.65928507215810458196","low":"0.65928507215810458196","open":"0.65928507215810458196","volume":"622000000"}
					]
				}`
			}
			rw.Write([]byte(resp))
		}))
		defer server.Close()
		p.client = server.Client()
		p.baseURL = server.URL
		prices, err := p.GetCandlePrices(types.CurrencyPair{Base: "KUJI", Quote: "AXLUSDC"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Len(t, prices["KUJIAXLUSDC"], 3)
		require.Equal(t, sdk.MustNewDecFromStr("0.656000045300552435"), prices["KUJIAXLUSDC"][0].Price)
		require.Equal(t, sdk.MustNewDecFromStr("0.659285072158104581"), prices["KUJIAXLUSDC"][2].Price)
		require.Equal(t, sdk.MustNewDecFromStr("7646000"), prices["KUJIAXLUSDC"][0].Volume)
		require.Equal(t, sdk.MustNewDecFromStr("0"), prices["KUJIAXLUSDC"][1].Volume)
		require.Equal(t, int64(1659881100), prices["KUJIAXLUSDC"][0].TimeStamp)
		require.Equal(t, int64(1659881700), prices["KUJIAXLUSDC"][2].TimeStamp)
	})
}

func TestFinProvider_GetAvailablePairs(t *testing.T) {
	p := NewFinProvider(config.ProviderEndpoint{})
	p.GetAvailablePairs()

	t.Run("valid_available_pair", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			require.Equal(t, "/api/coingecko/pairs", req.URL.String())
			resp := `{
				"pairs":[{
					"base":"KUJI",
					"pool_id":"kujira14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sl4e867",
					"target":"axlUSDC",
					"ticker_id":"KUJI_axlUSDC"
				},
				{
					"base":"wETH",
					"pool_id":"kujira1suhgf5svhu4usrurvxzlgn54ksxmn8gljarjtxqnapv8kjnp4nrsqq4jjh",
					"target":"axlUSDC",
					"ticker_id":"wETH_axlUSDC"
				}]
			}`
			rw.Write([]byte(resp))
		}))
		defer server.Close()
		p.client = server.Client()
		p.baseURL = server.URL
		availablePairs, err := p.GetAvailablePairs()
		require.Nil(t, err)
		_, exist := availablePairs["KUJIAXLUSDC"]
		require.True(t, exist)
		_, exist = availablePairs["WETHAXLUSDC"]
		require.True(t, exist)
	})
}
