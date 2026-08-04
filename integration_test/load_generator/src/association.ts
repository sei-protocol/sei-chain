import { Reader, Writer } from 'protobufjs';

interface AssociationQueryResponse {
    associated: boolean;
    evmAddress: string;
}

export async function queryEvmAssociation(
    cosmosRpcUrl: string,
    seiAddress: string,
): Promise<AssociationQueryResponse> {
    const request = Writer.create().uint32(10).string(seiAddress).finish();
    const response = await fetch(cosmosRpcUrl, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
            jsonrpc: '2.0',
            id: 1,
            method: 'abci_query',
            params: {
                path: '/seiprotocol.seichain.evm.Query/EVMAddressBySeiAddress',
                data: Buffer.from(request).toString('hex'),
                height: '0',
                prove: false,
            },
        }),
    });
    if (!response.ok) {
        throw new Error(`Association query failed with HTTP ${response.status}`);
    }
    const payload = (await response.json()) as {
        error?: { message?: string };
        result?: {
            response?: {
                code?: number;
                log?: string;
                value?: string;
            };
        };
    };
    if (payload.error) {
        throw new Error(`Association query failed: ${payload.error.message ?? 'unknown error'}`);
    }
    const result = payload.result?.response;
    if (!result || result.code !== 0 || !result.value) {
        throw new Error(`Association query failed: ${result?.log ?? 'empty ABCI response'}`);
    }
    const reader = Reader.create(Buffer.from(result.value, 'base64'));
    let evmAddress = '';
    let associated = false;
    while (reader.pos < reader.len) {
        const tag = reader.uint32();
        if ((tag >>> 3) === 1) evmAddress = reader.string();
        else if ((tag >>> 3) === 2) associated = reader.bool();
        else reader.skipType(tag & 7);
    }
    return { associated, evmAddress };
}
