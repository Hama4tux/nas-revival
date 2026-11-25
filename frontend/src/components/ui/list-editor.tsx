import { Plus, Trash2 } from 'lucide-react';
import { useId } from 'react';
import { cs } from '../../i18n/cs';
import { Button } from './button';
import { Input } from './input';

/** Editable list of text rows (add / remove) with a shared hint and error. */
export function ListEditor({ legend, hint, error, values, max, onChange, itemLabel, addLabel, placeholder, disabled }: {
  legend: string; hint?: string; error?: string | null; values: string[]; max: number;
  onChange: (v: string[]) => void; itemLabel: (n: number) => string; addLabel: string; placeholder?: string; disabled?: boolean;
}) {
  const id = useId();
  const desc = [hint ? `${id}-h` : '', error ? `${id}-e` : ''].filter(Boolean).join(' ') || undefined;
  return (
    <fieldset className="min-w-0 space-y-2" disabled={disabled} aria-describedby={desc}>
      <legend className="mb-1 text-sm font-medium">{legend}</legend>
      {values.map((v, i) => (
        <div key={i} className="flex items-center gap-2">
          <Input value={v} placeholder={placeholder} aria-label={itemLabel(i + 1)} aria-invalid={!!error} aria-describedby={desc}
            autoComplete="off" spellCheck={false} className="aria-[invalid=true]:border-danger"
            onChange={(e) => onChange(values.map((x, j) => (j === i ? e.target.value : x)))} />
          <Button type="button" variant="outline" size="icon" aria-label={`${cs.common.remove}: ${itemLabel(i + 1)}`}
            onClick={() => onChange(values.filter((_, j) => j !== i))}>
            <Trash2 className="h-4 w-4" aria-hidden />
          </Button>
        </div>
      ))}
      {values.length < max && (
        <Button type="button" variant="outline" size="sm" onClick={() => onChange([...values, ''])}>
          <Plus className="h-4 w-4" aria-hidden />{addLabel}
        </Button>
      )}
      {hint && <p id={`${id}-h`} className="text-xs text-muted-foreground">{hint}</p>}
      {error && <p id={`${id}-e`} role="alert" className="text-xs font-medium text-danger">{error}</p>}
    </fieldset>
  );
}
