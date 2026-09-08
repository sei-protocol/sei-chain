import { coins } from '@cosmjs/amino';
import { EncodeObject } from '@cosmjs/proto-signing';
import { SigningStargateClient } from '@cosmjs/stargate';

export async function sendFundingBatches(
    client: SigningStargateClient,
    sender: string,
    amounts: Array<{ address: string; amount: bigint }>,
    memo: string,
): Promise<void> {
    const nonZero = amounts.filter(item => item.amount > 0n);
    const fees = Array.from({ length: Math.ceil(nonZero.length / 10) }, (_, batch) =>
        BigInt(30_000 + Math.min(10, nonZero.length - batch * 10) * 5_000),
    ).reduce((sum, fee) => sum + fee, 0n);
    const required = nonZero.reduce((sum, item) => sum + item.amount, fees);
    const available = BigInt((await client.getBalance(sender, 'usei')).amount);
    if (available < required) {
        throw new Error(
            `Funding account has ${available} usei but this pass requires at least ${required} usei`,
        );
    }
    for (let start = 0; start < nonZero.length; start += 10) {
        const batch = nonZero.slice(start, start + 10);
        const messages: EncodeObject[] = batch.map(item => ({
            typeUrl: '/cosmos.bank.v1beta1.MsgSend',
            value: {
                fromAddress: sender,
                toAddress: item.address,
                amount: coins(item.amount.toString(), 'usei'),
            },
        }));
        const result = await client.signAndBroadcast(
            sender,
            messages,
            {
                amount: coins(String(30_000 + batch.length * 5_000), 'usei'),
                gas: String(200_000 + batch.length * 100_000),
            },
            memo,
        );
        if (result.code !== 0) throw new Error(`Funding batch failed: ${result.rawLog}`);
        console.log(`  funded batch ${start / 10 + 1}/${Math.ceil(nonZero.length / 10)}`);
    }
}
