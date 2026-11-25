/** The average calendar month, as internal/config counts it. */
const daysPerMonth = 365.25 / 12;

/** What a check costs in an average month when it runs every few days. */
export const monthOfChecks = (perCheck: number, everyDays: number) => (everyDays > 0 ? (perCheck * daysPerMonth) / everyDays : 0);

/**
 * The monthly spend cap finishing onboarding sets when none is set: three
 * times the monthly estimate, rounded up to a whole dollar, and at least $5.
 * internal/config's DefaultMonthCap does the same, and both are tested
 * against internal/config/monthcap.json.
 */
export const defaultMonthCap = (monthUSD: number) => Math.max(5, Math.ceil(Math.round(3 * monthUSD * 100) / 100));
