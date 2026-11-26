import { Fan, Lightbulb, Thermometer, TriangleAlert } from 'lucide-react';
import { useCallback, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type {
  FanProfileId, HardwareHistory, HardwarePoint, HardwareSettings, HardwareState, HardwareThresholds, LedSettings,
} from '../api/types';
import { useVisiblePoll } from '../hooks/useVisiblePoll';
import { cs } from '../i18n/cs';
import { historySince, maxSeries, mergeHardwareHistory, pwmPercent, tempLevel, thresholdError } from '../lib/hardware';
import { AreaChart } from './AreaChart';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { Widget } from './ui/card';
import { Field, invalidCls } from './ui/field';
import { Input } from './ui/input';
import { Skeleton } from './ui/skeleton';

const t = cs.hw;
const levelVariant = { ok: 'success', warn: 'warning', crit: 'danger' } as const;
const fmtC = (c: number) => `${c.toLocaleString('cs-CZ', { maximumFractionDigits: 1 })} °C`;

function apiMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return cs.errors.network;
    if (e.status === 503) return cs.errors.helperDown;
    if (e.status === 400 || e.status === 409 || e.status === 500) return e.message;
  }
  return cs.errors.generic;
}

const Dot = ({ tone }: { tone: 'primary' | 'warning' }) => (
  <span aria-hidden className={`mr-1.5 inline-block h-2 w-2 rounded-full ${tone === 'primary' ? 'bg-primary' : 'bg-warning'}`} />
);

function spanText(points: HardwarePoint[]): string {
  if (points.length < 2) return t.span(0);
  return t.span(Math.round((points[points.length - 1]!.t - points[0]!.t) / 3_600_000));
}

/* ---------- temperatures ---------- */

function TempsWidget({ st, points }: { st: HardwareState; points: HardwarePoint[] }) {
  const th = st.settings.thresholds;
  const board = st.sensors.filter((s) => s.kind === 'board').map((s) => s.id);
  const disk = st.sensors.filter((s) => s.kind === 'disk').map((s) => s.id);
  const b = maxSeries(points, board), d = maxSeries(points, disk);
  const peak = Math.max(0, ...b, ...d);
  return (
    <Widget id="hw-temps" title={t.temps} icon={<Thermometer />} className="lg:col-span-3">
      {points.length < 2 ? <p className="text-sm text-muted-foreground">{t.noHistory}</p> : (
        <figure>
          <AreaChart label={t.tempChart} series={[{ values: b, tone: 'primary' }, ...(d.length ? [{ values: d, tone: 'warning' as const }] : [])]} />
          <figcaption className="mt-1 flex flex-wrap justify-between gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span><Dot tone="primary" />{t.board}{d.length > 0 && <><span className="mx-2" /><Dot tone="warning" />{t.disk}</>}</span>
            <span className="tabular-nums">{t.peak} {fmtC(peak)} · {spanText(points)}</span>
          </figcaption>
        </figure>
      )}
      <ul className="mt-3 divide-y divide-border text-sm">
        {st.sensors.map((s) => {
          const lv = tempLevel(s, th);
          return (
            <li key={s.id} className="flex flex-wrap items-center justify-between gap-2 py-1.5">
              <span className="min-w-0 break-all">
                <span className="text-muted-foreground">{s.kind === 'disk' ? t.diskKind : t.boardKind} · </span>{s.label}
              </span>
              <span className="flex items-center gap-2">
                <span className="font-medium tabular-nums">{s.c !== null ? fmtC(s.c) : s.standby ? t.sleeping : t.noValue}</span>
                {s.c !== null && <Badge variant={levelVariant[lv]}>{t.level[lv]}</Badge>}
              </span>
            </li>
          );
        })}
      </ul>
    </Widget>
  );
}

/* ---------- thresholds ---------- */

const FIELDS: (keyof HardwareThresholds)[] = ['boardWarnC', 'boardCritC', 'diskWarnC', 'diskCritC'];

