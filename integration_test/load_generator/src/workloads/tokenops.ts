import { MsgExecuteContract } from 'cosmjs-types/cosmwasm/wasm/v1/tx';
import { ethers } from 'ethers';
import { mapConcurrent } from '../concurrency';
import { LoadOperation, LoadWorker, WorkloadContext } from './types';
import { evmOperation, nextWorker, requiredContract } from './common';

const ERC20 = new ethers.Interface([
    'function mint(address,uint256)',
    'function transfer(address,uint256) returns(bool)',
    'function approve(address,uint256) returns(bool)',
    'function burn(uint256)',
    'function balanceOf(address) view returns(uint256)',
]);
const ERC721 = new ethers.Interface([
    'function safeMint(address,uint256)',
    'function ownerOf(uint256) view returns(address)',
    'function transferFrom(address,address,uint256)',
    'function roundTripTransfer(address,uint256)',
]);
const ERC1155 = new ethers.Interface([
    'function mint(address,uint256,uint256)',
    'function mintBatch(address,uint256[],uint256[])',
    'function safeTransferFrom(address,address,uint256,uint256,bytes)',
    'function safeBatchTransferFrom(address,address,uint256[],uint256[],bytes)',
    'function balanceOf(uint256,address) view returns(uint256)',
]);
const AMOUNT = ethers.parseEther('0.001');
const GAS_LIMIT = 800_000n;

export function tokenOperations(context: WorkloadContext): LoadOperation[] {
    const token = requiredContract(context, 'tokenA');
    const nft = requiredContract(context, 'nft');
    const erc1155 = requiredContract(context, 'erc1155');
    const operations: LoadOperation[] = [
        evmOperation('erc20_mint', 18, token, ERC20, 'mint', GAS_LIMIT, worker => [
            worker.evmAddress,
            AMOUNT,
        ]),
        evmOperation('erc20_transfer', 20, token, ERC20, 'transfer', GAS_LIMIT, worker => [
            nextWorker(context, worker.slot).evmAddress,
            AMOUNT / 10n,
        ]),
        evmOperation('erc20_approve', 8, token, ERC20, 'approve', GAS_LIMIT, worker => [
            nextWorker(context, worker.slot).evmAddress,
            AMOUNT,
        ]),
        evmOperation('erc20_burn', 8, token, ERC20, 'burn', GAS_LIMIT, () => [AMOUNT / 20n]),
        evmOperation('erc721_mint', 14, nft, ERC721, 'safeMint', GAS_LIMIT, (worker, sequence) => [
            worker.evmAddress,
            uniqueTokenId(context.executionId, worker, sequence),
        ]),
        evmOperation(
            'erc721_round_trip',
            10,
            nft,
            ERC721,
            'roundTripTransfer',
            GAS_LIMIT,
            worker => [nextWorker(context, worker.slot).evmAddress, seedTokenId(worker)],
        ),
        evmOperation('erc1155_mint', 10, erc1155, ERC1155, 'mint', GAS_LIMIT, worker => [
            worker.evmAddress,
            worker.index,
            10,
        ]),
        evmOperation(
            'erc1155_transfer',
            8,
            erc1155,
            ERC1155,
            'safeTransferFrom',
            GAS_LIMIT,
            worker => [
                worker.evmAddress,
                nextWorker(context, worker.slot).evmAddress,
                worker.index,
                1,
                '0x',
            ],
        ),
        evmOperation('erc1155_batch_mint', 4, erc1155, ERC1155, 'mintBatch', GAS_LIMIT, worker => [
            worker.evmAddress,
            [worker.index * 10, worker.index * 10 + 1],
            [2, 2],
        ]),
    ];
    if (context.cw1155Contract) {
        operations.push({
            name: 'cw1155_send',
            lane: 'cosmos',
            weight: 8,
            async build(worker) {
                return {
                    lane: 'cosmos',
                    messages: [
                        {
                            typeUrl: '/cosmwasm.wasm.v1.MsgExecuteContract',
                            value: MsgExecuteContract.fromPartial({
                                sender: worker.seiAddress,
                                contract: context.cw1155Contract,
                                msg: Buffer.from(
                                    JSON.stringify({
                                        send: {
                                            from: worker.seiAddress,
                                            to: nextWorker(context, worker.slot).seiAddress,
                                            token_id: String(worker.index),
                                            amount: '1',
                                        },
                                    }),
                                ),
                                funds: [],
                            }),
                        },
                    ],
                    gas: '600000',
                    memo: 'loadgen cw1155 send',
                };
            },
        });
    }
    return operations;
}

export async function prepareTokenFixtures(
    workers: LoadWorker[],
    context: WorkloadContext,
    receiptTimeoutMs: number,
): Promise<void> {
    const token = requiredContract(context, 'tokenA');
    const nft = requiredContract(context, 'nft');
    const erc1155 = requiredContract(context, 'erc1155');
    await mapConcurrent(workers, 5, async worker => {
        worker.evmNonce = await worker.wallet.provider!.getTransactionCount(
            worker.evmAddress,
            'pending',
        );
        const erc20 = new ethers.Contract(token, ERC20, worker.wallet);
        const erc721 = new ethers.Contract(nft, ERC721, worker.wallet);
        const multi = new ethers.Contract(erc1155, ERC1155, worker.wallet);
        const sends: Array<() => Promise<ethers.ContractTransactionResponse>> = [];
        if (((await erc20.balanceOf(worker.evmAddress)) as bigint) < AMOUNT) {
            sends.push(() => erc20.mint(worker.evmAddress, AMOUNT, transaction(worker)));
        }
        const tokenId = seedTokenId(worker);
        if (((await erc721.ownerOf(tokenId)) as string) === ethers.ZeroAddress) {
            sends.push(() => erc721.safeMint(worker.evmAddress, tokenId, transaction(worker)));
        }
        if (((await multi.balanceOf(worker.index, worker.evmAddress)) as bigint) < 10n) {
            sends.push(() => multi.mint(worker.evmAddress, worker.index, 10, transaction(worker)));
        }
        for (const send of sends) {
            const response = await send();
            await response.wait(1, receiptTimeoutMs);
            worker.evmNonce++;
        }
    });
}

function uniqueTokenId(executionId: string, worker: LoadWorker, sequence: number): bigint {
    return BigInt(
        ethers.keccak256(
            ethers.solidityPacked(
                ['string', 'address', 'uint256'],
                [executionId, worker.evmAddress, sequence + 1],
            ),
        ),
    );
}

function seedTokenId(worker: LoadWorker): bigint {
    return BigInt(
        ethers.keccak256(
            ethers.solidityPacked(['string', 'address'], ['sei-loadgen-seed', worker.evmAddress]),
        ),
    );
}

function transaction(worker: LoadWorker): ethers.TransactionRequest {
    return { nonce: worker.evmNonce, gasLimit: GAS_LIMIT };
}
