import type { ButtonHTMLAttributes } from 'react';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  readonly variant?: 'primary' | 'quiet' | 'danger';
}

/** A button with the game's look; pointer events are on so it works over the canvas. */
export function Button({
  variant = 'primary',
  className = '',
  type = 'button',
  ...rest
}: ButtonProps) {
  return <button type={type} className={`btn btn-${variant} ${className}`.trim()} {...rest} />;
}
