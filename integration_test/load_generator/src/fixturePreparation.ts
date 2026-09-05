import { ethers } from 'ethers';
import { mapConcurrent } from './concurrency';
import { ReplayDeploymentManifest } from './replay/replayTypes';

interface FixturePreparationWorker {
    evmAddress: string;
    evmNonce: number;
    wallet: ethers.Wallet;
}

const TOKEN_ABI = [
    'function balanceOf(address) view returns(uint256)',
    'function allowance(address,address) view returns(uint256)',
    'function mint(address,uint256)',
    'function approve(address,uint256)',
];
const FARM_ABI = [
    'function userInfo(uint256,address) view returns(uint256 amount,uint256 rewardDebt,uint256 unpaidRewards)',
    'function deposit(uint256,uint256)',
];
const LENDING_ABI = [
    'function accountSnapshot(address) view returns(uint256 supplied,uint256 debt)',
    'function supply(address,uint256,address,uint16)',
    'function borrow(uint256)',
];
const LIQUID_STAKING_ABI = ['function stake(uint256) returns(uint256)'];
const VAULT_ABI = [
    'function balanceOf(address) view returns(uint256)',
    'function deposit(uint256,address) returns(uint256)',
];

export async function prepareSemanticFixtures(
    workers: FixturePreparationWorker[],
    deployment: ReplayDeploymentManifest,
    gasLimit: bigint,
): Promise<void> {
    const contracts = requiredPreparationContracts(deployment);
    const targetBalance = ethers.parseEther('1000');
    const baseline = ethers.parseEther('0.01');
    console.log(
        `Preparing balances, approvals, and baseline protocol positions for ${workers.length} users...`,
    );

    await mapConcurrent(workers, 5, async worker => {
        let nonce = await worker.wallet.provider!.getTransactionCount(worker.evmAddress, 'pending');
        const options = () => ({ nonce, gasLimit });
        const send = async (
            operation: string,
            transaction: Promise<ethers.ContractTransactionResponse>,
        ): Promise<void> => {
            const response = await transaction;
            try {
                await response.wait();
                nonce++;
            } catch (error) {
                throw new Error(
                    `Fixture preparation failed for ${worker.evmAddress} during ${operation} ` +
                        `(tx ${response.hash})`,
                    { cause: error },
                );
            }
        };

        for (const address of [contracts.tokenA, contracts.tokenB]) {
            const token = new ethers.Contract(address, TOKEN_ABI, worker.wallet);
            const balance = (await token.balanceOf(worker.evmAddress)) as bigint;
            if (balance < targetBalance) {
                await send(
                    `mint ${address}`,
                    token.mint(worker.evmAddress, targetBalance - balance, options()),
                );
            }
            const spenders =
                address === contracts.tokenA
                    ? [
                          contracts.router,
                          contracts.v3Router,
                          contracts.lendingPoolProxy,
                          contracts.strategyVaultProxy,
                      ]
                    : [
                          contracts.router,
                          contracts.masterChef,
                          contracts.liquidStakingProxy,
                      ];
            for (const spender of spenders) {
                const allowance = (await token.allowance(
                    worker.evmAddress,
                    spender,
                )) as bigint;
                if (allowance < targetBalance) {
                    await send(
                        `approve ${spender}`,
                        token.approve(spender, ethers.MaxUint256, options()),
                    );
                }
            }
        }

        const farm = new ethers.Contract(contracts.masterChef, FARM_ABI, worker.wallet);
        const farmPosition = await farm.userInfo(0, worker.evmAddress);
        if ((farmPosition.amount as bigint) < baseline) {
            await send(
                'seed farm position',
                farm.deposit(0, baseline - (farmPosition.amount as bigint), options()),
            );
        }

        const lending = new ethers.Contract(
            contracts.lendingPoolProxy,
            LENDING_ABI,
            worker.wallet,
        );
        const lendingPosition = await lending.accountSnapshot(worker.evmAddress);
        if ((lendingPosition.supplied as bigint) < baseline) {
            await send(
                'seed lending supply',
                lending.supply(
                    contracts.tokenA,
                    baseline - (lendingPosition.supplied as bigint),
                    worker.evmAddress,
                    0,
                    options(),
                ),
            );
        }
        const baselineDebt = baseline / 10n;
        if ((lendingPosition.debt as bigint) < baselineDebt) {
            await send(
                'seed lending borrow',
                lending.borrow(
                    baselineDebt - (lendingPosition.debt as bigint),
                    options(),
                ),
            );
        }

        const liquidReceipt = new ethers.Contract(
            contracts.liquidStakingReceiptTokenProxy,
            TOKEN_ABI,
            worker.wallet,
        );
        const liquidShares = (await liquidReceipt.balanceOf(worker.evmAddress)) as bigint;
        if (liquidShares < baseline) {
            await send(
                'seed liquid staking position',
                new ethers.Contract(
                    contracts.liquidStakingProxy,
                    LIQUID_STAKING_ABI,
                    worker.wallet,
                ).stake(baseline - liquidShares, options()),
            );
        }

        const vault = new ethers.Contract(
            contracts.strategyVaultProxy,
            VAULT_ABI,
            worker.wallet,
        );
        const vaultShares = (await vault.balanceOf(worker.evmAddress)) as bigint;
        if (vaultShares < baseline) {
            await send(
                'seed strategy vault position',
                vault.deposit(baseline - vaultShares, worker.evmAddress, options()),
            );
        }
        worker.evmNonce = nonce;
    });
}

function requiredPreparationContracts(deployment: ReplayDeploymentManifest) {
    const {
        tokenA,
        tokenB,
        router,
        v3Router,
        masterChef,
        lendingPoolProxy,
        liquidStakingProxy,
        liquidStakingReceiptTokenProxy,
        strategyVaultProxy,
    } = deployment.contracts;
    if (
        !tokenA ||
        !tokenB ||
        !router ||
        !v3Router ||
        !masterChef ||
        !lendingPoolProxy ||
        !liquidStakingProxy ||
        !liquidStakingReceiptTokenProxy ||
        !strategyVaultProxy
    ) {
        throw new Error('Deployment is missing contracts required for fixture preparation');
    }
    return {
        tokenA,
        tokenB,
        router,
        v3Router,
        masterChef,
        lendingPoolProxy,
        liquidStakingProxy,
        liquidStakingReceiptTokenProxy,
        strategyVaultProxy,
    };
}
