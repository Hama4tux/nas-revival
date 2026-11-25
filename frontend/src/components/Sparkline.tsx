export function Sparkline({ values, label, min, max, className }: {
  values: number[]; label: string; min?: number; max?: number; className?: string;
}) {
  const W = 120, H = 32, P = 2;
  if (values.length < 2) {
    return <div role="img" aria-label={label} className={`h-8 w-full rounded bg-muted/50 ${className ?? ''}`} />;
  }
  const lo = min ?? Math.min(...values);
  let hi = max ?? Math.max(...values);
  if (hi - lo < 1) hi = lo + 1;
  const pts = values.map((v, i) => {
    const x = P + (i / (values.length - 1)) * (W - 2 * P);
    const y = H - P - ((Math.min(hi, Math.max(lo, v)) - lo) / (hi - lo)) * (H - 2 * P);
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
  return (
    <svg role="img" aria-label={label} viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none"
      className={`h-8 w-full text-primary ${className ?? ''}`}>
      <polygon points={`${P},${H - P} ${pts.join(' ')} ${W - P},${H - P}`} fill="currentColor" opacity="0.12" />
      <polyline points={pts.join(' ')} fill="none" stroke="currentColor" strokeWidth="1.5"
        strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}
