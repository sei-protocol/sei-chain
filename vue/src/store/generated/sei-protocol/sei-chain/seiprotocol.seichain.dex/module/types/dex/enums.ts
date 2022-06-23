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
  LIQUIDATION = 2,
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
    case 2:
    case "LIQUIDATION":
      return OrderType.LIQUIDATION;
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
    case OrderType.LIQUIDATION:
      return "LIQUIDATION";
    default:
      return "UNKNOWN";
  }
}

export enum Denom {
  SEI = 0,
  ATOM = 1,
  BTC = 2,
  ETH = 3,
  SOL = 4,
  AVAX = 5,
  USDC = 6,
  NEAR = 7,
  OSMO = 8,
  UNRECOGNIZED = -1,
}

export function denomFromJSON(object: any): Denom {
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

export function denomToJSON(object: Denom): string {
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
