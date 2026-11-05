import { describe, expect, it } from "vitest";
import { eurosToCents, formatEuro, parseQuantity, totalCents } from "./money";

describe("exact euro input", () => {
  it.each([
    ["0", 0],
    ["0.01", 1],
    ["0.29", 29],
    ["19.99", 1999],
    ["800", 80000],
    ["1,05", 105],
    [" 01.2 ", 120],
    ["90071992547409.91", Number.MAX_SAFE_INTEGER],
  ])("parses %s without floating-point rounding", (text, cents) => {
    expect(eurosToCents(text)).toBe(cents);
  });
  it.each([
    "",
    "-1",
    "1.005",
    "1,000.00",
    "1e3",
    "Infinity",
    "NaN",
    ".5",
    "90071992547409.92",
  ])("rejects ambiguous or unsafe input %s", (text) => {
    expect(() => eurosToCents(text)).toThrow();
  });
  it("checks quantity and multiplication safety", () => {
    expect(parseQuantity("2")).toBe(2);
    expect(totalCents(2, eurosToCents("800"))).toBe(160000);
    expect(totalCents(Number.MAX_SAFE_INTEGER, 0)).toBe(0);
    for (const text of ["0", "-2", "1.5", "1e2", "9007199254740992"])
      expect(() => parseQuantity(text)).toThrow();
    expect(() => totalCents(2, Number.MAX_SAFE_INTEGER)).toThrow();
  });
  it("formats cents exactly, including a large aggregate", () => {
    expect(formatEuro(29)).toBe("€0.29");
    expect(formatEuro(160000)).toBe("€1,600.00");
    expect(formatEuro(9007199254740991n)).toBe("€90,071,992,547,409.91");
    expect(() => formatEuro(Number.MAX_SAFE_INTEGER + 1)).toThrow();
  });
});
