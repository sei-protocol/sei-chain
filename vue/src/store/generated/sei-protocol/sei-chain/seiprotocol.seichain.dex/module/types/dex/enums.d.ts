export declare const protobufPackage = "seiprotocol.seichain.dex";
export declare enum PositionDirection {
    LONG = 0,
    SHORT = 1,
    UNRECOGNIZED = -1
}
export declare function positionDirectionFromJSON(object: any): PositionDirection;
export declare function positionDirectionToJSON(object: PositionDirection): string;
export declare enum PositionEffect {
    OPEN = 0,
    CLOSE = 1,
    UNRECOGNIZED = -1
}
export declare function positionEffectFromJSON(object: any): PositionEffect;
export declare function positionEffectToJSON(object: PositionEffect): string;
export declare enum OrderType {
    LIMIT = 0,
    MARKET = 1,
    LIQUIDATION = 2,
    UNRECOGNIZED = -1
}
export declare function orderTypeFromJSON(object: any): OrderType;
export declare function orderTypeToJSON(object: OrderType): string;
export declare enum Denom {
    SEI = 0,
    ATOM = 1,
    BTC = 2,
    ETH = 3,
    SOL = 4,
    AVAX = 5,
    USDC = 6,
    NEAR = 7,
    OSMO = 8,
    UNRECOGNIZED = -1
}
export declare function denomFromJSON(object: any): Denom;
export declare function denomToJSON(object: Denom): string;
export declare enum Unit {
    STANDARD = 0,
    MILLI = 1,
    MICRO = 2,
    NANO = 3,
    UNRECOGNIZED = -1
}
export declare function unitFromJSON(object: any): Unit;
export declare function unitToJSON(object: Unit): string;