function ThresholdsWidget({ value, readOnly, onSaved }: { value: HardwareThresholds; readOnly: boolean; onSaved: (s: HardwareSettings) => void }) {
  const [form, setForm] = useState<Record<keyof HardwareThresholds, string>>(() => strings(value));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<{ field: string | null; message: string } | null>(null);
  const [ok, setOk] = useState(false);
  const nums = Object.fromEntries(FIELDS.map((k) => [k, Number(form[k])])) as unknown as HardwareThresholds;
  const local = thresholdError(nums);
  const changed = FIELDS.some((k) => nums[k] !== value[k]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setOk(false);
    if (local) { setErr({ field: local, message: t.thresholdErr[local]! }); return; }
    setBusy(true); setErr(null);
    try {
      const s = await api<HardwareSettings>('/hardware/thresholds', { method: 'PUT', body: nums });
      onSaved(s); setForm(strings(s.thresholds)); setOk(true);
    } catch (x) { setErr({ field: x instanceof ApiError ? x.field : null, message: apiMessage(x) }); }
    finally { setBusy(false); }
  }

  return (
    <Widget id="hw-thresholds" title={t.thresholds} icon={<TriangleAlert />} className="lg:col-span-2">
      <form onSubmit={(e) => void save(e)} noValidate className="space-y-3">
        <p className="text-sm text-muted-foreground">{t.thresholdsHint}</p>
        <div className="grid grid-cols-2 gap-3">
          {FIELDS.map((k) => (
            <Field key={k} label={t[k]} error={err?.field === k ? err.message : null}>
              {(p) => <Input {...p} type="number" inputMode="numeric" min={30} max={90} step={1} value={form[k]} disabled={readOnly || busy}
                className={invalidCls} onChange={(e) => { setForm({ ...form, [k]: e.target.value }); setErr(null); setOk(false); }} />}
            </Field>
          ))}
        </div>
        {err && err.field === null && <p role="alert" className="text-sm font-medium text-danger">{err.message}</p>}
        {!readOnly && (
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={busy || !changed}>{busy ? cs.common.saving : cs.common.save}</Button>
            {ok && <span role="status" className="text-sm text-success">{t.saved}</span>}
          </div>
        )}
      </form>
    </Widget>
  );
}

function strings(v: HardwareThresholds): Record<keyof HardwareThresholds, string> {
  return { boardWarnC: String(v.boardWarnC), boardCritC: String(v.boardCritC), diskWarnC: String(v.diskWarnC), diskCritC: String(v.diskCritC) };
}

/* ---------- fan ---------- */

