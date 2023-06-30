package provider

import (
	"context"

	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
)

var _ Provider = (*Provider)(nil)

type (
	// CryptoProvider defines an oracle provider implemented by the crypto.com
	// public API.
	//
	// REF: https://exchange-docs.crypto.com/spot/index.html
	CryptoProvider struct {
		ctx       context.Context
		endpoints config.ProviderEndpoint
	}

	CryptoTickersResponse struct {
		Code   int64                     `json:"code"`
		Result CryptoTickersResponseData `json:"result"`
	}

	CryptoTickersResponseData struct {
		Data []CryptoTicker `json:"data"`
	}

	CryptoTicker struct {
		Symbol string `json:"i"` // Symbol ex.: BTC_USDT
		Price  string `json:"a"` // Last price ex.: 0.0025
		Volume string `json:"v"` // Total traded base asset volume ex.: 1000
		Time   int64  `json:"t"` // Timestamp ex.: 1675246930699
	}
)
