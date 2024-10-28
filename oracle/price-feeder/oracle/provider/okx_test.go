package provider

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestOkxProvider_GetTickerPrices(t *testing.T) {
	p, err := NewOkxProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := "34.69000000"
		volume := "2396974.02000000"

		syncMap := map[string]OkxTickerPair{}
		syncMap["ATOM-USDT"] = OkxTickerPair{
			OkxInstID: OkxInstID{
				InstID: "ATOM-USDT",
			},
			Last:   lastPrice,
			Vol24h: volume,
		}

		p.tickers = syncMap

		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, sdk.MustNewDecFromStr(lastPrice), prices["ATOMUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices["ATOMUSDT"].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		lastPriceAtom := "34.69000000"
		lastPriceSei := "41.35000000"
		volume := "2396974.02000000"

		syncMap := map[string]OkxTickerPair{}
		syncMap["ATOM-USDT"] = OkxTickerPair{
			OkxInstID: OkxInstID{
				InstID: "ATOM-USDT",
			},
			Last:   lastPriceAtom,
			Vol24h: volume,
		}

		syncMap["SEI-USDT"] = OkxTickerPair{
			OkxInstID: OkxInstID{
				InstID: "SEI-USDT",
			},
			Last:   lastPriceSei,
			Vol24h: volume,
		}

		p.tickers = syncMap
		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "SEI", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, sdk.MustNewDecFromStr(lastPriceAtom), prices["ATOMUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices["ATOMUSDT"].Volume)
		require.Equal(t, sdk.MustNewDecFromStr(lastPriceSei), prices["SEIUSDT"].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices["SEIUSDT"].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.NoError(t, err)
		require.Zero(t, len(prices))
	})
}

func TestOkxProvider_SubscribeCurrencyPairs(t *testing.T) {
	p, err := NewOkxProvider(
		context.TODO(),
		zerolog.Nop(),
		config.ProviderEndpoint{},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("invalid_subscribe_channels_empty", func(t *testing.T) {
		err = p.SubscribeCurrencyPairs([]types.CurrencyPair{}...)
		require.ErrorContains(t, err, "currency pairs is empty")
	})
}

func TestOkxCurrencyPairToOkxPair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	okxSymbol := currencyPairToOkxPair(cp)
	require.Equal(t, okxSymbol, "ATOM-USDT")
}
