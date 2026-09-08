import { MsgSend } from 'cosmjs-types/cosmos/bank/v1beta1/tx';
import { ethers } from 'ethers';
import { nextWorker } from './common';
import { LoadOperation, WorkloadContext } from './types';

const BANK_PRECOMPILE = '0x0000000000000000000000000000000000001001';
const BANK = new ethers.Interface(['function sendNative(string) payable returns(bool)']);

// sendNative moves EVM value, which is denominated in wei rather than usei.
const WEI_PER_USEI = 10n ** 12n;

export function nativeTransferOperations(context: WorkloadContext): LoadOperation[] {
    return [
        {
            name: 'cosmos_bank_send',
            lane: 'cosmos',
            weight: 40,
            async build(worker) {
                return {
                    lane: 'cosmos',
                    messages: [
                        {
                            typeUrl: '/cosmos.bank.v1beta1.MsgSend',
                            value: MsgSend.fromPartial({
                                fromAddress: worker.seiAddress,
                                toAddress: nextWorker(context, worker.slot).seiAddress,
                                amount: [{ denom: 'usei', amount: '1' }],
                            }),
                        },
                    ],
                    // Measured usage is ~52k; the fee is derived from this figure, so
                    // headroom here is paid for on every transaction.
                    gas: '80000',
                    memo: 'loadgen bank send',
                };
            },
        },
        {
            name: 'evm_native_transfer',
            lane: 'evm',
            weight: 30,
            async build(worker) {
                return {
                    lane: 'evm',
                    transaction: {
                        to: nextWorker(context, worker.slot).evmAddress,
                        value: 1n,
                        gasLimit: 100_000n,
                    },
                };
            },
        },
        {
            name: 'bank_precompile_send',
            lane: 'evm',
            weight: 30,
            async build(worker) {
                return {
                    lane: 'evm',
                    transaction: {
                        to: BANK_PRECOMPILE,
                        // `send` is gated to the denom's ERC20 pointer contract,
                        // so an EOA worker must use sendNative instead.
                        data: BANK.encodeFunctionData('sendNative', [
                            nextWorker(context, worker.slot).seiAddress,
                        ]),
                        value: WEI_PER_USEI,
                        // ~50k when the receiver exists, more when sendNative has to create
                        // the account. Unused EVM gas is refunded, but a revert burns the
                        // whole limit, so this stays close to worst-case usage.
                        gasLimit: 150_000n,
                    },
                };
            },
        },
    ];
}
