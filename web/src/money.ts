const cents = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 2 });

/**
 * An amount of US dollars, zero or more, the one way Byname shows money:
 * "$0.00", "under $0.01", "$7.97", "$1,234.50". With "about" it is an
 * estimate, "about $10.83", and stays "under $0.01" or "$0.00" where
 * "about" would read oddly. internal/money does the same in Go, and both
 * are tested against internal/money/cases.json.
 */
export function money(usd: number, estimate?: "about"): string {
  if (usd > 0 && usd < 0.01) return "under $0.01";
  const s = cents.format(usd);
  return estimate && usd > 0 ? `about ${s}` : s;
}
