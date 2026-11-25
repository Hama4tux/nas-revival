/** Generic two-bay NAS illustration (no vendor branding). Decorative only. */
export function NasArt({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 120 140" className={className} aria-hidden focusable="false">
      <defs>
        <linearGradient id="nas-body" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#2b3640" />
          <stop offset="1" stopColor="#0d1318" />
        </linearGradient>
        <linearGradient id="nas-side" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stopColor="#151c22" />
          <stop offset="1" stopColor="#0a0f13" />
        </linearGradient>
        <radialGradient id="nas-glow" cx=".5" cy=".5" r=".5">
          <stop offset="0" stopColor="#2dd4bf" stopOpacity=".35" />
          <stop offset="1" stopColor="#2dd4bf" stopOpacity="0" />
        </radialGradient>
      </defs>
      <ellipse cx="60" cy="130" rx="46" ry="7" fill="url(#nas-glow)" />
      <path d="M88 14 L104 22 L104 124 L88 130 Z" fill="url(#nas-side)" />
      <rect x="18" y="14" width="70" height="116" rx="7" fill="url(#nas-body)" />
      <rect x="18.5" y="14.5" width="69" height="115" rx="6.5" fill="none" stroke="#ffffff" strokeOpacity=".08" />
      <rect x="26" y="24" width="44" height="44" rx="3" fill="#0b1014" opacity=".55" />
      <rect x="26" y="74" width="44" height="44" rx="3" fill="#0b1014" opacity=".55" />
      <g stroke="#ffffff" strokeOpacity=".06">
        <line x1="30" y1="34" x2="66" y2="34" /><line x1="30" y1="44" x2="66" y2="44" /><line x1="30" y1="54" x2="66" y2="54" />
        <line x1="30" y1="84" x2="66" y2="84" /><line x1="30" y1="94" x2="66" y2="94" /><line x1="30" y1="104" x2="66" y2="104" />
      </g>
      <circle cx="79" cy="28" r="4.5" fill="none" stroke="#38bdf8" strokeWidth="1.5" />
      <circle cx="79" cy="42" r="1.8" fill="#38bdf8" />
      <circle cx="79" cy="50" r="1.8" fill="#38bdf8" opacity=".7" />
      <circle cx="79" cy="58" r="1.8" fill="#2dd4bf" />
      <circle cx="79" cy="66" r="1.8" fill="#38bdf8" opacity=".35" />
    </svg>
  );
}
