export interface BarProps {
  readonly label: string;
  readonly value: number;
  readonly max: number;
  readonly kind: 'hp' | 'exp';
  /** Replaces "value / max", e.g. the EXP bar at the cap. */
  readonly text?: string;
  /** The meter's accessible name where several bars share a label, e.g. a party member's "HP"; the label by default. */
  readonly name?: string;
  /** A shield over HP: a pale part from the fill's end, ending at the bar's end at most, and "+shield" after the numbers. */
  readonly shield?: number;
}

/** A label too long for the bar, such as a party member's name, ends in an ellipsis; the numbers always show. */
export function Bar({ label, value, max, kind, text, name, shield = 0 }: BarProps) {
  const ratio = max > 0 ? Math.min(1, Math.max(0, value / max)) : 0;
  const shown = max > 0 ? Math.min(1, shield / max) : 0;
  const from = Math.min(ratio, 1 - shown);
  return (
    <div
      className={`bar bar-${kind}`}
      role="meter"
      aria-label={name ?? label}
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuenow={Math.min(max, Math.max(0, value))}
    >
      <div className="bar-fill" style={{ width: `${(ratio * 100).toFixed(1)}%` }} />
      {shield > 0 && (
        <div
          className="bar-shield"
          style={{
            left: `${(from * 100).toFixed(1)}%`,
            width: `${(shown * 100).toFixed(1)}%`,
          }}
        />
      )}
      <span className="bar-text">
        <span className="bar-label" title={label}>
          {label}
        </span>{' '}
        <span className="bar-value">
          {text ?? `${value} / ${max}`}
          {shield > 0 && <span className="bar-shield-value"> +{shield}</span>}
        </span>
      </span>
    </div>
  );
}
