// Accounting evidence and control limits are integer cents, never floats.
// Keep these helpers independent of the browser transport so overflow, signs
// and decimal input can be qualified without any Control API dependencies.
export function parseDollarCents(value: string): number | null {
  if (value.length > 20 || !/^(?:0|[1-9]\d*)(?:\.\d{1,2})?$/.test(value)) return null;
  const [dollars, fraction = ''] = value.split('.');
  const cents = BigInt(dollars) * 100n + BigInt(fraction.padEnd(2, '0'));
  return cents > 0n && cents <= BigInt(Number.MAX_SAFE_INTEGER) ? Number(cents) : null;
}
export function formatCents(value: number | string): string {
  if (typeof value === 'number' && !Number.isSafeInteger(value)) return 'Amount exceeds browser integer precision';
  const cents = BigInt(value);
  const sign = cents < 0n ? '-' : '';
  const magnitude = cents < 0n ? -cents : cents;
  return `${sign}$${magnitude / 100n}.${(magnitude % 100n).toString().padStart(2, '0')}`;
}
