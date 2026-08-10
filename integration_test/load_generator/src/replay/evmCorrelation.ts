import { Encoder } from '@sei-js/cosmos/encoding';
import { ethers } from 'ethers';
import { Reader } from 'protobufjs';

interface DecodedSetCodeAuthorization {
    chain_id?: string;
    address: string;
    nonce: number;
    v: Uint8Array;
    r: Uint8Array;
    s: Uint8Array;
}

interface DecodedEthereumTx {
    chain_id?: string;
    nonce: number;
    gas_price?: string;
    gas_tip_cap?: string;
    gas_fee_cap?: string;
    gas_limit: number;
    to: string;
    value: string;
    data: Uint8Array;
    accesses?: Array<{ address: string; storage_keys: string[] }>;
    auth_list?: DecodedSetCodeAuthorization[];
    blob_fee_cap?: string;
    blob_hashes?: Uint8Array[];
    v: Uint8Array;
    r: Uint8Array;
    s: Uint8Array;
}

export interface EvmWrapperCorrelation {
    hash?: string;
    typeUrl?: string;
    method: 'signed_payload' | 'unresolved';
    reason?: string;
}

export function correlateEvmWrapper(messageValue: Uint8Array): EvmWrapperCorrelation {
    try {
        const wrapper = Encoder.evm.MsgEVMTransaction.decode(messageValue);
        if (!wrapper.data) {
            return { method: 'unresolved', reason: 'MsgEVMTransaction has no data Any' };
        }
        const typeUrl = wrapper.data.type_url;
        const decoded = decodeEthereumTx(typeUrl, wrapper.data.value);
        if (!decoded) {
            return {
                typeUrl,
                method: 'unresolved',
                reason: `Unsupported wrapped Ethereum type ${typeUrl}`,
            };
        }
        const transaction = toEthersTransaction(typeUrl, decoded);
        return { hash: transaction.hash!.toLowerCase(), typeUrl, method: 'signed_payload' };
    } catch (error) {
        return {
            method: 'unresolved',
            reason: error instanceof Error ? error.message : String(error),
        };
    }
}

// @sei-js/cosmos (1.0.6) ships decoders for LegacyTx/AccessListTx/DynamicFeeTx/
// BlobTx but not SetCodeTx, so EIP-7702 wrappers are decoded manually below.
// Drop decodeSetCodeTx once @sei-js exposes Encoder.eth.SetCodeTx.
function decodeEthereumTx(
    typeUrl: string,
    value: Uint8Array,
): DecodedEthereumTx | undefined {
    const decoder =
        typeUrl.endsWith('.LegacyTx')
            ? Encoder.eth.LegacyTx
            : typeUrl.endsWith('.AccessListTx')
              ? Encoder.eth.AccessListTx
              : typeUrl.endsWith('.DynamicFeeTx')
                ? Encoder.eth.DynamicFeeTx
                : typeUrl.endsWith('.BlobTx')
                  ? Encoder.eth.BlobTx
                  : undefined;
    if (decoder) return decoder.decode(value) as unknown as DecodedEthereumTx;
    if (typeUrl.endsWith('.SetCodeTx')) return decodeSetCodeTx(value);
    return undefined;
}

function toEthersTransaction(
    typeUrl: string,
    decoded: DecodedEthereumTx,
): ethers.Transaction {
    const isLegacy = typeUrl.endsWith('.LegacyTx');
    const type = isLegacy
        ? 0
        : typeUrl.endsWith('.AccessListTx')
          ? 1
          : typeUrl.endsWith('.DynamicFeeTx')
            ? 2
            : typeUrl.endsWith('.BlobTx')
              ? 3
              : 4;
    const v = bytesToBigInt(decoded.v);
    const legacySignature = isLegacy ? decodeLegacyV(v) : undefined;
    const yParity = isLegacy ? legacySignature!.yParity : Number(v);
    if (yParity !== 0 && yParity !== 1) {
        throw new Error(`Invalid signature parity ${yParity} for ${typeUrl}`);
    }
    const chainId = isLegacy ? legacySignature!.chainId : BigInt(decoded.chain_id ?? '0');
    const accessList = (decoded.accesses ?? []).map(tuple => ({
        address: tuple.address,
        storageKeys: tuple.storage_keys,
    }));
    return ethers.Transaction.from({
        type,
        chainId,
        nonce: decoded.nonce,
        gasLimit: decoded.gas_limit,
        gasPrice: type <= 1 ? decoded.gas_price : undefined,
        maxPriorityFeePerGas: type >= 2 ? decoded.gas_tip_cap : undefined,
        maxFeePerGas: type >= 2 ? decoded.gas_fee_cap : undefined,
        maxFeePerBlobGas: type === 3 ? decoded.blob_fee_cap : undefined,
        blobVersionedHashes:
            type === 3
                ? (decoded.blob_hashes ?? []).map(hash => ethers.hexlify(hash))
                : undefined,
        authorizationList:
            type === 4
                ? (decoded.auth_list ?? []).map(authorization => ({
                      chainId: BigInt(authorization.chain_id ?? '0'),
                      address: authorization.address,
                      nonce: BigInt(authorization.nonce),
                      signature: ethers.Signature.from({
                          r: ethers.hexlify(authorization.r),
                          s: ethers.hexlify(authorization.s),
                          yParity: bytesToParity(authorization.v),
                      }),
                  }))
                : undefined,
        to: decoded.to ? decoded.to : null,
        value: decoded.value || '0',
        data: ethers.hexlify(decoded.data),
        accessList: type >= 1 ? accessList : undefined,
        signature: {
            r: ethers.hexlify(decoded.r),
            s: ethers.hexlify(decoded.s),
            yParity,
        },
    });
}

