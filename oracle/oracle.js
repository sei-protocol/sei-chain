const { SigningCosmWasmClient } = require('@cosmjs/cosmwasm-stargate');
const { GasPrice, assertIsDeliverTxSuccess } = require('@cosmjs/stargate');
require('dotenv').config();
require('log-timestamp');
const prices = require(`./prices`).prices;
const utils = require('./utils').utils;

// TODO(psu): Insert actual addresses
const TOKEN_ADDR_MAP = {
  btc: 'cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr',
  eth: 'cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr',
  sol: 'cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr',
  osmo: 'cosmos14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9s4hmalr',
};

/**
 * Price oracle class for posting prices to sei-chain.
 */
class PriceOracle {
  constructor(marketIDs, chainRpc, wallet, account, contractAddress, gasFee) {
    if (!marketIDs) {
      throw new Error('must specify at least one market ID');
    }
    if (!chainRpc) {
      throw new Error('must specify at rpc endpoint for chain');
    }

    // Validate each market ID on CoinGecko (primary) and Binance (backup)
    for (let i = 0; i < marketIDs.length; i += 1) {
      try {
        utils.loadPrimaryMarket(marketIDs[i]);
        utils.loadBackupMarket(marketIDs[i]);
      } catch (e) {
        console.log("couldn't load remote market from market ID, error:", e);
        return;
      }
    }
    this.marketIDs = marketIDs;
    this.wallet = wallet;
    this.account = account;
    this.chainRpc = chainRpc;
    this.contractAddress = contractAddress;
    this.gasFee = gasFee;
  }

  /**
   * Get prices for each market
   */
  async getAndPostPrices() {
    const chainClient = await SigningCosmWasmClient.connectWithSigner(
      this.chainRpc,
      this.wallet,
      { gasPrice: GasPrice.fromString(this.gasFee) }
    );
    await asyncForEach(this.marketIDs, async (market) => {
      const fetchedPrice = await this.fetchPrice(market);
      if (!fetchedPrice.success) {
        return;
      }
      // Post fetched price
      const msg = { "update_price": { "token_addr": TOKEN_ADDR_MAP[market], "price": String(fetchedPrice.price) } };
      const result = await chainClient.execute(
        this.account.address,
        this.contractAddress,
        msg,
        "auto"
      );
      console.log(`Finished executing contract for price update ${result}`);
      assertIsDeliverTxSuccess(result);
      return fetchedPrice.price;
    });
  }


  /**
   * Fetches price for a market ID
   * @param {String} marketID the market's ID
   */
  async fetchPrice(marketID) {
    let error = false;
    let res;
    try {
      res = await this.fetchPrimaryPrice(marketID);
      if (!res.success) {
        error = true;
      }
    } catch (error) {
      error = true;
    }
    if (error) {
      console.log("trying backup price source after error");
      res = await this.fetchBackupPrice(marketID);
    }
    return res;
  }

  /**
 * Fetches price from the primary source for a market
 * @param {String} marketID the market's ID
 */
  async fetchPrimaryPrice(marketID) {
    return this.fetchPriceCoinGecko(marketID);
  }

  /**
* Fetches price from the backup source for a market
* @param {String} marketID the market's ID
*/
  async fetchBackupPrice(marketID) {
    return this.fetchPriceBinance(marketID);
  }

  /**
   * Fetches price from Binance
   * @param {String} marketID the market's ID
   */
  async fetchPriceBinance(marketID) {
    let retreivedPrice;
    try {
      retreivedPrice = await prices.getBinancePrice(marketID);
      console.debug(`Binance price for ${marketID}: ${retreivedPrice}`);
    } catch (e) {
      console.log(`could not get ${marketID} price from Binance: ${e}`);
      return { price: null, success: false };
    }
    return { price: retreivedPrice, success: true };
  }

  /**
   * Fetches price from Coin Gecko
   * @param {String} marketID the market's ID
   */
  async fetchPriceCoinGecko(marketID) {
    let retreivedPrice;
    try {
      retreivedPrice = await prices.getCoinGeckoPrice(marketID);
      console.debug(`Coin Gecko price for ${marketID}: ${retreivedPrice}`);
    } catch (e) {
      console.log(`could not get ${marketID} price from Coin Gecko: ${e}`);
      return { price: null, success: false };
    }
    return { price: retreivedPrice, success: true };
  }
}

var asyncForEach = async (array, callback) => {
  for (let index = 0; index < array.length; index += 1) {
    await callback(array[index], index, array);
  }
};

module.exports.PriceOracle = PriceOracle;
