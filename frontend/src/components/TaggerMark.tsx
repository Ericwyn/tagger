import type {SVGProps} from 'react';

export function TaggerMark(props: SVGProps<SVGSVGElement>) {
  return (
    <svg
      {...props}
      viewBox="0 0 64 64"
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      <circle cx="32" cy="32" r="26" fill="currentColor" />
      <circle cx="32" cy="32" r="10" fill="var(--paper-2)" />
      <rect x="22" y="12.5" width="20" height="6" rx="3" fill="var(--accent)" />
    </svg>
  );
}
