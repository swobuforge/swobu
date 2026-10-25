import { describe, expect, it } from 'vitest';
import { formatCents, parseDollarCents } from './currency';

describe('spending policy money boundaries', () => {
  it('converts exact USD decimal strings to positive integer cents', () => {
    expect(parseDollarCents('1')).toBe(100);
    expect(parseDollarCents('1.2')).toBe(120);
    expect(parseDollarCents('0.01')).toBe(1);
    expect(parseDollarCents('10.00')).toBe(1000);
  });
  it('rejects negative, zero, float precision and overflow before preview', () => {
    for (const value of ['0', '0.00', '-1', '+1', '0.001', '1.234', '1e6', ' 1', '1 ', '01', '99999999999999999999'])
      expect(parseDollarCents(value)).toBeNull();
  });
  it('renders signed large retained cent totals without floating point conversion', () => {
    expect(formatCents('9007199254740994')).toBe('$90071992547409.94');
    expect(formatCents('-123')).toBe('-$1.23');
    expect(formatCents(Number.MAX_SAFE_INTEGER + 1)).toBe('Amount exceeds browser integer precision');
  });
});
