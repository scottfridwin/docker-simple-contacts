type IconProps = { className?: string };

const base = {
  viewBox: '0 0 20 20',
  width: 16,
  height: 16,
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 2,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
  'aria-hidden': true,
};

export function PlusIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className}>
      <path d="M10 3v14M3 10h14" />
    </svg>
  );
}

export function TrashIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className}>
      <path d="M4 6h12M8 6V4h4v2M6 6l.7 10a1 1 0 0 0 1 .9h4.6a1 1 0 0 0 1-.9L14 6" />
    </svg>
  );
}

export function SyncIcon({ className }: IconProps) {
  return (
    <svg {...base} width={18} height={18} className={className}>
      <path d="M4 8a6 6 0 0 1 10.5-3.5M16 4v3.5h-3.5" />
      <path d="M16 12a6 6 0 0 1-10.5 3.5M4 16v-3.5h3.5" />
    </svg>
  );
}

export function CloseIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className}>
      <path d="M5 5l10 10M15 5L5 15" />
    </svg>
  );
}
