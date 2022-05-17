require('dotenv').config({ path: process.env.ENV_FILE });
const { DirectSecp256k1HdWallet } = require('@cosmjs/proto-signing');
const cron = require('node-cron');
const PriceOracle = require('./oracle').PriceOracle

const main = async () => {
  // Load environment variables
  const marketIDs = process.env.MARKET_IDS.split(',');
  const chainRpc = process.env.CHAIN_RPC;
  const mnemonic = process.env.MNEMONIC;
  const contractAddress = process.env.CONTRACT_ADDRESS;
  const gasFee = process.env.GAS_FEE;

  // Initiate price oracle
  console.log('Getting account for price feeds');
  const wallet = await DirectSecp256k1HdWallet.fromMnemonic(mnemonic);
  const [firstAccount] = await wallet.getAccounts();
  console.log('Initializing Oracle');
  const oracle = new PriceOracle(marketIDs, chainRpc, wallet, firstAccount, contractAddress, gasFee);
  console.log(`Getting prices for: ${marketIDs}`);
  // Start cron job
  cron.schedule(process.env.CRONTAB, () => {
    oracle.getAndPostPrices();
  });
};

main();
