/**
 * Deploy the allowlisted contracts used by Pacific semantic replay.
 *
 *   TARGET_NETWORK=<target> EXECUTE=1 npm run replay:deploy
 */
import fs from 'fs/promises';
import path from 'path';
import { ethers } from 'ethers';
import { loadDeployConfig, loadTargetConfig, verifyTargetRpc } from './config';
import { minBigInt } from './numeric';
import {
    REPLAY_DEPLOYMENT_SCHEMA_VERSION,
    REPLAY_CONTRACT_KEYS,
    ReplayDeploymentManifest,
} from './replay/replayTypes';
import { writeJsonAtomic } from './io';
import {
    SUSHI_V2_PROVENANCE,
    validateSushiV2Provenance,
} from './sushiV2';

interface Artifact {
    abi: ethers.InterfaceAbi;
    bytecode: string;
}

const deployConfig = loadDeployConfig();
const { execute: EXECUTE, forceDeploy: FORCE_DEPLOY } = deployConfig;

export async function deployFixturesMain(): Promise<void> {
    const target = loadTargetConfig();
    console.log(`Replay fixtures: ${target.network} (${target.evmChainId})`);
    console.log(`Manifest: ${target.deploymentPath}`);
    if (!EXECUTE) {
        console.log('Dry-run only. Set EXECUTE=1 to deploy.');
        return;
    }
    if (!target.mnemonic) {
        throw new Error('TARGET_MNEMONIC or SEI_ADMIN_MNEMONIC is required for deployment');
    }

    const provider = new ethers.JsonRpcProvider(target.evmRpcUrl);
    provider.pollingInterval = 200;
    await verifyTargetRpc(target, provider);
    const artifacts = await loadArtifacts();
    validateSushiArtifacts(artifacts);
    const existing = await readExisting(target.deploymentPath);
    if (existing && !FORCE_DEPLOY) {
        await verifyExisting(existing, target.network, target.evmChainId, provider, artifacts);
        console.log('Existing deployment is valid; set FORCE_DEPLOY=1 to replace it.');
        provider.destroy();
        return;
    }

    const admin = ethers.HDNodeWallet.fromPhrase(
        target.mnemonic,
        '',
        "m/44'/118'/0'/0/0",
    ).connect(provider);
    const adminAddress = admin.address;
    let nextNonce = await provider.getTransactionCount(adminAddress, 'pending');
    const takeNonce = () => nextNonce++;
    const balance = await provider.getBalance(adminAddress);
    if (balance === 0n) throw new Error(`Deployment account ${adminAddress} has no balance`);
    console.log(`Deploying from ${adminAddress} (${ethers.formatEther(balance)} SEI)`);

    const weth = await deploy('WETH9', admin, artifacts.weth, takeNonce());
    const factory = await deploy(
        'UniswapV2Factory',
        admin,
        artifacts.factory,
        takeNonce(),
        [adminAddress],
    );
    const router = await deploy(
        'UniswapV2Router02',
        admin,
        artifacts.router,
        takeNonce(),
        [await factory.getAddress(), await weth.getAddress()],
    );
    const tokenA = await deploy(
        'ReplayTokenA',
        admin,
        artifacts.token,
        takeNonce(),
        [adminAddress],
    );
    const tokenB = await deploy(
        'ReplayTokenB',
        admin,
        artifacts.token,
        takeNonce(),
        [adminAddress],
    );
    const nft = await deploy('ReplayNFT', admin, artifacts.nft, takeNonce(), [adminAddress]);
    const erc1155 = await deploy('ReplayERC1155', admin, artifacts.erc1155, takeNonce());
    const profileHarness = await deploy(
        'ProfileLoadHarness',
        admin,
        artifacts.harness,
        takeNonce(),
    );
    const callGraphHarness = await deploy(
        'CallGraphHarness',
        admin,
        artifacts.callGraph,
        takeNonce(),
    );
    const syntheticCreationHarness = await deploy(
        'SyntheticCreationHarness',
        admin,
        artifacts.syntheticCreation,
        takeNonce(),
    );
    const callGraphContract = new ethers.Contract(
        await callGraphHarness.getAddress(),
        artifacts.callGraph.abi,
        admin,
    );
    const callGraphNode = (await callGraphContract.node()) as string;
    const tokenAContract = new ethers.Contract(await tokenA.getAddress(), artifacts.token.abi, admin);
    const tokenBContract = new ethers.Contract(await tokenB.getAddress(), artifacts.token.abi, admin);
    const routerContract = new ethers.Contract(await router.getAddress(), artifacts.router.abi, admin);
    const factoryContract = new ethers.Contract(await factory.getAddress(), artifacts.factory.abi, admin);
    const tokenAAddress = await tokenA.getAddress();
    const tokenBAddress = await tokenB.getAddress();
    const expectedPair = expectedPairAddress(
        await factory.getAddress(),
        tokenAAddress,
        tokenBAddress,
    );
    const supply = ethers.parseEther(deployConfig.tokenSupply);
    const liquidity = ethers.parseEther(deployConfig.liquidity);
    await (await tokenAContract.mint(adminAddress, supply, { nonce: takeNonce() })).wait();
    await (await tokenBContract.mint(adminAddress, supply, { nonce: takeNonce() })).wait();
    await (
        await tokenAContract.approve(await router.getAddress(), ethers.MaxUint256, {
            nonce: takeNonce(),
        })
    ).wait();
    await (
        await tokenBContract.approve(await router.getAddress(), ethers.MaxUint256, {
            nonce: takeNonce(),
        })
    ).wait();
    await (
        await routerContract.addLiquidity(
            await tokenA.getAddress(),
            await tokenB.getAddress(),
            liquidity,
            liquidity,
            0,
            0,
            adminAddress,
            Math.floor(Date.now() / 1_000) + 3_600,
            { nonce: takeNonce() },
        )
    ).wait();
    const pair = await factoryContract.getPair(
        tokenAAddress,
        tokenBAddress,
    );
    if (pair === ethers.ZeroAddress) throw new Error('Replay pair was not created');
    if ((pair as string).toLowerCase() !== expectedPair.toLowerCase()) {
        throw new Error(`Replay pair ${pair} does not match canonical CREATE2 address ${expectedPair}`);
    }

    const proxyErc20Implementation = await deploy(
        'ProxyERC20Implementation',
        admin,
        artifacts.proxyErc20,
        takeNonce(),
    );
    const proxyErc20Address = await proxyErc20Implementation.getAddress();
    const proxyToken = async (name: string, symbol: string): Promise<ethers.BaseContract> =>
        deploy(name, admin, artifacts.proxy, takeNonce(), [
            adminAddress,
            proxyErc20Address,
            new ethers.Interface(artifacts.proxyErc20.abi).encodeFunctionData('initialize', [
                name,
                symbol,
                18,
                adminAddress,
            ]),
        ]);
    const dexOutputToken = await proxyToken('Replay DEX Output', 'rDEX');
    const farmRewardToken = await proxyToken('Replay Farm Reward', 'rFARM');
    const lendingReceiptToken = await proxyToken('Replay Lending Receipt', 'rLEND');
    const liquidStakingReceiptToken = await proxyToken('Replay Staked Receipt', 'rSTAKE');

    const dexOutputAddress = await dexOutputToken.getAddress();
    const [poolToken0, poolToken1] =
        tokenAAddress.toLowerCase() < dexOutputAddress.toLowerCase()
            ? [tokenAAddress, dexOutputAddress]
            : [dexOutputAddress, tokenAAddress];
    const fixtureSeed = minBigInt(liquidity, ethers.parseEther('1000000'));
    const maxSwapAmount = ethers.parseEther('100');
    const v3Pool = await deploy('DeterministicV3Pool', admin, artifacts.v3Pool, takeNonce(), [
        poolToken0,
        poolToken1,
        30,
        maxSwapAmount,
    ]);
    const v3Router = await deploy(
        'ProductionShapedSwapRouter',
        admin,
        artifacts.v3Router,
        takeNonce(),
        [adminAddress],
    );
    const v3RouterContract = new ethers.Contract(
        await v3Router.getAddress(),
        artifacts.v3Router.abi,
        admin,
    );
    await waitFor(
        v3RouterContract.configurePool(
            tokenAAddress,
            dexOutputAddress,
            3000,
            await v3Pool.getAddress(),
            { nonce: takeNonce() },
        ),
    );
    await waitFor(
        tokenAContract.transfer(await v3Pool.getAddress(), fixtureSeed, { nonce: takeNonce() }),
    );
    const dexOutputContract = new ethers.Contract(
        dexOutputAddress,
        artifacts.proxyErc20.abi,
        admin,
    );
    await waitFor(
        dexOutputContract.mint(await v3Pool.getAddress(), fixtureSeed, { nonce: takeNonce() }),
    );

    const masterChef = await deploy(
        'DeterministicMasterChef',
        admin,
        artifacts.masterChef,
        takeNonce(),
        [adminAddress, await farmRewardToken.getAddress()],
    );
    const farmRewardContract = new ethers.Contract(
        await farmRewardToken.getAddress(),
        artifacts.proxyErc20.abi,
        admin,
    );
    await waitFor(
        farmRewardContract.setMinter(await masterChef.getAddress(), true, { nonce: takeNonce() }),
    );
    const masterChefContract = new ethers.Contract(
        await masterChef.getAddress(),
        artifacts.masterChef.abi,
        admin,
    );
    await waitFor(
        masterChefContract.addPool(tokenBAddress, ethers.parseEther('0.01'), {
            nonce: takeNonce(),
        }),
    );

    const lendingOracle = await deploy(
        'DeterministicPriceOracle',
        admin,
        artifacts.lendingOracle,
        takeNonce(),
        [adminAddress],
    );
    const lendingRateModel = await deploy(
        'DeterministicRateModel',
        admin,
        artifacts.lendingRate,
        takeNonce(),
        [1_000_000_000n, 5_000_000_000n],
    );
    const lendingComptroller = await deploy(
        'FixtureComptroller',
        admin,
        artifacts.lendingComptroller,
        takeNonce(),
        [await lendingOracle.getAddress(), ethers.parseEther('0.75')],
    );
    const lendingImplementation = await deploy(
        'LendingPoolImplementation',
        admin,
        artifacts.lendingImplementation,
        takeNonce(),
    );
    const lendingPool = await deploy('LendingPoolProxy', admin, artifacts.proxy, takeNonce(), [
        adminAddress,
        await lendingImplementation.getAddress(),
        new ethers.Interface(artifacts.lendingImplementation.abi).encodeFunctionData('initialize', [
            tokenAAddress,
            await lendingReceiptToken.getAddress(),
            await lendingComptroller.getAddress(),
            await lendingOracle.getAddress(),
            await lendingRateModel.getAddress(),
        ]),
    ]);
    const lendingReceiptContract = new ethers.Contract(
        await lendingReceiptToken.getAddress(),
        artifacts.proxyErc20.abi,
        admin,
    );
    await waitFor(
        lendingReceiptContract.setMinter(await lendingPool.getAddress(), true, {
            nonce: takeNonce(),
        }),
    );
    const lendingOracleContract = new ethers.Contract(
        await lendingOracle.getAddress(),
        artifacts.lendingOracle.abi,
        admin,
    );
    await waitFor(
        lendingOracleContract.setPrice(tokenAAddress, ethers.parseEther('1'), {
            nonce: takeNonce(),
        }),
    );
    await waitFor(
        tokenAContract.approve(await lendingPool.getAddress(), ethers.MaxUint256, {
            nonce: takeNonce(),
        }),
    );
    const lendingContract = new ethers.Contract(
        await lendingPool.getAddress(),
        artifacts.lendingImplementation.abi,
        admin,
    );
    await waitFor(
        lendingContract.supply(tokenAAddress, fixtureSeed, adminAddress, 0, {
            nonce: takeNonce(),
        }),
    );

    const exchangeRateOracle = await deploy(
        'DeterministicExchangeRateOracle',
        admin,
        artifacts.exchangeRateOracle,
        takeNonce(),
        [adminAddress, ethers.parseEther('1')],
    );
    const liquidStakingImplementation = await deploy(
        'LiquidStakingImplementation',
        admin,
        artifacts.liquidStakingImplementation,
        takeNonce(),
    );
    const liquidStaking = await deploy(
        'LiquidStakingProxy',
        admin,
        artifacts.proxy,
        takeNonce(),
        [
            adminAddress,
            await liquidStakingImplementation.getAddress(),
            new ethers.Interface(artifacts.liquidStakingImplementation.abi).encodeFunctionData(
                'initialize',
                [
                    tokenBAddress,
                    await liquidStakingReceiptToken.getAddress(),
                    await exchangeRateOracle.getAddress(),
                    60,
                ],
            ),
        ],
    );
    const liquidReceiptContract = new ethers.Contract(
        await liquidStakingReceiptToken.getAddress(),
        artifacts.proxyErc20.abi,
        admin,
    );
    await waitFor(
        liquidReceiptContract.setMinter(await liquidStaking.getAddress(), true, {
            nonce: takeNonce(),
        }),
    );
    await waitFor(
        tokenBContract.approve(await liquidStaking.getAddress(), ethers.MaxUint256, {
            nonce: takeNonce(),
        }),
    );
    const liquidStakingContract = new ethers.Contract(
        await liquidStaking.getAddress(),
        artifacts.liquidStakingImplementation.abi,
        admin,
    );
    await waitFor(liquidStakingContract.stake(fixtureSeed, { nonce: takeNonce() }));

    const strategyModule = await deploy(
        'NamespacedStrategyModule',
        admin,
        artifacts.strategyModule,
        takeNonce(),
    );
    const strategyAdapter = await deploy(
        'DeterministicStrategyAdapter',
        admin,
        artifacts.strategyAdapter,
        takeNonce(),
        [tokenAAddress, adminAddress],
    );
    const strategyVaultImplementation = await deploy(
        'StrategyVaultImplementation',
        admin,
        artifacts.strategyVaultImplementation,
        takeNonce(),
    );
    const strategyVault = await deploy(
        'StrategyVaultProxy',
        admin,
        artifacts.proxy,
        takeNonce(),
        [
            adminAddress,
            await strategyVaultImplementation.getAddress(),
            new ethers.Interface(artifacts.strategyVaultImplementation.abi).encodeFunctionData(
                'initialize',
                [
                    tokenAAddress,
                    adminAddress,
                    await strategyModule.getAddress(),
                    await strategyAdapter.getAddress(),
                    5000,
                    'Replay Strategy Vault',
                    'rVAULT',
                ],
            ),
        ],
    );
    const strategyAdapterContract = new ethers.Contract(
        await strategyAdapter.getAddress(),
        artifacts.strategyAdapter.abi,
        admin,
    );
    await waitFor(
        strategyAdapterContract.setController(await strategyVault.getAddress(), {
            nonce: takeNonce(),
        }),
    );
    await waitFor(
        tokenAContract.approve(await strategyVault.getAddress(), ethers.MaxUint256, {
            nonce: takeNonce(),
        }),
    );
    const strategyVaultContract = new ethers.Contract(
        await strategyVault.getAddress(),
        artifacts.strategyVaultImplementation.abi,
        admin,
    );
    await waitFor(
        strategyVaultContract.deposit(fixtureSeed, adminAddress, { nonce: takeNonce() }),
    );

    const contracts = {
        weth: await weth.getAddress(),
        factory: await factory.getAddress(),
        router: await router.getAddress(),
        tokenA: await tokenA.getAddress(),
        tokenB: await tokenB.getAddress(),
        pair,
        nft: await nft.getAddress(),
        erc1155: await erc1155.getAddress(),
        profileHarness: await profileHarness.getAddress(),
        callGraphHarness: await callGraphHarness.getAddress(),
        callGraphNode,
        syntheticCreationHarness: await syntheticCreationHarness.getAddress(),
        proxyErc20Implementation: proxyErc20Address,
        dexOutputTokenProxy: dexOutputAddress,
        v3Pool: await v3Pool.getAddress(),
        v3Router: await v3Router.getAddress(),
        farmRewardTokenProxy: await farmRewardToken.getAddress(),
        masterChef: await masterChef.getAddress(),
        lendingReceiptTokenProxy: await lendingReceiptToken.getAddress(),
        lendingOracle: await lendingOracle.getAddress(),
        lendingRateModel: await lendingRateModel.getAddress(),
        lendingComptroller: await lendingComptroller.getAddress(),
        lendingImplementation: await lendingImplementation.getAddress(),
        lendingPoolProxy: await lendingPool.getAddress(),
        liquidStakingReceiptTokenProxy: await liquidStakingReceiptToken.getAddress(),
        exchangeRateOracle: await exchangeRateOracle.getAddress(),
        liquidStakingImplementation: await liquidStakingImplementation.getAddress(),
        liquidStakingProxy: await liquidStaking.getAddress(),
        strategyModule: await strategyModule.getAddress(),
        strategyAdapter: await strategyAdapter.getAddress(),
        strategyVaultImplementation: await strategyVaultImplementation.getAddress(),
        strategyVaultProxy: await strategyVault.getAddress(),
    };
    await verifyWiring(contracts, artifacts, provider);
    const codeHashes = Object.fromEntries(
        await Promise.all(
            Object.entries(contracts).map(async ([name, address]) => [
                name,
                ethers.keccak256(await provider.getCode(address)),
            ]),
        ),
    );
    const manifest = {
        schemaVersion: REPLAY_DEPLOYMENT_SCHEMA_VERSION,
        network: target.network,
        chainId: Number(target.evmChainId),
        deployedAt: new Date().toISOString(),
        deployer: adminAddress,
        sushiV2: SUSHI_V2_PROVENANCE,
        contracts,
        codeHashes,
    };
    await fs.mkdir(path.dirname(target.deploymentPath), { recursive: true });
    await writeJsonAtomic(target.deploymentPath, manifest);
    console.log(`Saved replay deployment to ${target.deploymentPath}`);
    provider.destroy();
}

