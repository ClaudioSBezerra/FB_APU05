import { describe, expect, it } from 'vitest';
import { paraDecimal } from './ValorInput';

describe('paraDecimal', () => {
  it.each([
    ['5000', '5000'],
    ['5.000', '5000'],
    ['5.000,50', '5000.50'],
    ['1.234,56', '1234.56'],
    ['R$ 1.234,56', '1234.56'],
    ['500000', '500000'],
    ['5,5', '5.5'],
    ['5.5', '5.5'],
    ['1.234.567', '1234567'],
    ['0,5', '0.5'],
    ['abc', ''],
  ])('%s -> %s', (entrada, esperado) => {
    expect(paraDecimal(entrada)).toBe(esperado);
  });
});
