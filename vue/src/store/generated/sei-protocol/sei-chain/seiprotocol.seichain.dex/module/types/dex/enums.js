/* eslint-disable */
export const protobufPackage = "seiprotocol.seichain.dex";
export var PositionDirection;
(function (PositionDirection) {
    PositionDirection[PositionDirection["LONG"] = 0] = "LONG";
    PositionDirection[PositionDirection["SHORT"] = 1] = "SHORT";
    PositionDirection[PositionDirection["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(PositionDirection || (PositionDirection = {}));
export function positionDirectionFromJSON(object) {
    switch (object) {
        case 0:
        case "LONG":
            return PositionDirection.LONG;
        case 1:
        case "SHORT":
            return PositionDirection.SHORT;
        case -1:
        case "UNRECOGNIZED":
        default:
            return PositionDirection.UNRECOGNIZED;
    }
}
export function positionDirectionToJSON(object) {
    switch (object) {
        case PositionDirection.LONG:
            return "LONG";
        case PositionDirection.SHORT:
            return "SHORT";
        default:
            return "UNKNOWN";
    }
}
export var PositionEffect;
(function (PositionEffect) {
    PositionEffect[PositionEffect["OPEN"] = 0] = "OPEN";
    PositionEffect[PositionEffect["CLOSE"] = 1] = "CLOSE";
    PositionEffect[PositionEffect["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(PositionEffect || (PositionEffect = {}));
export function positionEffectFromJSON(object) {
    switch (object) {
        case 0:
        case "OPEN":
            return PositionEffect.OPEN;
        case 1:
        case "CLOSE":
            return PositionEffect.CLOSE;
        case -1:
        case "UNRECOGNIZED":
        default:
            return PositionEffect.UNRECOGNIZED;
    }
}
export function positionEffectToJSON(object) {
    switch (object) {
        case PositionEffect.OPEN:
            return "OPEN";
        case PositionEffect.CLOSE:
            return "CLOSE";
        default:
            return "UNKNOWN";
    }
}
export var OrderType;
(function (OrderType) {
    OrderType[OrderType["LIMIT"] = 0] = "LIMIT";
    OrderType[OrderType["MARKET"] = 1] = "MARKET";
    OrderType[OrderType["LIQUIDATION"] = 2] = "LIQUIDATION";
    OrderType[OrderType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(OrderType || (OrderType = {}));
export function orderTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "LIMIT":
            return OrderType.LIMIT;
        case 1:
        case "MARKET":
            return OrderType.MARKET;
        case 2:
        case "LIQUIDATION":
            return OrderType.LIQUIDATION;
        case -1:
        case "UNRECOGNIZED":
        default:
            return OrderType.UNRECOGNIZED;
    }
}
export function orderTypeToJSON(object) {
    switch (object) {
        case OrderType.LIMIT:
            return "LIMIT";
        case OrderType.MARKET:
            return "MARKET";
        case OrderType.LIQUIDATION:
            return "LIQUIDATION";
        default:
            return "UNKNOWN";
    }
}
export var Denom;
(function (Denom) {
    Denom[Denom["SEI"] = 0] = "SEI";
    Denom[Denom["ATOM"] = 1] = "ATOM";
    Denom[Denom["BTC"] = 2] = "BTC";
    Denom[Denom["ETH"] = 3] = "ETH";
    Denom[Denom["SOL"] = 4] = "SOL";
    Denom[Denom["AVAX"] = 5] = "AVAX";
    Denom[Denom["USDC"] = 6] = "USDC";
    Denom[Denom["NEAR"] = 7] = "NEAR";
    Denom[Denom["OSMO"] = 8] = "OSMO";
    Denom[Denom["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(Denom || (Denom = {}));
export function denomFromJSON(object) {
    switch (object) {
        case 0:
        case "SEI":
            return Denom.SEI;
        case 1:
        case "ATOM":
            return Denom.ATOM;
        case 2:
        case "BTC":
            return Denom.BTC;
        case 3:
        case "ETH":
            return Denom.ETH;
        case 4:
        case "SOL":
            return Denom.SOL;
        case 5:
        case "AVAX":
            return Denom.AVAX;
        case 6:
        case "USDC":
            return Denom.USDC;
        case 7:
        case "NEAR":
            return Denom.NEAR;
        case 8:
        case "OSMO":
            return Denom.OSMO;
        case -1:
        case "UNRECOGNIZED":
        default:
            return Denom.UNRECOGNIZED;
    }
}
export function denomToJSON(object) {
    switch (object) {
        case Denom.SEI:
            return "SEI";
        case Denom.ATOM:
            return "ATOM";
        case Denom.BTC:
            return "BTC";
        case Denom.ETH:
            return "ETH";
        case Denom.SOL:
            return "SOL";
        case Denom.AVAX:
            return "AVAX";
        case Denom.USDC:
            return "USDC";
        case Denom.NEAR:
            return "NEAR";
        case Denom.OSMO:
            return "OSMO";
        default:
            return "UNKNOWN";
    }
}
export var Unit;
(function (Unit) {
    Unit[Unit["STANDARD"] = 0] = "STANDARD";
    Unit[Unit["MILLI"] = 1] = "MILLI";
    Unit[Unit["MICRO"] = 2] = "MICRO";
    Unit[Unit["NANO"] = 3] = "NANO";
    Unit[Unit["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(Unit || (Unit = {}));
export function unitFromJSON(object) {
    switch (object) {
        case 0:
        case "STANDARD":
            return Unit.STANDARD;
        case 1:
        case "MILLI":
            return Unit.MILLI;
        case 2:
        case "MICRO":
            return Unit.MICRO;
        case 3:
        case "NANO":
            return Unit.NANO;
        case -1:
        case "UNRECOGNIZED":
        default:
            return Unit.UNRECOGNIZED;
    }
}
export function unitToJSON(object) {
    switch (object) {
        case Unit.STANDARD:
            return "STANDARD";
        case Unit.MILLI:
            return "MILLI";
        case Unit.MICRO:
            return "MICRO";
        case Unit.NANO:
            return "NANO";
        default:
            return "UNKNOWN";
    }
}