async function loadArtifacts(): Promise<Record<string, Artifact>> {
    const files = {
        factory: 'vendor/sushiswap-v2/artifacts/UniswapV2Factory.json',
        router: 'vendor/sushiswap-v2/artifacts/UniswapV2Router02.json',
        pair: 'vendor/sushiswap-v2/artifacts/UniswapV2Pair.json',
        weth: 'artifacts/contracts/mocks/WETH9.sol/WETH9.json',
        token: 'artifacts/contracts/TestERC20.sol/TestERC20.json',
        nft: 'artifacts/contracts/TestNFT.sol/TestNFT.json',
        erc1155: 'artifacts/contracts/TestERC1155.sol/TestERC1155.json',
        harness: 'artifacts/contracts/ProfileLoadHarness.sol/ProfileLoadHarness.json',
        callGraph: 'artifacts/contracts/CallGraphHarness.sol/CallGraphHarness.json',
        syntheticCreation:
            'artifacts/contracts/SyntheticCreationHarness.sol/SyntheticCreationHarness.json',
        proxy: 'artifacts/contracts/fixtures/FixtureCore.sol/Allowlisted1967Proxy.json',
        proxyErc20:
            'artifacts/contracts/fixtures/ProxyERC20Fixture.sol/ProxyERC20Implementation.json',
        v3Pool: 'artifacts/contracts/fixtures/CallbackDexFixture.sol/DeterministicV3Pool.json',
        v3Router:
            'artifacts/contracts/fixtures/CallbackDexFixture.sol/ProductionShapedSwapRouter.json',
        masterChef:
            'artifacts/contracts/fixtures/MasterChefFixture.sol/DeterministicMasterChef.json',
        lendingOracle:
            'artifacts/contracts/fixtures/LendingFixture.sol/DeterministicPriceOracle.json',
        lendingRate:
            'artifacts/contracts/fixtures/LendingFixture.sol/DeterministicRateModel.json',
        lendingComptroller:
            'artifacts/contracts/fixtures/LendingFixture.sol/FixtureComptroller.json',
        lendingImplementation:
            'artifacts/contracts/fixtures/LendingFixture.sol/LendingPoolImplementation.json',
        exchangeRateOracle:
            'artifacts/contracts/fixtures/LiquidStakingFixture.sol/DeterministicExchangeRateOracle.json',
        liquidStakingImplementation:
            'artifacts/contracts/fixtures/LiquidStakingFixture.sol/LiquidStakingImplementation.json',
        strategyModule:
            'artifacts/contracts/fixtures/StrategyVaultFixture.sol/NamespacedStrategyModule.json',
        strategyAdapter:
            'artifacts/contracts/fixtures/StrategyVaultFixture.sol/DeterministicStrategyAdapter.json',
        strategyVaultImplementation:
            'artifacts/contracts/fixtures/StrategyVaultFixture.sol/StrategyVaultImplementation.json',
    };
    return Object.fromEntries(
        await Promise.all(Object.entries(files).map(async ([name, file]) => [name, await artifact(file)])),
    );
}

