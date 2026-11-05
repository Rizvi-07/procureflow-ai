const maxSafe = BigInt(Number.MAX_SAFE_INTEGER);
const wholeEuros = new Intl.NumberFormat("en-IE", { maximumFractionDigits: 0 });

// Parse decimal text, never a floating-point euro multiplication or rounding.
// A comma or dot is accepted as the decimal separator; grouping is not accepted.
export function eurosToCents(value: string): number {
  const match = /^(\d+)(?:[.,](\d{1,2}))?$/.exec(value.trim());
  if (!match)
    throw new Error(
      "Enter a nonnegative euro amount with up to two decimal places, such as 19.99.",
    );
  const cents =
    BigInt(match[1]) * 100n + BigInt((match[2] ?? "").padEnd(2, "0"));
  if (cents > maxSafe)
    throw new Error(
      "This amount is too large for an exact browser calculation.",
    );
  return Number(cents);
}

export function parseQuantity(value: string): number {
  if (!/^\d+$/.test(value.trim()))
    throw new Error("Enter a whole quantity greater than zero.");
  const quantity = BigInt(value.trim());
  if (quantity < 1n || quantity > maxSafe)
    throw new Error(
      "Quantity must be a positive, safely representable whole number.",
    );
  return Number(quantity);
}

export function totalCents(quantity: number, unitCents: number): number {
  if (
    !Number.isSafeInteger(quantity) ||
    quantity < 1 ||
    !Number.isSafeInteger(unitCents) ||
    unitCents < 0
  )
    throw new Error("Enter a valid quantity and unit price.");
  const total = BigInt(quantity) * BigInt(unitCents);
  if (total > maxSafe)
    throw new Error(
      "The request total is too large for an exact browser calculation.",
    );
  return Number(total);
}

export function formatEuro(cents: number | bigint): string {
  if (typeof cents === "number" && (!Number.isSafeInteger(cents) || cents < 0))
    throw new Error("Cannot display an inexact amount.");
  const amount = BigInt(cents);
  return `€${wholeEuros.format(amount / 100n)}.${String(amount % 100n).padStart(2, "0")}`;
}
