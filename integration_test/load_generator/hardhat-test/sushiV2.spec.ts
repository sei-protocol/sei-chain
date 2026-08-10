import { expect } from 'chai';
import { artifacts, ethers } from 'hardhat';
import factoryArtifact from '../vendor/sushiswap-v2/artifacts/UniswapV2Factory.json';
import pairArtifact from '../vendor/sushiswap-v2/artifacts/UniswapV2Pair.json';
import routerArtifact from '../vendor/sushiswap-v2/artifacts/UniswapV2Router02.json';
import { SUSHI_V2_PROVENANCE } from '../src/sushiV2';

describe('canonical production SushiSwap V2', () => {
    it('keeps the vendored pair source aligned with production bytecode', async () => {
        const compiled = await artifacts.readArtifact('UniswapV2Pair');
        expect(stripMetadata(compiled.bytecode)).to.equal(
            stripMetadata(pairArtifact.bytecode),
        );
    });

    it('creates the canonical pair, adds liquidity, and swaps tokens', async () => {
        for (const artifact of [factoryArtifact, pairArtifact, routerArtifact]) {
            expect(artifact.bytecode).not.to.include('__$');
        }
        expect(ethers.keccak256(pairArtifact.bytecode)).to.equal(
            SUSHI_V2_PROVENANCE.pairInitCodeHash,
        );

        const [admin, trader] = await ethers.getSigners();
        const weth = await ethers.deployContract('WETH9');
        const factory = await new ethers.ContractFactory(
            factoryArtifact.abi,
            factoryArtifact.bytecode,
            admin,
        ).deploy(admin.address);
        const router = await new ethers.ContractFactory(
            routerArtifact.abi,
            routerArtifact.bytecode,
            admin,
        ).deploy(await factory.getAddress(), await weth.getAddress());
        const tokenA = await ethers.deployContract('TestERC20', [admin.address]);
        const tokenB = await ethers.deployContract('TestERC20', [admin.address]);
        await Promise.all([
            weth.waitForDeployment(),
            factory.waitForDeployment(),
            router.waitForDeployment(),
            tokenA.waitForDeployment(),
            tokenB.waitForDeployment(),
        ]);
        const factoryContract = new ethers.Contract(
            await factory.getAddress(),
            factoryArtifact.abi,
            admin,
        );
        const routerContract = new ethers.Contract(
            await router.getAddress(),
            routerArtifact.abi,
            admin,
        );

        expect(await factoryContract.pairCodeHash()).to.equal(
            SUSHI_V2_PROVENANCE.pairInitCodeHash,
        );
        expect(await routerContract.factory()).to.equal(await factory.getAddress());
        expect(await routerContract.WETH()).to.equal(await weth.getAddress());

        const tokenAAddress = await tokenA.getAddress();
        const tokenBAddress = await tokenB.getAddress();
        const [token0, token1] =
            tokenAAddress.toLowerCase() < tokenBAddress.toLowerCase()
                ? [tokenAAddress, tokenBAddress]
                : [tokenBAddress, tokenAAddress];
        const salt = ethers.keccak256(
            ethers.solidityPacked(['address', 'address'], [token0, token1]),
        );
        const expectedPair = ethers.getCreate2Address(
            await factory.getAddress(),
            salt,
            SUSHI_V2_PROVENANCE.pairInitCodeHash,
        );

        const liquidity = ethers.parseEther('1000');
        await tokenA.mint(admin.address, liquidity);
        await tokenB.mint(admin.address, liquidity);
        await tokenA.approve(await router.getAddress(), liquidity);
        await tokenB.approve(await router.getAddress(), liquidity);
        const deadline = (await ethers.provider.getBlock('latest'))!.timestamp + 3600;
        await routerContract.addLiquidity(
            tokenAAddress,
            tokenBAddress,
            liquidity,
            liquidity,
            0,
            0,
            admin.address,
            deadline,
        );

        expect(await factoryContract.getPair(tokenAAddress, tokenBAddress)).to.equal(expectedPair);
        const pair = new ethers.Contract(expectedPair, pairArtifact.abi, admin);
        expect(await pair.factory()).to.equal(await factory.getAddress());
        expect([await pair.token0(), await pair.token1()]).to.deep.equal([token0, token1]);
        expect(await pair.name()).to.equal('SushiSwap LP Token');
        expect(await pair.symbol()).to.equal('SLP');
        expect(await pair.balanceOf(admin.address)).to.be.greaterThan(0n);

        const amountIn = ethers.parseEther('10');
        await tokenA.mint(trader.address, amountIn);
        const traderTokenA = new ethers.Contract(tokenAAddress, tokenA.interface, trader);
        const traderRouter = new ethers.Contract(
            await router.getAddress(),
            routerArtifact.abi,
            trader,
        );
        await traderTokenA.approve(await router.getAddress(), amountIn);
        const before = await tokenB.balanceOf(trader.address);
        await traderRouter.swapExactTokensForTokens(
            amountIn,
            0,
            [tokenAAddress, tokenBAddress],
            trader.address,
            deadline,
        );
        expect(await tokenB.balanceOf(trader.address)).to.be.greaterThan(before);
    });
});

function stripMetadata(bytecode: string): string {
    const bytes = ethers.getBytes(bytecode);
    const metadataLength = bytes[bytes.length - 2] * 256 + bytes[bytes.length - 1];
    return ethers.hexlify(bytes.slice(0, bytes.length - metadataLength - 2));
}
