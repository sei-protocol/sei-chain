require('log-timestamp');
const axios = require('axios');
const coinUtils = require('./utils').utils;

// Note that ust seems to only support conversions to usd, so we need to
// do the following conversion to get x:ust:
//   x:usd / ust:usd
var getCoinGeckoPrice = async (marketID) => {
  let coinPrice;
  let ustPrice;
  try {
    const coinPriceUrl = coinUtils.loadCoinGeckoQuery(marketID);
    const ustPriceUrl = coinUtils.loadCoinGeckoQuery('ust');
    const coinPriceFetchAsync = axios.get(coinPriceUrl);
    const ustPriceFetchAsync = axios.get(ustPriceUrl);
    const coinPriceFetch = await coinPriceFetchAsync;
    const ustPriceFetch = await ustPriceFetchAsync;
    coinPrice = coinPriceFetch.data[coinUtils.loadCoinGeckoMarket(marketID)].usd;
    ustPrice = ustPriceFetch.data[coinUtils.loadCoinGeckoMarket('ust')].usd;
  } catch (e) {
    console.log(e);
    console.log(`could not fetch ${marketID} price from Coin Gecko`);
    return;
  }
  return parseFloat(coinPrice) / parseFloat(ustPrice);
};

var getBinancePrice = async (marketID) => {
  if (!coinUtils.marketSupportedByBinance(marketID)) {
    throw new Error(`${marketID} not supported in Binance`);
  }
  if (coinUtils.marketHasDirectUSTConversion(marketID)) {
    return getBinanceUSTPrice(marketID);
  }
  return calculateBinanceUSTPrice(marketID);

};

// Directly query binance for price of x:ust
var getBinanceUSTPrice = async (marketID) => {
  let priceFetch;
  try {
    let url = coinUtils.loadBinanceQuery(marketID, 'ust');
    priceFetch = await axios.get(url);
  } catch (e) {
    console.log(e);
    throw new Error(`could not fetch ${marketID} price from binance`);
  }
  return priceFetch.data.lastPrice;
};

// Calculate the price of x:ust by getting the prices based on usdt
// (similar to Coingecko computation)
var calculateBinanceUSTPrice = async (marketID) => {
  let coinPriceFetch;
  let ustPriceFetch;
  try {
    let coinPriceUrl = coinUtils.loadBinanceQuery(marketID);
    let ustPriceUrl = coinUtils.loadBinanceQuery('ust');
    let coinPriceFetchAsync = axios.get(coinPriceUrl);
    let ustPriceFetchAsync = axios.get(ustPriceUrl);
    coinPriceFetch = await coinPriceFetchAsync;
    ustPriceFetch = await ustPriceFetchAsync;
  } catch (e) {
    console.log(e);
    throw new Error(`could not fetch ${marketID} price from binance`)
  }
  return coinPriceFetch.data.lastPrice / ustPriceFetch.data.lastPrice;
};

module.exports.prices = {
  getBinancePrice,
  getCoinGeckoPrice,
};
