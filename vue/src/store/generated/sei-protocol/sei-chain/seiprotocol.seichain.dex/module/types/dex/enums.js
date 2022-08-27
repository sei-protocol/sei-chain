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
    /** FOKMARKET - fill-or-kill market order */
    OrderType[OrderType["FOKMARKET"] = 3] = "FOKMARKET";
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
        case 3:
        case "FOKMARKET":
            return OrderType.FOKMARKET;
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
        case OrderType.FOKMARKET:
            return "FOKMARKET";
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
export var OrderStatus;
(function (OrderStatus) {
    OrderStatus[OrderStatus["PLACED"] = 0] = "PLACED";
    OrderStatus[OrderStatus["FAILED_TO_PLACE"] = 1] = "FAILED_TO_PLACE";
    OrderStatus[OrderStatus["CANCELLED"] = 2] = "CANCELLED";
    OrderStatus[OrderStatus["FULFILLED"] = 3] = "FULFILLED";
    OrderStatus[OrderStatus["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(OrderStatus || (OrderStatus = {}));
export function orderStatusFromJSON(object) {
    switch (object) {
        case 0:
        case "PLACED":
            return OrderStatus.PLACED;
        case 1:
        case "FAILED_TO_PLACE":
            return OrderStatus.FAILED_TO_PLACE;
        case 2:
        case "CANCELLED":
            return OrderStatus.CANCELLED;
        case 3:
        case "FULFILLED":
            return OrderStatus.FULFILLED;
        case -1:
        case "UNRECOGNIZED":
        default:
            return OrderStatus.UNRECOGNIZED;
    }
}
export function orderStatusToJSON(object) {
    switch (object) {
        case OrderStatus.PLACED:
            return "PLACED";
        case OrderStatus.FAILED_TO_PLACE:
            return "FAILED_TO_PLACE";
        case OrderStatus.CANCELLED:
            return "CANCELLED";
        case OrderStatus.FULFILLED:
            return "FULFILLED";
        default:
            return "UNKNOWN";
    }
}
export var CancellationInitiator;
(function (CancellationInitiator) {
    CancellationInitiator[CancellationInitiator["USER"] = 0] = "USER";
    CancellationInitiator[CancellationInitiator["LIQUIDATED"] = 1] = "LIQUIDATED";
    CancellationInitiator[CancellationInitiator["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(CancellationInitiator || (CancellationInitiator = {}));
export function cancellationInitiatorFromJSON(object) {
    switch (object) {
        case 0:
        case "USER":
            return CancellationInitiator.USER;
        case 1:
        case "LIQUIDATED":
            return CancellationInitiator.LIQUIDATED;
        case -1:
        case "UNRECOGNIZED":
        default:
            return CancellationInitiator.UNRECOGNIZED;
    }
}
export function cancellationInitiatorToJSON(object) {
    switch (object) {
        case CancellationInitiator.USER:
            return "USER";
        case CancellationInitiator.LIQUIDATED:
            return "LIQUIDATED";
        default:
            return "UNKNOWN";
    }
}