function decodeLegacyV(v: bigint): { chainId: bigint; yParity: number } {
    if (v === 27n || v === 28n) return { chainId: 0n, yParity: Number(v - 27n) };
    if (v < 35n) throw new Error(`Invalid legacy signature v=${v}`);
    const yParity = Number((v - 35n) % 2n);
    return { chainId: (v - 35n - BigInt(yParity)) / 2n, yParity };
}

function bytesToBigInt(value: Uint8Array): bigint {
    return value.length === 0 ? 0n : BigInt(ethers.hexlify(value));
}

function bytesToParity(value: Uint8Array): 0 | 1 {
    const parity = Number(bytesToBigInt(value));
    if (parity !== 0 && parity !== 1) throw new Error(`Invalid authorization parity ${parity}`);
    return parity;
}

function decodeSetCodeTx(value: Uint8Array): DecodedEthereumTx {
    const reader = Reader.create(value);
    const decoded: DecodedEthereumTx = {
        nonce: 0,
        gas_limit: 0,
        to: '',
        value: '0',
        data: new Uint8Array(),
        accesses: [],
        auth_list: [],
        v: new Uint8Array(),
        r: new Uint8Array(),
        s: new Uint8Array(),
    };
    while (reader.pos < reader.len) {
        const tag = reader.uint32();
        switch (tag >>> 3) {
            case 1:
                decoded.chain_id = readSdkInt(reader);
                break;
            case 2:
                decoded.nonce = readUint64(reader);
                break;
            case 3:
                decoded.gas_tip_cap = readSdkInt(reader);
                break;
            case 4:
                decoded.gas_fee_cap = readSdkInt(reader);
                break;
            case 5:
                decoded.gas_limit = readUint64(reader);
                break;
            case 6:
                decoded.to = reader.string();
                break;
            case 7:
                decoded.value = readSdkInt(reader);
                break;
            case 8:
                decoded.data = reader.bytes();
                break;
            case 9:
                decoded.accesses!.push(decodeAccessTuple(reader.bytes()));
                break;
            case 10:
                decoded.auth_list!.push(decodeSetCodeAuthorization(reader.bytes()));
                break;
            case 11:
                decoded.v = reader.bytes();
                break;
            case 12:
                decoded.r = reader.bytes();
                break;
            case 13:
                decoded.s = reader.bytes();
                break;
            default:
                reader.skipType(tag & 7);
        }
    }
    return decoded;
}

function decodeAccessTuple(value: Uint8Array): {
    address: string;
    storage_keys: string[];
} {
    const reader = Reader.create(value);
    const decoded = { address: '', storage_keys: [] as string[] };
    while (reader.pos < reader.len) {
        const tag = reader.uint32();
        if ((tag >>> 3) === 1) decoded.address = reader.string();
        else if ((tag >>> 3) === 2) decoded.storage_keys.push(reader.string());
        else reader.skipType(tag & 7);
    }
    return decoded;
}

function decodeSetCodeAuthorization(value: Uint8Array): DecodedSetCodeAuthorization {
    const reader = Reader.create(value);
    const decoded: DecodedSetCodeAuthorization = {
        address: '',
        nonce: 0,
        v: new Uint8Array(),
        r: new Uint8Array(),
        s: new Uint8Array(),
    };
    while (reader.pos < reader.len) {
        const tag = reader.uint32();
        switch (tag >>> 3) {
            case 1:
                decoded.chain_id = readSdkInt(reader);
                break;
            case 2:
                decoded.address = reader.string();
                break;
            case 3:
                decoded.nonce = readUint64(reader);
                break;
            case 4:
                decoded.v = reader.bytes();
                break;
            case 5:
                decoded.r = reader.bytes();
                break;
            case 6:
                decoded.s = reader.bytes();
                break;
            default:
                reader.skipType(tag & 7);
        }
    }
    return decoded;
}

function readSdkInt(reader: Reader): string {
    return new TextDecoder().decode(reader.bytes());
}

function readUint64(reader: Reader): number {
    const value = BigInt(reader.uint64().toString());
    if (value > BigInt(Number.MAX_SAFE_INTEGER)) {
        throw new Error(`Protobuf uint64 ${value} exceeds JavaScript safe integer range`);
    }
    return Number(value);
}
