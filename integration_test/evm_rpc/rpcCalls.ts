import ExpectStatic = Chai.ExpectStatic;
import {ethers, formatEther, toBeHex} from 'ethers';
import { SigningStargateClient} from '@cosmjs/stargate';
import {createEvmProvider, createEvmWallet, sendFundsFromEvmClient} from './utils/evmUtils';
import {
  associateWallet,
  createSeiProvider,
  createSeiWallet, generateEvmAddressFromMnemonic,
  generateSeiAddressFromMnemonic,
  sendFundsFromSeiClient
} from './utils/seiUtils';
import {DirectSecp256k1HdWallet} from '@cosmjs/proto-signing';
import {fundEvmWallet, fundSeiWallet, waitFor} from './utils/cmdUtils';
import {seiprotocol} from '@sei-js/proto';
import testConfig from './testConfig.json';

/**
 * Runs against local sei chain. Generates two random accounts.
 * Tests some of the rpc calls.
 */

describe('Evm Rpc Calls', function (){
  let expect: ExpectStatic;
  this.timeout( 4 * 60 * 1000);
  const evmRpc = testConfig.evmRpc
  const seiRpc = testConfig.seiRpc;
  let evmClient: ethers.JsonRpcProvider;
  let seiClient: SigningStargateClient;
  let seiWallet: DirectSecp256k1HdWallet;
  let evmWallet: ethers.HDNodeWallet;
  let evmAddress: string;
  let seiAddress: string;
  let evmWalletSeiAddress: string;
  const feeAccount = testConfig.feeAccount;
  const chainId = testConfig.chainId;

  before('Tests set up', async () =>{
    const chai = await import('chai');
    ({expect} = chai);
    seiWallet = await createSeiWallet();
    evmClient = await createEvmProvider(evmRpc);
    seiClient = await createSeiProvider(seiRpc, seiWallet);
    evmWallet = await createEvmWallet(evmClient);
    evmWalletSeiAddress = await generateSeiAddressFromMnemonic(evmWallet);
    await fundEvmWallet(evmWallet, evmRpc);
    await fundSeiWallet(seiWallet);
    await fundSeiWallet(evmWalletSeiAddress);
    await waitFor(2);
    evmAddress = await evmWallet.getAddress();
    seiAddress = (await seiWallet.getAccounts())[0].address;
  })

  describe('Association rpc tests', function (){
    let txHash: string;

    it('Evm users can associate accounts', async () =>{
      await associateWallet(evmClient, evmWallet);
      const queriedSeiAddress = await evmClient.send('sei_getSeiAddress', [evmAddress]);
      const seiQueryClient = await seiprotocol.ClientFactory
        .createRPCQueryClient({rpcEndpoint: seiRpc});
      const {associated} = await seiQueryClient.seiprotocol.seichain.evm
        .seiAddressByEVMAddress({evmAddress: evmAddress});
      const expectedSeiAddress = await generateSeiAddressFromMnemonic(evmWallet);

      expect(associated).to.be.true;
      expect(queriedSeiAddress).to.be.eq(expectedSeiAddress);
    });

    it('Sei wallet can implicitly associate accounts', async () =>{
      const expectedSeiAddress = await generateSeiAddressFromMnemonic(evmWallet);
      txHash = await sendFundsFromSeiClient(seiClient, seiAddress, expectedSeiAddress);
      await waitFor(1);
      const seiQueryClient = await seiprotocol.ClientFactory.createRPCQueryClient({rpcEndpoint: seiRpc});
      const {associated} = await seiQueryClient.seiprotocol.seichain.evm.eVMAddressBySeiAddress({seiAddress: seiAddress});

      const queriedEvmAddress = await evmClient.send('sei_getEVMAddress', [seiAddress]);
      const expectedEvmAddress = await generateEvmAddressFromMnemonic(seiWallet);

      expect(associated).to.be.true;
      expect(queriedEvmAddress).to.be.eq(expectedEvmAddress);
    });

    it('Users can query evm tx', async () =>{
      const evmAddress = await generateEvmAddressFromMnemonic(seiWallet);
      const [txHash,blockNumber, blockHash] =
        await sendFundsFromEvmClient(evmWallet, evmAddress);
      await waitFor(1);

      const txRecord = await evmClient.send('sei_getCosmosTx', [txHash]);
      expect(txRecord).not.to.be.null;

      const txRecordOnCosmos = await evmClient.send('sei_getEvmTx', [txRecord]);
      expect(txRecordOnCosmos).not.to.be.null;
    });
  });

  /**
   * eth_getBlockReceipts
   * eth_getBlockTransactionCountByNumber
   * eth_getBlockTransactionCountByHash
   * eth_getBlockByHash
   * eth_getBlockByNumber
   */
  describe('Block json rpc calls test', function (){
    let blockNumber: string;
    let blockHash: string;
    let txHash: string;
    let evmSeiAddress: string;

    it('Users will get the block receipts with block number', async () =>{
      evmSeiAddress = await generateEvmAddressFromMnemonic(seiWallet);
      ([txHash, blockNumber, blockHash] =
        await sendFundsFromEvmClient(evmWallet, evmSeiAddress) as string[]);
      const [txReceipt] = await evmClient.send('eth_getBlockReceipts', [ethers.toQuantity(blockNumber)]);

      expect(txReceipt).to.haveOwnProperty('blockHash');
      expect(txReceipt).to.haveOwnProperty('blockNumber');
      expect(txReceipt).to.haveOwnProperty('gasUsed');

      expect(txReceipt.transactionHash).to.be.eq(txHash);
      expect(txReceipt.from).to.be.eq(evmAddress.toLowerCase());
      expect(txReceipt.to).to.be.eq(evmSeiAddress.toLowerCase());
    });

    it('Users will get the block receipts with block hash', async () =>{
      const [txReceipt] = await evmClient.send('eth_getBlockReceipts', [blockHash]);

      expect(txReceipt).to.haveOwnProperty('blockHash');
      expect(txReceipt).to.haveOwnProperty('blockNumber');
      expect(txReceipt).to.haveOwnProperty('gasUsed');

      expect(txReceipt.transactionHash).to.be.eq(txHash);
      expect(txReceipt.from).to.be.eq(evmAddress.toLowerCase());
      expect(txReceipt.to).to.be.eq(evmSeiAddress.toLowerCase());
    });

    it('Users will see get block transaction count by number', async () =>{
      const txCount = await evmClient.send('eth_getBlockTransactionCountByNumber', [ethers.toQuantity(blockNumber)]);
      expect(parseInt(txCount)).to.be.eq(1)
    });

    it('Users will see transaction count by hash', async () =>{
      const txCount = await evmClient.send('eth_getBlockTransactionCountByHash', [blockHash]);
      expect(parseInt(txCount)).to.be.eq(1)
    });

    it('Users will get block details by hash', async () =>{
      const block =  await evmClient.send('eth_getBlockByHash', [blockHash, true]);

      expect(block.transactions).to.have.length(1);
      expect(block.hash).to.be.eq(blockHash);
      expect(parseInt(block.number)).to.be.eq(parseInt(blockNumber));
    });

    it('Users will get block details by number', async () =>{
      const block =  await evmClient.send('eth_getBlockByNumber', [ethers.toQuantity(blockNumber), false]);

      expect(block.transactions).to.have.length(1);
      expect(block.hash).to.be.eq(blockHash);
      expect(parseInt(block.number)).to.be.eq(parseInt(blockNumber));
    });
  });

  /**
   * eth_BlockNumber
   * eth_ChainId
   * eth_Coinbase
   * eth_Accounts
   * eth_GasPrice
   * eth_feeHistory
   * eth_maxPriorityFeePerGas
   */
  describe('Users can query the chain info through json rpc calls', function() {

    it('Users can query latest block number', async () =>{
      const blockNumber = await evmClient.send('eth_blockNumber', []);
      expect(parseInt(blockNumber)).to.be.above(0);
    });

    it('Users can query the fee account', async () =>{
      const coinbase = await evmClient.send('eth_coinbase', []);
      expect(coinbase).to.be.eq(feeAccount);
    });

    it('Users can query chain id', async () =>{
      const queriedChainId = await evmClient.send('eth_chainId', []);
      expect(parseInt(queriedChainId)).to.be.eq(chainId);
    });

    it('Users can query accounts', async () =>{
      const accounts = await evmClient.send('eth_accounts', []);
      expect(accounts.length).to.be.eq(21);
    });

    it('Users can query gas price', async () =>{
      const gasPrice = await evmClient.send('eth_gasPrice', []);
      expect(parseInt(gasPrice)).to.be.above(990000000);
    });

    it('Users can query fee history', async () =>{
      const blockCount = 10; //
      const lastBlock = await evmClient.getBlockNumber();
      const rewardPercentiles = [10.0];
      const feeHistory = await evmClient.send('eth_feeHistory',
        [ethers.toQuantity(blockCount), ethers.toQuantity(lastBlock), rewardPercentiles]);

      expect(Number(feeHistory.oldestBlock)).to.be.eq((lastBlock - blockCount) + 1);
      expect(feeHistory.baseFeePerGas).to.have.length(blockCount);
      expect(feeHistory.gasUsedRatio).to.have.length(blockCount);
    });

    it('Users can query max priority fee per gas', async () =>{
      const maxPriority = await evmClient.send('eth_maxPriorityFeePerGas', []);
      expect(Number(maxPriority)).to.be.gte(0);
    })
  })

  /**
   * eth_getNonce
   * eth_getBalance
   * eth_getBlockByNumber
   *
   */
  describe('State rpc call tests', () =>{
    let latestBlockNumber: number;
    let latestBalance: string;
    let previousBalance: string;
    let previousNonce: number;

    before('Gets the latest block number', async () =>{
      latestBlockNumber = await evmClient.getBlockNumber();
      previousNonce = await evmClient.send('eth_getNonce', [evmAddress]);
    });

    it('Users can query their balance with block number', async() =>{
      const userBalance = await evmClient.getBalance(evmAddress);
      latestBalance = await evmClient.send('eth_getBalance', [evmAddress,ethers.toQuantity(latestBlockNumber)]);
      expect(userBalance.toString()).to.be.eq(BigInt(latestBalance).toString());
    });

    it('Users can query their balance from a previous block number', async () =>{
      previousBalance = await evmClient.send('eth_getBalance', [evmAddress, ethers.toQuantity(latestBlockNumber - 3)]);
      expect(previousBalance).not.to.be.eq('0x');
    });

    it('Users can query their balance with block hash', async() =>{
      const {hash} =  await evmClient.send('eth_getBlockByNumber', [ethers.toQuantity(latestBlockNumber), false]);
      const latestBalanceWithHash = await evmClient.send('eth_getBalance', [evmAddress, hash]);
      expect(latestBalanceWithHash).to.be.eq(latestBalance);
    });

    it('Users can query their balance from a previous block with a hash', async () =>{
      const {hash} =  await evmClient.send('eth_getBlockByNumber', [ethers.toQuantity(latestBlockNumber - 3), false]);
      const previousBalanceWithHash = await evmClient.send('eth_getBalance', [evmAddress, hash]);
      expect(previousBalanceWithHash).to.be.eq(previousBalance);
    });

    it('Users can query balance after transfers', async () =>{
      const seiEvmAddress = await generateEvmAddressFromMnemonic(seiWallet);
      const [hash, blockNumberOfTransfer] =
        await sendFundsFromEvmClient(evmWallet, seiEvmAddress) as [string, string];
      await waitFor(2);
      const latestBalanceAfterTransfer = await evmClient.send('eth_getBalance', [evmAddress, 'latest']);
      const balanceDifference = BigInt(latestBalance) - BigInt(latestBalanceAfterTransfer);
      const difference = formatEther(balanceDifference.toString());
      expect(Number(difference)).to.be.gte(0.1);

      const previousBalanceOnTransferBlock = await evmClient.send('eth_getBalance',
        [evmAddress, ethers.toQuantity(Number(blockNumberOfTransfer) - 1)]);
      expect(previousBalanceOnTransferBlock).to.be.eq(latestBalance);
    });

    //ToDo Check the code get in detail
    it.skip('Users can query byte code of a contract', async () =>{
      const ibcPrecompileAddress = '0x0000000000000000000000000000000000001009';
      const byteCode = await evmClient.send('eth_getCode', [ibcPrecompileAddress, 'latest']);
      console.log(byteCode);
    });


    it('Users can query next nonce', async () =>{
      const nonce = await evmClient.send('eth_getNonce', [evmAddress]);
      expect(nonce).to.be.eq(previousNonce + 1);
    });
  });

  /**
   * eth_getTransactionReceipt
   * eth_getTransactionByHash
   * eth_getTransactionByBlockNumberAndIndex
   * eth_getTransactionByBlockHashAndIndex
   * eth_getTransactionCount
   */
  describe('Tx Rpc Tests', function () {
    let blockNumber: string;
    let blockHash: string;
    let evmSeiAddress: string;
    let txHash: string;
    let usertxCount: string;

    it('Users can get transaction receipt', async () =>{
      evmSeiAddress = await generateEvmAddressFromMnemonic(seiWallet);
      const [txHash] = await sendFundsFromEvmClient(evmWallet, evmSeiAddress);
      await waitFor(1);
      const receipt = await evmClient.send('eth_getTransactionReceipt', [txHash]);

      blockNumber = receipt.blockNumber;
      blockHash = receipt.blockHash;

      expect(receipt).to.haveOwnProperty('blockHash');
      expect(receipt).to.haveOwnProperty('blockNumber');
      expect(receipt).to.haveOwnProperty('gasUsed');

      expect(receipt.from).to.be.eq(evmAddress.toLowerCase());
      expect(receipt.to).to.be.eq(evmSeiAddress.toLowerCase());
    });

    it('Users can get transaction by hash', async () =>{
      ([txHash] = await sendFundsFromEvmClient(evmWallet, evmSeiAddress) as [string]);
      await waitFor(1);
      const txDetails = await evmClient.send('eth_getTransactionByHash', [txHash]);
      expect(txDetails).to.haveOwnProperty('blockHash');
      expect(txDetails).to.haveOwnProperty('blockNumber');

      expect(txDetails.from).to.be.eq(evmAddress.toLowerCase());
      expect(txDetails.to).to.be.eq(evmSeiAddress.toLowerCase());
    });

    it('Users can query tx from block number and index', async () =>{
      const txDetails = await evmClient.send('eth_getTransactionByBlockNumberAndIndex',
        [ethers.toQuantity(blockNumber), ethers.toQuantity(0)]);

      expect(txDetails).to.haveOwnProperty('blockHash');
      expect(txDetails).to.haveOwnProperty('blockNumber');

      expect(txDetails.from).to.be.eq(evmAddress.toLowerCase());
      expect(txDetails.to).to.be.eq(evmSeiAddress.toLowerCase());
    });

    it('Users can query tx from block hash and index', async () =>{
      const txDetails = await evmClient.send('eth_getTransactionByBlockHashAndIndex',
        [blockHash, ethers.toQuantity(0)]);

      expect(txDetails).to.haveOwnProperty('blockHash');
      expect(txDetails).to.haveOwnProperty('blockNumber');

      expect(txDetails.from).to.be.eq(evmAddress.toLowerCase());
      expect(txDetails.to).to.be.eq(evmSeiAddress.toLowerCase());
    });

    it('Users cant query unexisting indexes', async () =>{
      const txDetails = await evmClient.send('eth_getTransactionByBlockHashAndIndex', [blockHash, ethers.toQuantity(5)]);
      expect(txDetails).to.be.null;
    });

    it('Users can query tx count from block number', async () =>{
      usertxCount = await evmClient.send('eth_getTransactionCount', [evmAddress, ethers.toQuantity(blockNumber)]);
      expect(parseInt(usertxCount)).to.be.above(1);
    });

    it('Users can query tx count from block hash', async () =>{
      const txCount = await evmClient.send('eth_getTransactionCount', [evmAddress, blockHash]);
      expect(parseInt(txCount)).to.be.above(1);
      expect(parseInt(usertxCount)).to.be.eq(parseInt(txCount));
    });
  });

})