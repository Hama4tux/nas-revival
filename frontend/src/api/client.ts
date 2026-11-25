export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public retryAfter: number | null = null,
    public field: string | null = null,
  ) {
    super(message);
  }
}

type Unauthorized = () => void;
let onUnauthorized: Unauthorized = () => {};
export const setUnauthorizedHandler = (fn: Unauthorized) => {
  onUnauthorized = fn;
};

/** `quiet401`: do not trigger the global logout handling (used for the login probe / login form). */
export async function api<T>(
  path: string,
  opts: { method?: string; body?: unknown; quiet401?: boolean; signal?: AbortSignal } = {},
): Promise<T> {
  const method = opts.method ?? 'GET';
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (method !== 'GET') headers['X-Requested-With'] = 'nas-revival';
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';

  let res: Response;
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      headers,
      credentials: 'same-origin',
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      signal: opts.signal,
    });
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e;
    throw new ApiError(0, 'network', 'network');
  }

  if (res.ok) {
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  }

  let code = 'error';
  let message = res.statusText;
  let field: string | null = null;
  try {
    const j = (await res.json()) as { error?: { code?: string; message?: string; field?: string } };
    code = j.error?.code ?? code;
    message = j.error?.message ?? message;
    field = j.error?.field ?? null;
  } catch {
    /* non-JSON error body */
  }
  const ra = Number(res.headers.get('Retry-After'));
  const err = new ApiError(res.status, code, message, Number.isFinite(ra) && ra > 0 ? ra : null, field);
  if (res.status === 401 && !opts.quiet401) onUnauthorized();
  throw err;
}
