import { QueryClient, setupBankExtension, BankExtension } from '@cosmjs/stargate';
import { Tendermint34Client } from '@cosmjs/tendermint-rpc';
import { QueryBalanceRequest, QueryBalanceResponse } from 'cosmjs-types/cosmos/bank/v1beta1/query';
import { Endpoints } from '../config/endpoints';

let clientPromise: Promise<QueryClient & BankExtension> | undefined;

async function bankClient(): Promise<QueryClient & BankExtension> {
    if (!clientPromise) {
        clientPromise = (async () => {
            const tm = await Tendermint34Client.connect(Endpoints.sei.cosmosRpc);
            return QueryClient.withExtensions(tm, setupBankExtension);
        })();
    }
    return clientPromise;
}

export async function bankBalanceUsei(seiAddress: string, height?: number): Promise<bigint> {
    const qc = await bankClient();
    if (height === undefined) {
        const coin = await qc.bank.balance(seiAddress, 'usei');
        return BigInt(coin.amount);
    }
    const request = QueryBalanceRequest.encode({ address: seiAddress, denom: 'usei' }).finish();
    const { value } = await qc.queryAbci('/cosmos.bank.v1beta1.Query/Balance', request, height);
    const { balance } = QueryBalanceResponse.decode(value);
    return balance ? BigInt(balance.amount) : 0n;
}
