import { describe, it, expect } from 'vitest';
import { verdictVariant, signalVariant } from './idCheckPresentation';

describe('verdictVariant', () => {
  it('maps each verdict to a semantic variant', () => {
    expect(verdictVariant('strong')).toBe('success');
    expect(verdictVariant('mixed')).toBe('warning');
    expect(verdictVariant('weak')).toBe('error');
  });
});

describe('signalVariant', () => {
  it('maps each signal status to a semantic variant', () => {
    expect(signalVariant('pass')).toBe('success');
    expect(signalVariant('warn')).toBe('warning');
    expect(signalVariant('neutral')).toBe('info');
    expect(signalVariant('unknown')).toBe('neutral');
  });
});