function validateSushiArtifacts(artifacts: Record<string, Artifact>): void {
    if (ethers.keccak256(artifacts.pair.bytecode) !== SUSHI_V2_PROVENANCE.pairInitCodeHash) {
        throw new Error('Canonical SushiSwap V2 pair init-code hash does not match provenance');
    }
    if (
        ethers.keccak256(artifacts.factory.bytecode) !==
            SUSHI_V2_PROVENANCE.factoryCreationCodeHash ||
        ethers.keccak256(artifacts.router.bytecode) !==
            SUSHI_V2_PROVENANCE.routerCreationCodeHash
    ) {
        throw new Error('Canonical SushiSwap V2 deployment artifacts do not match provenance');
    }
    for (const name of ['factory', 'router', 'pair'] as const) {
        if (artifacts[name].bytecode.includes('__$')) {
            throw new Error(`SushiSwap V2 ${name} artifact contains unresolved library links`);
        }
    }
}

function expectedPairAddress(factory: string, tokenA: string, tokenB: string): string {
    const [token0, token1] =
        tokenA.toLowerCase() < tokenB.toLowerCase() ? [tokenA, tokenB] : [tokenB, tokenA];
    const salt = ethers.keccak256(ethers.solidityPacked(['address', 'address'], [token0, token1]));
    return ethers.getCreate2Address(factory, salt, SUSHI_V2_PROVENANCE.pairInitCodeHash);
}

