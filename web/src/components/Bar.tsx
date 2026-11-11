export interface BarProps {
  readonly label: string;
  readonly value: number;
  readonly max: number;
  readonly kind: 'hp' | 'mp' | 'exp';
  /** Replaces "value / max", e.g. the EXP bar at the cap. */
  readonly text?: string;
}

/** A label too long for the bar, such as a party member's name, ends in an ellipsis; the numbers always show. */
export function Bar({ label, value, max, kind, text }: BarProps) {
  const ratio = max > 0 ? Math.min(1, Math.max(0, value / max)) : 0;
  return (
    <div
      className={`bar bar-${kind}`}
      role="meter"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuenow={Math.min(max, Math.max(0, value))}
    >
      <div className="bar-fill" style={{ width: `${(ratio * 100).toFixed(1)}%` }} />
      <span className="bar-text">
        <span className="bar-label" title={label}>
          {label}
        </span>{' '}
        <span className="bar-value">{text ?? `${value} / ${max}`}</span>
      </span>
    </div>
  );
}
