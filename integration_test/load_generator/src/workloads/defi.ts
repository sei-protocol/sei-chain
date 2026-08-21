import { ethers } from 'ethers';
import { LoadOperation, WorkloadContext } from './types';
import { evmCall, evmOperation, requiredContracts } from './common';

const ROUTER = new ethers.Interface([
    'function getAmountsOut(uint256,address[]) view returns(uint256[])',
    'function swapExactTokensForTokens(uint256,uint256,address[],address,uint256) returns(uint256[])',
]);
const LENDING = new ethers.Interface([
    'function supply(address,uint256,address,uint16)',
    'function withdraw(address,uint256,address) returns(uint256)',
    'function borrow(uint256)',
    'function repayBorrow(uint256) returns(uint256)',
]);
const FARM = new ethers.Interface([
    'function deposit(uint256,uint256)',
    'function withdraw(uint256,uint256)',
]);
const STAKING = new ethers.Interface([
    'function stake(uint256) returns(uint256)',
    'function requestWithdrawal(uint256) returns(uint256)',
]);
const VAULT = new ethers.Interface([
    'function deposit(uint256,address) returns(uint256)',
    'function withdraw(uint256,address,address) returns(uint256)',
]);

const SWAP_AMOUNT = ethers.parseEther('0.001');
const POSITION_AMOUNT = ethers.parseEther('0.0001');
const GAS_LIMIT = 1_500_000n;

export function defiOperations(context: WorkloadContext): LoadOperation[] {
    const contracts = requiredContracts(context, [
        'router',
        'tokenA',
        'tokenB',
        'lendingPoolProxy',
        'masterChef',
        'liquidStakingProxy',
        'strategyVaultProxy',
    ]);
    const swap = (name: string, tokenIn: string, tokenOut: string): LoadOperation => ({
        name,
        lane: 'evm',
        weight: 25,
        async build(worker) {
            const path = [tokenIn, tokenOut];
            const router = new ethers.Contract(contracts.router, ROUTER, context.provider);
            const amounts = (await router.getAmountsOut(SWAP_AMOUNT, path)) as bigint[];
            const minimum = (amounts.at(-1)! * 95n) / 100n;
            return evmCall(
                contracts.router,
                ROUTER.encodeFunctionData('swapExactTokensForTokens', [
                    SWAP_AMOUNT,
                    minimum,
                    path,
                    worker.evmAddress,
                    Math.floor(Date.now() / 1_000) + 120,
                ]),
                GAS_LIMIT,
            );
        },
    });
    return [
        swap('swap_a_to_b', contracts.tokenA, contracts.tokenB),
        swap('swap_b_to_a', contracts.tokenB, contracts.tokenA),
        evmOperation(
            'lend_supply',
            12,
            contracts.lendingPoolProxy,
            LENDING,
            'supply',
            GAS_LIMIT,
            worker => [contracts.tokenA, POSITION_AMOUNT, worker.evmAddress, 0],
        ),
        evmOperation(
            'lend_borrow',
            8,
            contracts.lendingPoolProxy,
            LENDING,
            'borrow',
            GAS_LIMIT,
            () => [POSITION_AMOUNT / 10n],
        ),
        evmOperation(
            'lend_repay',
            8,
            contracts.lendingPoolProxy,
            LENDING,
            'repayBorrow',
            GAS_LIMIT,
            () => [POSITION_AMOUNT / 10n],
        ),
        evmOperation(
            'lend_withdraw',
            4,
            contracts.lendingPoolProxy,
            LENDING,
            'withdraw',
            GAS_LIMIT,
            worker => [contracts.tokenA, POSITION_AMOUNT, worker.evmAddress],
        ),
        evmOperation('farm_deposit', 6, contracts.masterChef, FARM, 'deposit', GAS_LIMIT, () => [
            0,
            POSITION_AMOUNT,
        ]),
        evmOperation('farm_withdraw', 3, contracts.masterChef, FARM, 'withdraw', GAS_LIMIT, () => [
            0,
            POSITION_AMOUNT,
        ]),
        evmOperation(
            'liquid_stake',
            6,
            contracts.liquidStakingProxy,
            STAKING,
            'stake',
            GAS_LIMIT,
            () => [POSITION_AMOUNT],
        ),
        evmOperation(
            'liquid_withdraw',
            2,
            contracts.liquidStakingProxy,
            STAKING,
            'requestWithdrawal',
            GAS_LIMIT,
            () => [POSITION_AMOUNT],
        ),
        evmOperation(
            'vault_deposit',
            6,
            contracts.strategyVaultProxy,
            VAULT,
            'deposit',
            GAS_LIMIT,
            worker => [POSITION_AMOUNT, worker.evmAddress],
        ),
        evmOperation(
            'vault_withdraw',
            2,
            contracts.strategyVaultProxy,
            VAULT,
            'withdraw',
            GAS_LIMIT,
            worker => [POSITION_AMOUNT, worker.evmAddress, worker.evmAddress],
        ),
    ];
}