async function waitFor(transaction: Promise<ethers.ContractTransactionResponse>): Promise<void> {
    await (await transaction).wait();
}

async function verifyWiring(
    contracts: Record<string, string>,
    artifacts: Record<string, Artifact>,
    provider: ethers.Provider,
): Promise<void> {
    for (const [name, address] of Object.entries(contracts)) {
        if ((await provider.getCode(address)) === '0x') throw new Error(`${name} has no deployed code`);
    }
    const runner = provider;
    const factory = new ethers.Contract(contracts.factory, artifacts.factory.abi, runner);
    const router = new ethers.Contract(contracts.router, artifacts.router.abi, runner);
    const pair = new ethers.Contract(contracts.pair, artifacts.pair.abi, runner);
    if (
        ((await router.factory()) as string).toLowerCase() !== contracts.factory.toLowerCase() ||
        ((await router.WETH()) as string).toLowerCase() !== contracts.weth.toLowerCase() ||
        (await factory.pairCodeHash()) !== SUSHI_V2_PROVENANCE.pairInitCodeHash ||
        ((await factory.getPair(contracts.tokenA, contracts.tokenB)) as string).toLowerCase() !==
            contracts.pair.toLowerCase() ||
        ((await pair.factory()) as string).toLowerCase() !== contracts.factory.toLowerCase() ||
        (await pair.name()) !== 'SushiSwap LP Token' ||
        (await pair.symbol()) !== 'SLP'
    ) {
        throw new Error('SushiSwap V2 wiring is invalid');
    }
    const pairTokens = [await pair.token0(), await pair.token1()].map(value =>
        (value as string).toLowerCase(),
    );
    if (
        !pairTokens.includes(contracts.tokenA.toLowerCase()) ||
        !pairTokens.includes(contracts.tokenB.toLowerCase())
    ) {
        throw new Error('SushiSwap V2 pair token wiring is invalid');
    }
    const callGraph = new ethers.Contract(
        contracts.callGraphHarness,
        artifacts.callGraph.abi,
        runner,
    );
    if (
        ((await callGraph.node()) as string).toLowerCase() !==
        contracts.callGraphNode.toLowerCase()
    ) {
        throw new Error('Call graph node wiring is invalid');
    }
    const proxy = (address: string) => new ethers.Contract(address, artifacts.proxy.abi, runner);
    const token = (address: string) => new ethers.Contract(address, artifacts.proxyErc20.abi, runner);
    for (const name of [
        'dexOutputTokenProxy',
        'farmRewardTokenProxy',
        'lendingReceiptTokenProxy',
        'liquidStakingReceiptTokenProxy',
    ]) {
        if (
            ((await proxy(contracts[name]).implementation()) as string).toLowerCase() !==
            contracts.proxyErc20Implementation.toLowerCase()
        ) {
            throw new Error(`${name} implementation wiring is invalid`);
        }
    }
    const poolKey = ethers.keccak256(
        ethers.AbiCoder.defaultAbiCoder().encode(
            ['address', 'address', 'uint24'],
            contracts.tokenA.toLowerCase() < contracts.dexOutputTokenProxy.toLowerCase()
                ? [contracts.tokenA, contracts.dexOutputTokenProxy, 3000]
                : [contracts.dexOutputTokenProxy, contracts.tokenA, 3000],
        ),
    );
    const v3Router = new ethers.Contract(contracts.v3Router, artifacts.v3Router.abi, runner);
    if (((await v3Router.poolForPair(poolKey)) as string).toLowerCase() !== contracts.v3Pool.toLowerCase()) {
        throw new Error('V3 router pool wiring is invalid');
    }
    const chef = new ethers.Contract(contracts.masterChef, artifacts.masterChef.abi, runner);
    const pool = await chef.poolInfo(0);
    if (
        (pool.stakingToken as string).toLowerCase() !== contracts.tokenB.toLowerCase() ||
        !(await token(contracts.farmRewardTokenProxy).isMinter(contracts.masterChef))
    ) {
        throw new Error('MasterChef wiring is invalid');
    }
    const lending = new ethers.Contract(
        contracts.lendingPoolProxy,
        artifacts.lendingImplementation.abi,
        runner,
    );
    if (
        ((await proxy(contracts.lendingPoolProxy).implementation()) as string).toLowerCase() !==
            contracts.lendingImplementation.toLowerCase() ||
        ((await lending.asset()) as string).toLowerCase() !== contracts.tokenA.toLowerCase() ||
        !(await token(contracts.lendingReceiptTokenProxy).isMinter(contracts.lendingPoolProxy))
    ) {
        throw new Error('Lending wiring is invalid');
    }
    const liquid = new ethers.Contract(
        contracts.liquidStakingProxy,
        artifacts.liquidStakingImplementation.abi,
        runner,
    );
    if (
        ((await liquid.underlyingToken()) as string).toLowerCase() !== contracts.tokenB.toLowerCase() ||
        !(await token(contracts.liquidStakingReceiptTokenProxy).isMinter(
            contracts.liquidStakingProxy,
        ))
    ) {
        throw new Error('Liquid staking wiring is invalid');
    }
    const adapter = new ethers.Contract(
        contracts.strategyAdapter,
        artifacts.strategyAdapter.abi,
        runner,
    );
    if (
        ((await adapter.controller()) as string).toLowerCase() !==
            contracts.strategyVaultProxy.toLowerCase() ||
        ((await proxy(contracts.strategyVaultProxy).implementation()) as string).toLowerCase() !==
            contracts.strategyVaultImplementation.toLowerCase()
    ) {
        throw new Error('Strategy vault wiring is invalid');
    }
}

