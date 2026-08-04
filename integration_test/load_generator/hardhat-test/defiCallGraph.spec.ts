import { expect } from 'chai';
import { ethers, network } from 'hardhat';

type OpcodeTrace = { structLogs?: Array<{ op?: string }> };

async function opcodeCounts(hash: string): Promise<Record<string, number>> {
    const trace = (await network.provider.send('debug_traceTransaction', [
        hash,
        { disableMemory: true, disableStack: true, disableStorage: true },
    ])) as OpcodeTrace;
    const counts: Record<string, number> = {};
    for (const step of trace.structLogs ?? []) {
        if (step.op) counts[step.op] = (counts[step.op] ?? 0) + 1;
    }
    return counts;
}

describe('production-shaped DeFi call graphs', () => {
    it('executes callback DEX CALL, STATICCALL, and proxy DELEGATECALL paths', async () => {
        const [admin, user] = await ethers.getSigners();
        const tokenA = await ethers.deployContract('TestERC20', [admin.address]);
        const tokenImplementation = await ethers.deployContract('ProxyERC20Implementation');
        const tokenInterface = tokenImplementation.interface;
        const proxy = await ethers.deployContract('Allowlisted1967Proxy', [
            admin.address,
            await tokenImplementation.getAddress(),
            tokenInterface.encodeFunctionData('initialize', [
                'Replay DEX Output',
                'rDEX',
                18,
                admin.address,
            ]),
        ]);
        const outputToken = await ethers.getContractAt(
            'ProxyERC20Implementation',
            await proxy.getAddress(),
        );

        const [token0, token1] =
            (await tokenA.getAddress()).toLowerCase() < (await proxy.getAddress()).toLowerCase()
                ? [await tokenA.getAddress(), await proxy.getAddress()]
                : [await proxy.getAddress(), await tokenA.getAddress()];
        const pool = await ethers.deployContract('DeterministicV3Pool', [
            token0,
            token1,
            30,
            ethers.parseEther('100'),
        ]);
        const router = await ethers.deployContract('ProductionShapedSwapRouter', [admin.address]);
        await router.configurePool(
            await tokenA.getAddress(),
            await proxy.getAddress(),
            3000,
            await pool.getAddress(),
        );
        await outputToken.mint(await pool.getAddress(), ethers.parseEther('100'));
        await tokenA.mint(user.address, ethers.parseEther('10'));
        const userTokenA = new ethers.Contract(await tokenA.getAddress(), tokenA.interface, user);
        const userRouter = new ethers.Contract(await router.getAddress(), router.interface, user);
        await userTokenA.approve(await router.getAddress(), ethers.MaxUint256);

        const transaction = await userRouter.exactInputSingle({
            tokenIn: await tokenA.getAddress(),
            tokenOut: await proxy.getAddress(),
            fee: 3000,
            recipient: user.address,
            amountIn: ethers.parseEther('1'),
            amountOutMinimum: 0,
            sqrtPriceLimitX96: 0,
        });
        await transaction.wait();
        const counts = await opcodeCounts(transaction.hash);

        expect(counts.CALL ?? 0).to.be.greaterThan(2);
        expect(counts.STATICCALL ?? 0).to.be.greaterThan(1);
        expect(counts.DELEGATECALL ?? 0).to.be.greaterThan(0);
        expect(await outputToken.balanceOf(user.address)).to.be.greaterThan(0n);
    });

    it('executes proxy and nested strategy DELEGATECALL paths', async () => {
        const [admin, user] = await ethers.getSigners();
        const asset = await ethers.deployContract('TestERC20', [admin.address]);
        const module = await ethers.deployContract('NamespacedStrategyModule');
        const adapter = await ethers.deployContract('DeterministicStrategyAdapter', [
            await asset.getAddress(),
            admin.address,
        ]);
        const implementation = await ethers.deployContract('StrategyVaultImplementation');
        const proxy = await ethers.deployContract('Allowlisted1967Proxy', [
            admin.address,
            await implementation.getAddress(),
            implementation.interface.encodeFunctionData('initialize', [
                await asset.getAddress(),
                admin.address,
                await module.getAddress(),
                await adapter.getAddress(),
                5000,
                'Replay Strategy Vault',
                'rVAULT',
            ]),
        ]);
        const vault = await ethers.getContractAt(
            'StrategyVaultImplementation',
            await proxy.getAddress(),
        );
        await adapter.setController(await proxy.getAddress());
        await asset.mint(user.address, ethers.parseEther('10'));
        const userAsset = new ethers.Contract(await asset.getAddress(), asset.interface, user);
        const userVault = new ethers.Contract(await proxy.getAddress(), vault.interface, user);
        await userAsset.approve(await proxy.getAddress(), ethers.MaxUint256);
        await userVault.deposit(ethers.parseEther('10'), user.address);

        const transaction = await vault.rebalance(ethers.parseEther('1'));
        await transaction.wait();
        const counts = await opcodeCounts(transaction.hash);

        expect(counts.DELEGATECALL ?? 0).to.be.greaterThan(1);
        expect(counts.CALL ?? 0).to.be.greaterThan(0);
        expect(counts.STATICCALL ?? 0).to.be.greaterThan(0);
        expect(await adapter.managedAssets()).to.equal(ethers.parseEther('1'));
    });
});
