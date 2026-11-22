/** The average calendar month, as internal/config counts it. */
const daysPerMonth = 365.25 / 12;

/** What a check costs in an average month when it runs every few days. */
export const monthOfChecks = (perCheck: number, everyDays: number) => (everyDays > 0 ? (perCheck * daysPerMonth) / everyDays : 0);