function FanWidget({ st, points, readOnly, onSaved }: { st: HardwareState; points: HardwarePoint[]; readOnly: boolean; onSaved: (s: HardwareSettings) => void }) {
  const [choice, setChoice] = useState<FanProfileId>(st.settings.fanProfile);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const f = st.fan;
  const pwm = points.map((p) => p.pwmPercent).filter((v): v is number => v !== null);
  // The chip is in the chosen profile only in automatic mode with matching limits.
  const needsApply = choice !== st.settings.fanProfile || f.mode !== 'auto' || f.profile !== st.settings.fanProfile;

  async function apply() {
    setBusy(true); setMsg(null);
    try {
      onSaved(await api<HardwareSettings>('/hardware/fan', { method: 'PUT', body: { profile: choice } }));
      setMsg({ ok: true, text: t.profileDone });
    } catch (x) { setMsg({ ok: false, text: apiMessage(x) }); }
    finally { setBusy(false); }
  }

  return (
    <Widget id="hw-fan" title={t.fan} icon={<Fan />} className="lg:col-span-3"
      right={<span className="text-lg font-semibold tabular-nums">{st.fanRpm !== null ? t.rpm(st.fanRpm) : t.modes.unknown}</span>}>
      {pwm.length >= 2 && (
        <figure className="mb-3">
          <AreaChart label={t.fanChart} series={[{ values: pwm, tone: 'primary' }]} />
          <figcaption className="mt-1 text-right text-xs text-muted-foreground tabular-nums">{t.pwm} · {spanText(points)}</figcaption>
        </figure>
      )}
      <dl className="grid gap-x-6 text-sm sm:grid-cols-2">
        <div className="flex justify-between gap-3 py-0.5"><dt className="text-muted-foreground">{t.mode}</dt><dd className="font-medium">{t.modes[f.mode]}</dd></div>
        <div className="flex justify-between gap-3 py-0.5"><dt className="text-muted-foreground">{t.pwm}</dt><dd className="font-medium tabular-nums">{f.pwmPercent !== null ? `${f.pwmPercent} %` : t.noValue}</dd></div>
      </dl>
      {f.mode !== 'auto' && f.mode !== 'unknown' && <p role="status" className="mt-2 text-sm text-warning">{t.notAuto}</p>}
      {f.mode === 'auto' && f.profile === '' && <p role="status" className="mt-2 text-sm text-warning">{t.customProfile}</p>}
      <fieldset className="mt-3 space-y-2 border-t border-border/70 pt-3" disabled={readOnly || busy}>
        <legend className="mb-1 text-sm font-medium">{t.profile}</legend>
        {st.profiles.map((p) => (
          <label key={p.id} className="flex cursor-pointer items-start gap-2 text-sm">
            <input type="radio" name="fan-profile" className="mt-1" checked={choice === p.id} onChange={() => { setChoice(p.id); setMsg(null); }} />
            <span>
              <span className="font-medium">{t.profiles[p.id]}</span>
              {p.id === st.settings.fanProfile && f.profile === p.id && <Badge variant="success" className="ml-2">{t.active}</Badge>}
              <span className="block text-xs text-muted-foreground">{t.profileHint(p.zoneMinC, p.zoneMaxC, pwmPercent(p.pwmMin))}</span>
            </span>
          </label>
        ))}
      </fieldset>
      <p className="mt-2 text-xs text-muted-foreground">{t.critNote(st.zoneCritC)}</p>
      {!readOnly && (
        <div className="mt-3 flex flex-wrap items-center gap-3">
          <Button type="button" onClick={() => void apply()} disabled={busy || !needsApply}>{busy ? t.applying : t.applyProfile}</Button>
          {msg && <span role={msg.ok ? 'status' : 'alert'} className={`text-sm ${msg.ok ? 'text-success' : 'font-medium text-danger'}`}>{msg.text}</span>}
        </div>
      )}
    </Widget>
  );
}

/* ---------- LEDs ---------- */

const hm = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;