async function artifact(relativePath: string): Promise<Artifact> {
    const parsed = JSON.parse(await fs.readFile(path.resolve(relativePath), 'utf8')) as Artifact;
    return {
        abi: parsed.abi,
        bytecode: parsed.bytecode.startsWith('0x') ? parsed.bytecode : `0x${parsed.bytecode}`,
    };
}

async function deploy(
    name: string,
    signer: ethers.Signer,
    contractArtifact: Artifact,
    nonce: number,
    args: unknown[] = [],
): Promise<ethers.BaseContract> {
    const contract = await new ethers.ContractFactory(
        contractArtifact.abi,
        contractArtifact.bytecode,
        signer,
    ).deploy(...args, { nonce });
    console.log(`  ${name} submitted: ${contract.deploymentTransaction()?.hash}`);
    await contract.waitForDeployment();
    console.log(`  ${name}: ${await contract.getAddress()}`);
    return contract;
}

async function readExisting(file: string): Promise<ReplayDeploymentManifest | undefined> {
    try {
        return JSON.parse(await fs.readFile(file, 'utf8')) as ReplayDeploymentManifest;
    } catch (error) {
        if ((error as NodeJS.ErrnoException).code === 'ENOENT') return undefined;
        throw error;
    }
}

async function verifyExisting(
    manifest: ReplayDeploymentManifest,
    network: string,
    chainId: bigint,
    provider: ethers.Provider,
    artifacts: Record<string, Artifact>,
): Promise<void> {
    if (manifest.schemaVersion !== REPLAY_DEPLOYMENT_SCHEMA_VERSION) {
        throw new Error(
            `Existing deployment schema ${manifest.schemaVersion}; ` +
                `expected schema ${REPLAY_DEPLOYMENT_SCHEMA_VERSION}`,
        );
    }
    validateSushiV2Provenance(manifest);
    for (const name of REPLAY_CONTRACT_KEYS) {
        if (!manifest.contracts[name]) throw new Error(`Existing deployment is missing ${name}`);
    }
    if (manifest.network !== network || BigInt(manifest.chainId) !== chainId) {
        throw new Error(`Existing manifest is for ${manifest.network}/${manifest.chainId}`);
    }
    for (const [name, address] of Object.entries(manifest.contracts)) {
        if (!address) continue;
        const code = await provider.getCode(address);
        if (code === '0x') {
            throw new Error(`Existing ${name} has no code at ${address}`);
        }
        const expectedHash = manifest.codeHashes?.[name];
        if (expectedHash && ethers.keccak256(code) !== expectedHash) {
            throw new Error(`Existing ${name} bytecode does not match its deployment manifest`);
        }
    }
    await verifyWiring(manifest.contracts as Record<string, string>, artifacts, provider);
}

if (require.main === module) {
    deployFixturesMain().catch(error => {
        console.error('Fatal:', error instanceof Error ? error.message : error);
        process.exitCode = 1;
    });
}
