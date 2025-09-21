const ADDRESS_REGEX = /^0x[0-9a-fA-F]{40}$/;

export function isAddress(value) {
  if (typeof value !== "string") return false;
  return ADDRESS_REGEX.test(value.trim());
}

export function normalizeAddress(value) {
  if (!isAddress(value)) {
    throw new Error(`Invalid address: ${value}`);
  }
  const normalised = value.startsWith("0x") ? value : `0x${value}`;
  return normalised.toLowerCase();
}

function bigTen(exp) {
  if (exp < 0) {
    throw new Error("Negative decimal precision is not supported");
  }
  let result = 1n;
  for (let i = 0; i < exp; i += 1) {
    result *= 10n;
  }
  return result;
}

export function parseUnits(value, decimals) {
  if (typeof decimals !== "number" || !Number.isInteger(decimals) || decimals < 0) {
    throw new Error(`Invalid decimals value: ${decimals}`);
  }
  if (typeof value === "bigint") {
    return value;
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) {
      throw new Error("Cannot parse non-finite number");
    }
    value = value.toString();
  }
  if (typeof value !== "string") {
    throw new Error(`Unsupported amount type: ${typeof value}`);
  }
  if (value.startsWith("0x") || value.startsWith("-0x")) {
    return BigInt(value);
  }
  let raw = value.trim();
  let negative = false;
  if (raw.startsWith("-")) {
    negative = true;
    raw = raw.slice(1);
  }
  if (!/^\d*(\.\d*)?$/.test(raw)) {
    throw new Error(`Invalid decimal string: ${value}`);
  }
  let [integer = "0", fraction = ""] = raw.split(".");
  if (integer === "") integer = "0";
  if (fraction.length > decimals) {
    fraction = fraction.slice(0, decimals);
  }
  const paddedFraction = (fraction + "0".repeat(decimals)).slice(0, decimals);
  const base = BigInt(integer) * bigTen(decimals);
  const frac = paddedFraction ? BigInt(paddedFraction) : 0n;
  let result = base + frac;
  if (negative) {
    result = -result;
  }
  return result;
}

export function formatUnits(value, decimals) {
  if (typeof decimals !== "number" || !Number.isInteger(decimals) || decimals < 0) {
    throw new Error(`Invalid decimals value: ${decimals}`);
  }
  let bigintValue = typeof value === "bigint" ? value : BigInt(value);
  const negative = bigintValue < 0n;
  if (negative) {
    bigintValue = -bigintValue;
  }
  const base = bigTen(decimals);
  const integerPart = bigintValue / base;
  const fractionPart = bigintValue % base;
  let fraction = fractionPart.toString().padStart(decimals, "0");
  fraction = fraction.replace(/0+$/, "");
  const sign = negative ? "-" : "";
  if (fraction.length === 0) {
    return `${sign}${integerPart.toString()}.0`;
  }
  return `${sign}${integerPart.toString()}.${fraction}`;
}
