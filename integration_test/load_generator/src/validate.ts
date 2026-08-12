/**
 * Validate segment continuity and independently reconstruct each wrapped EVM
 * transaction hash from its Cosmos MsgEVMTransaction payload.
 */
import { correlateEvmWrapper } from './replay/evmCorrelation';
import { EVM_MESSAGE_TYPE } from './replay/replayTypes';
import { readReplaySegments, validateReplaySegments } from './replay/corpus';
import { replayDirectory } from './config';

async function main(): Promise<void> {
    const directory = replayDirectory();
    const segments = await readReplaySegments(directory);
    if (segments.length === 0) throw new Error(`No replay segments in ${directory}`);
    validateReplaySegments(segments);

    let canonical = 0;
    let wrappers = 0;
    let confirmed = 0;
    let unresolved = 0;
    let mismatched = 0;
    for (const segment of segments) {
        for (const block of segment.blocks) {
            canonical += block.transactions.length;
            for (const transaction of block.transactions) {
                if (!transaction.isEvm) continue;
                wrappers++;
                const messages = transaction.messages.filter(
                    message => message.typeUrl === EVM_MESSAGE_TYPE,
                );
                if (messages.length !== 1) {
                    unresolved++;
                    console.warn(
                        `Block ${block.number} Cosmos tx ${transaction.hash}: ` +
                            `${messages.length} EVM messages`,
                    );
                    continue;
                }
                const link = correlateEvmWrapper(
                    Buffer.from(messages[0].valueBase64, 'base64'),
                );
                if (!link.hash) {
                    unresolved++;
                    console.warn(
                        `Block ${block.number} Cosmos tx ${transaction.hash}: ${link.reason}`,
                    );
                    continue;
                }
                if (
                    transaction.evm &&
                    transaction.evm.hash.toLowerCase() !== link.hash.toLowerCase()
                ) {
                    mismatched++;
                    console.error(
                        `Block ${block.number}: attached ${transaction.evm.hash}, ` +
                            `signed payload ${link.hash}`,
                    );
                    continue;
                }
                confirmed++;
            }
        }
    }

    console.log(
        `Validated ${segments.length} segments, ${canonical} canonical txs, ` +
            `${wrappers} EVM wrappers`,
    );
    console.log(
        `EVM correlation: confirmed=${confirmed} unresolved=${unresolved} mismatched=${mismatched}`,
    );
    if (unresolved > 0 || mismatched > 0) process.exitCode = 1;
}

main().catch(error => {
    console.error('Fatal:', error instanceof Error ? error.message : error);
    process.exit(1);
});
