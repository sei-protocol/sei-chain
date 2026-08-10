export function minBigInt(left: bigint, right: bigint): bigint {
    return left < right ? left : right;
}

export function maxBigInt(left: bigint, right: bigint): bigint {
    return left > right ? left : right;
}