function LedsWidget({ st, readOnly, onSaved }: { st: HardwareState; readOnly: boolean; onSaved: (s: HardwareSettings) => void }) {
  const [form, setForm] = useState<LedSettings>(st.settings.leds);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const n = form.night;
  const timeErr = !hm.test(n.start) || !hm.test(n.end) || n.start === n.end;
  const changed = JSON.stringify(form) !== JSON.stringify(st.settings.leds);
  const nightNow = st.settings.leds.night.enabled && st.leds.some((l) => l.name === 'nsa310:green:sys' && l.trigger === 'none');
  const set = (p: Partial<LedSettings>) => { setForm({ ...form, ...p }); setMsg(null); };

  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (timeErr) { setMsg({ ok: false, text: t.nightTimeErr }); return; }
    setBusy(true); setMsg(null);
    try {
      const s = await api<HardwareSettings>('/hardware/leds', { method: 'PUT', body: form });
      onSaved(s); setForm(s.leds); setMsg({ ok: true, text: t.saved });
    } catch (x) { setMsg({ ok: false, text: apiMessage(x) }); }
    finally { setBusy(false); }
  }

  return (
    <Widget id="hw-leds" title={t.leds} icon={<Lightbulb />} className="lg:col-span-2">
      <form onSubmit={(e) => void save(e)} noValidate className="space-y-3">
        <fieldset className="space-y-2" disabled={readOnly || busy}>
          <label className="flex items-center gap-2 text-sm font-medium">
            <input type="checkbox" checked={n.enabled} onChange={(e) => set({ night: { ...n, enabled: e.target.checked } })} />{t.night}
          </label>
          <p className="text-xs text-muted-foreground">{t.nightHint}</p>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t.nightFrom} error={timeErr && !hm.test(n.start) ? t.nightTimeErr : null}>
              {(p) => <Input {...p} type="time" value={n.start} className={invalidCls} onChange={(e) => set({ night: { ...n, start: e.target.value } })} />}
            </Field>
            <Field label={t.nightTo} error={timeErr && hm.test(n.start) ? t.nightTimeErr : null}>
              {(p) => <Input {...p} type="time" value={n.end} className={invalidCls} onChange={(e) => set({ night: { ...n, end: e.target.value } })} />}
            </Field>
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={form.faultBlink} onChange={(e) => set({ faultBlink: e.target.checked })} />{t.faultBlink}
          </label>
        </fieldset>
        {nightNow && <p role="status" className="text-sm text-muted-foreground">{t.nightNow}</p>}
        {!readOnly && (
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={busy || !changed}>{busy ? cs.common.saving : cs.common.save}</Button>
            {msg && <span role={msg.ok ? 'status' : 'alert'} className={`text-sm ${msg.ok ? 'text-success' : 'font-medium text-danger'}`}>{msg.text}</span>}
          </div>
        )}
      </form>
      <details className="mt-4 border-t border-border/70 pt-3">
        <summary className="cursor-pointer text-sm font-medium">{t.ledList}</summary>
        <ul className="mt-2 divide-y divide-border text-sm">
          {st.leds.map((l) => (
            <li key={l.name} className="flex flex-wrap justify-between gap-2 py-1">
              <span className={l.red ? 'text-danger' : undefined}>{t.ledNames[l.name] ?? l.name}</span>
              <span className="text-muted-foreground">{t.triggers[l.trigger] ?? l.trigger} · {l.on ? t.ledOn : t.ledOff}</span>
            </li>
          ))}
        </ul>
      </details>
    </Widget>
  );
}

/* ---------- page ---------- */

/** Control panel → Hardware (W3.4): state every 10 s, history once a minute, only while visible. */
export function HardwarePanel({ readOnly }: { readOnly: boolean }) {
  const [st, setSt] = useState<HardwareState | null>(null);
  const [points, setPoints] = useState<HardwarePoint[]>([]);
  const [failed, setFailed] = useState(false);
  const pts = useRef<HardwarePoint[]>([]);
  const ticks = useRef(0);

  const poll = useCallback(() => {
    api<HardwareState>('/hardware').then((s) => { setSt(s); setFailed(false); }).catch(() => setFailed(true));
    if (ticks.current++ % 6 === 0) {
      api<HardwareHistory>(`/hardware/history?since=${historySince(pts.current)}`)
        .then((h) => { pts.current = mergeHardwareHistory(pts.current, h.points); setPoints(pts.current); })
        .catch(() => {});
    }
  }, []);
  useVisiblePoll(poll, 10_000);

  if (!st) {
    return failed
      ? <p role="alert" className="text-sm text-danger">{t.loadError}</p>
      : <div aria-busy="true" aria-label={cs.common.loading}><Skeleton className="h-64 w-full" /></div>;
  }
  const saved = (s: HardwareSettings) => setSt({ ...st, settings: s });
  return (
    <div className="grid items-start gap-4 lg:grid-cols-5">
      {failed && <p role="alert" className="text-sm text-danger lg:col-span-5">{t.loadError}</p>}
      <TempsWidget st={st} points={points} />
      <ThresholdsWidget value={st.settings.thresholds} readOnly={readOnly} onSaved={saved} />
      <FanWidget st={st} points={points} readOnly={readOnly} onSaved={(s) => { saved(s); poll(); }} />
      <LedsWidget st={st} readOnly={readOnly} onSaved={(s) => { saved(s); poll(); }} />
    </div>
  );
}
