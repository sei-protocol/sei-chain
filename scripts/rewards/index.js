'use strict';

const DEFAULT_PROXY_ADDRESS = '0x28BF6D71b6Dc837F56F5afbF1F4A46AaC0B1f31E';
const IMPLEMENTATION_SLOT = '0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc';
const DISTRIBUTOR_ABI = [
  'function disburseSupplierRewards(address,address,bool)',
  'function disburseBorrowerRewards(address,address,bool)'
];

let cachedEthers;

async function loadEthers() {
  if (!cachedEthers) {
    const mod = await import('ethers');
    cachedEthers = mod.ethers ?? mod.default ?? mod;
  }
  return cachedEthers;
}

function ensure(value, message) {
  if (value === undefined || value === null || value === '') {
    throw new Error(message);
  }
  return value;
}

function normalizeBoolean(value, fallback = false) {
  if (value === undefined || value === null) {
    return fallback;
  }
  if (typeof value === 'boolean') {
    return value;
  }
  if (typeof value === 'number') {
    return value !== 0;
  }
  const normalized = String(value).trim().toLowerCase();
  if (['true', 't', 'yes', 'y', '1'].includes(normalized)) {
    return true;
  }
  if (['false', 'f', 'no', 'n', '0'].includes(normalized)) {
    return false;
  }
  throw new Error(`Unable to interpret boolean value from "${value}"`);
}

async function getProvider(rpcUrl) {
  const ethers = await loadEthers();
  const url = ensure(rpcUrl ?? process.env.RPC_URL, 'RPC URL is required (set RPC_URL)');
  return new ethers.JsonRpcProvider(url);
}

async function getSigner(privateKey, provider) {
  const ethers = await loadEthers();
  const key = ensure(privateKey ?? process.env.PRIVATE_KEY, 'Private key is required (set PRIVATE_KEY)');
  if (!provider) {
    provider = await getProvider();
  }
  return new ethers.Wallet(key, provider);
}

async function fetchImplementationAddress(provider, proxyAddress, slot = IMPLEMENTATION_SLOT) {
  const ethers = await loadEthers();
  const normalizedProxy = ethers.getAddress(proxyAddress ?? DEFAULT_PROXY_ADDRESS);
  const raw = await provider.getStorageAt(normalizedProxy, slot);
  if (!raw) {
    throw new Error(`No storage value returned for slot ${slot}`);
  }
  const stripped = raw.replace(/^0x/i, '').padStart(64, '0');
  const impl = '0x' + stripped.slice(-40);
  return ethers.getAddress(impl);
}

function getMethodForRewardType(rewardType) {
  const normalized = (rewardType ?? '').toString().toLowerCase();
  if (normalized === 'supplier' || normalized === 'suppliers') {
    return 'disburseSupplierRewards';
  }
  if (normalized === 'borrower' || normalized === 'borrowers') {
    return 'disburseBorrowerRewards';
  }
  if (normalized === 'disbursesupplierrewards') {
    return 'disburseSupplierRewards';
  }
  if (normalized === 'disburseborrowerrewards') {
    return 'disburseBorrowerRewards';
  }
  throw new Error('Reward type must be either "borrower" or "supplier"');
}

async function getDistributorContract(signerOrProvider, proxyAddress = DEFAULT_PROXY_ADDRESS) {
  const ethers = await loadEthers();
  const normalizedProxy = ethers.getAddress(proxyAddress);
  return new ethers.Contract(normalizedProxy, DISTRIBUTOR_ABI, signerOrProvider);
}

async function disburseRewards({
  signer,
  provider,
  proxyAddress = DEFAULT_PROXY_ADDRESS,
  rewardType = 'borrower',
  tToken,
  user,
  sendTokens = true,
  waitForReceipt = true
}) {
  if (!signer) {
    if (!provider) {
      provider = await getProvider();
    }
    signer = await getSigner(undefined, provider);
  }
  const ethers = await loadEthers();
  const contract = await getDistributorContract(signer, proxyAddress);
  const method = getMethodForRewardType(rewardType);
  const targetToken = ethers.getAddress(ensure(tToken, 'Market (TTOKEN) address is required'));
  const userAddress = ethers.getAddress(ensure(user, 'User address is required'));
  const shouldSend = normalizeBoolean(sendTokens, true);
  const tx = await contract[method](targetToken, userAddress, shouldSend);
  if (!waitForReceipt) {
    return { tx };
  }
  const receipt = await tx.wait();
  return { tx, receipt };
}

async function encodeDisburseCalldata({
  rewardType = 'borrower',
  tToken,
  user,
  sendTokens = true
}) {
  const ethers = await loadEthers();
  const iface = new ethers.Interface(DISTRIBUTOR_ABI);
  const method = getMethodForRewardType(rewardType);
  const targetToken = ethers.getAddress(ensure(tToken, 'Market (TTOKEN) address is required'));
  const userAddress = ethers.getAddress(ensure(user, 'User address is required'));
  const shouldSend = normalizeBoolean(sendTokens, true);
  return iface.encodeFunctionData(method, [targetToken, userAddress, shouldSend]);
}

module.exports = {
  DEFAULT_PROXY_ADDRESS,
  IMPLEMENTATION_SLOT,
  DISTRIBUTOR_ABI,
  loadEthers,
  getProvider,
  getSigner,
  fetchImplementationAddress,
  getDistributorContract,
  disburseRewards,
  encodeDisburseCalldata,
  normalizeBoolean,
  getMethodForRewardType
};
