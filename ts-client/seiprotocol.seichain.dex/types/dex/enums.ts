/* eslint-disable */
export const protobufPackage = "seiprotocol.seichain.dex";

export enum PositionDirection {
  LONG = 0,
  SHORT = 1,
  UNRECOGNIZED = -1,
}

export function positionDirectionFromJSON(object: any): PositionDirection {
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

export function positionDirectionToJSON(object: PositionDirection): string {
  switch (object) {
    case PositionDirection.LONG:
      return "LONG";
    case PositionDirection.SHORT:
      return "SHORT";
    default:
      return "UNKNOWN";
  }
}

export enum PositionEffect {
  OPEN = 0,
  CLOSE = 1,
  UNRECOGNIZED = -1,
}

export function positionEffectFromJSON(object: any): PositionEffect {
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

export function positionEffectToJSON(object: PositionEffect): string {
  switch (object) {
    case PositionEffect.OPEN:
      return "OPEN";
    case PositionEffect.CLOSE:
      return "CLOSE";
    default:
      return "UNKNOWN";
  }
}

export enum OrderType {
  LIMIT = 0,
  MARKET = 1,
  /** FOKMARKET - fill-or-kill market order */
  FOKMARKET = 3,
  /** FOKMARKETBYVALUE - fill-or-kill market by value order */
  FOKMARKETBYVALUE = 4,
  STOPLOSS = 5,
  STOPLIMIT = 6,
  UNRECOGNIZED = -1,
}

export function orderTypeFromJSON(object: any): OrderType {
  switch (object) {
    case 0:
    case "LIMIT":
      return OrderType.LIMIT;
    case 1:
    case "MARKET":
      return OrderType.MARKET;
    case 3:
    case "FOKMARKET":
      return OrderType.FOKMARKET;
    case 4:
    case "FOKMARKETBYVALUE":
      return OrderType.FOKMARKETBYVALUE;
    case 5:
    case "STOPLOSS":
      return OrderType.STOPLOSS;
    case 6:
    case "STOPLIMIT":
      return OrderType.STOPLIMIT;
    case -1:
    case "UNRECOGNIZED":
    default:
      return OrderType.UNRECOGNIZED;
  }
}

export function orderTypeToJSON(object: OrderType): string {
  switch (object) {
    case OrderType.LIMIT:
      return "LIMIT";
    case OrderType.MARKET:
      return "MARKET";
    case OrderType.FOKMARKET:
      return "FOKMARKET";
    case OrderType.FOKMARKETBYVALUE:
      return "FOKMARKETBYVALUE";
    case OrderType.STOPLOSS:
      return "STOPLOSS";
    case OrderType.STOPLIMIT:
      return "STOPLIMIT";
    default:
      return "UNKNOWN";
  }
}

export enum Unit {
  STANDARD = 0,
  MILLI = 1,
  MICRO = 2,
  NANO = 3,
  UNRECOGNIZED = -1,
}

export function unitFromJSON(object: any): Unit {
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

export function unitToJSON(object: Unit): string {
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

export enum OrderStatus {
  PLACED = 0,
  FAILED_TO_PLACE = 1,
  CANCELLED = 2,
  FULFILLED = 3,
  UNRECOGNIZED = -1,
}

export function orderStatusFromJSON(object: any): OrderStatus {
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

export function orderStatusToJSON(object: OrderStatus): string {
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

export enum CancellationInitiator {
  USER = 0,
  LIQUIDATED = 1,
  UNRECOGNIZED = -1,
}

export function cancellationInitiatorFromJSON(
  object: any
): CancellationInitiator {
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

export function cancellationInitiatorToJSON(
  object: CancellationInitiator
): string {
  switch (object) {
    case CancellationInitiator.USER:
      return "USER";
    case CancellationInitiator.LIQUIDATED:
      return "LIQUIDATED";
    default:
      return "UNKNOWN";
  }
}
