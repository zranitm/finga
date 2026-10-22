export interface BarProps {
  readonly label: string;
  readonly value: number;
  readonly max: number;
  readonly kind: 'hp' | 'mp' | 'exp';
  /** Shown instead of "value / max" when given (the EXP bar at the cap). */
  readonly text?: string;
}

/** A filled bar for HP, MP and EXP. */
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
        {label} {text ?? `${value} / ${max}`}
      </span>
    </div>
  );
}
