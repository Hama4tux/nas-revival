/** Smooth area chart for one or two series sharing a y-axis starting at 0. */
export function AreaChart({ series, label, className }: {
  series: { values: number[]; tone: 'primary' | 'accent' }[]; label: string; className?: string;
}) {
  const W = 300, H = 80, P = 2;
  const n = Math.max(...series.map((s) => s.values.length), 0);
  if (n < 2) return <div role="img" aria-label={label} className={`h-24 w-full rounded-lg bg-muted/40 ${className ?? ''}`} />;
  const hi = Math.max(1, ...series.flatMap((s) => s.values)) * 1.15;
  const path = (vals: number[]) => {
    // Right-align shorter series so the newest samples line up.
    const off = n - vals.length;
    const pts = vals.map((v, i) => [P + ((i + off) / (n - 1)) * (W - 2 * P), H - P - (Math.max(0, v) / hi) * (H - 2 * P)] as const);
    let d = `M${pts[0]![0].toFixed(1)},${pts[0]![1].toFixed(1)}`;
    for (let i = 1; i < pts.length; i++) {
      const [x0, y0] = pts[i - 1]!;
      const [x1, y1] = pts[i]!;
      const mx = (x0 + x1) / 2;
      d += ` C${mx.toFixed(1)},${y0.toFixed(1)} ${mx.toFixed(1)},${y1.toFixed(1)} ${x1.toFixed(1)},${y1.toFixed(1)}`;
    }
    return { line: d, area: `${d} L${pts[pts.length - 1]![0].toFixed(1)},${H} L${pts[0]![0].toFixed(1)},${H} Z` };
  };
  return (
    <svg role="img" aria-label={label} viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className={`h-24 w-full ${className ?? ''}`}>
      <defs>
        {(['primary', 'accent'] as const).map((t) => (
          <linearGradient key={t} id={`ac-${t}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stopColor={`hsl(var(--${t}))`} stopOpacity=".35" />
            <stop offset="1" stopColor={`hsl(var(--${t}))`} stopOpacity="0" />
          </linearGradient>
        ))}
      </defs>
      {series.filter((s) => s.values.length >= 2).map((s) => {
        const p = path(s.values);
        return (
          <g key={s.tone}>
            <path d={p.area} fill={`url(#ac-${s.tone})`} />
            <path d={p.line} fill="none" stroke={`hsl(var(--${s.tone}))`} strokeWidth="2" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
          </g>
        );
      })}
    </svg>
  );
}
