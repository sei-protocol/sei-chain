const COINGECKO_V3_SIMPLE_PRICE_REQUEST = (ids, currencies) =>
  `https://api.coingecko.com/api/v3/simple/price/?ids=${ids}&vs_currencies=${currencies}`;
const BINANCE_V3_TICKER_REQUEST = (symbol) =>
  `https://api.binance.com/api/v3/ticker/24hr?symbol=${symbol}`;
const BINANCE_SUPPORTED_MARKET_IDS = ['ust', 'btc', 'atom', 'sei', 'sol', 'eth'];
const BINACE_MARKET_IDS_WITH_DIRECT_UST_CONVERSION = ['sei', 'eth'];

// Check https://api.coingecko.com/api/v3/simple/supported_vs_currencies
// for supported currencies
const loadCoinGeckoMarket = (marketID) => {
  switch (marketID) {
    case 'ust':
      return 'terrausd';
    case 'btc':
      return 'bitcoin';
    case 'atom':
      return 'cosmos';
    case 'sei':
      return 'sei';
    case 'osmo':
      return 'osmosis';
    case 'sol':
      return 'solana';
    case 'eth':
      return 'ethereum';
    default:
      throw `invalid coin gecko market id ${marketID}`;
  }
};

const loadCoinGeckoQuery = (marketID) => {
  return COINGECKO_V3_SIMPLE_PRICE_REQUEST(loadCoinGeckoMarket(marketID), 'usd');
};

const loadPrimaryMarket = (marketID) => {
  loadCoinGeckoMarket(marketID);
};

const loadBackupMarket = (marketID) => {
  loadBinanceMarket(marketID);
};

// Check https://api.binance.com/api/v3/ticker/price
// for supported conversions
const marketSupportedByBinance = (marketID) => {
  return BINANCE_SUPPORTED_MARKET_IDS.indexOf(marketID) > -1;
};

const marketHasDirectUSTConversion = (marketID) => {
  return BINACE_MARKET_IDS_WITH_DIRECT_UST_CONVERSION.indexOf(marketID) > -1;
};

const loadBinanceMarket = (marketID1, marketID2) => {
  return `${marketID1}`.toUpperCase() + `${marketID2}`.toUpperCase();
};

const loadBinanceQuery = (marketID1, marketID2= 'usdt') => {
  return BINANCE_V3_TICKER_REQUEST(loadBinanceMarket(marketID1, marketID2));
};

module.exports.utils = {
  loadCoinGeckoMarket,
  loadCoinGeckoQuery,
  loadPrimaryMarket,
  loadBackupMarket,
  loadBinanceMarket,
  loadBinanceQuery,
  marketSupportedByBinance,
  marketHasDirectUSTConversion,
};
