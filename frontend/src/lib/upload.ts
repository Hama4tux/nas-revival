import { ApiError } from '../api/client';

/** One upload: PUT of the raw file with progress. fetch() has no upload progress, hence XMLHttpRequest. */
export type UploadFn = (url: string, file: Blob, onProgress: (sent: number) => void, signal: AbortSignal) => Promise<void>;

const xhrUpload: UploadFn = (url, file, onProgress, signal) => new Promise((resolve, reject) => {
  const x = new XMLHttpRequest();
  x.open('PUT', url);
  x.setRequestHeader('X-Requested-With', 'nas-revival');
  x.setRequestHeader('Content-Type', 'application/octet-stream');
  x.upload.onprogress = (e) => onProgress(e.loaded);
  x.onload = () => {
    if (x.status >= 200 && x.status < 300) { resolve(); return; }
    let code = 'error', message = x.statusText, field: string | null = null;
    try {
      const j = JSON.parse(x.responseText) as { error?: { code?: string; message?: string; field?: string } };
      code = j.error?.code ?? code; message = j.error?.message ?? message; field = j.error?.field ?? null;
    } catch { /* non-JSON body */ }
    reject(new ApiError(x.status, code, message, null, field));
  };
  x.onerror = () => reject(new ApiError(0, 'network', 'network'));
  x.onabort = () => reject(new DOMException('aborted', 'AbortError'));
  signal.addEventListener('abort', () => x.abort(), { once: true });
  x.send(file);
});

let impl: UploadFn = xhrUpload;
/** The development mock replaces the transport. */
export const setUploadImpl = (fn: UploadFn) => { impl = fn; };
export const uploadFile: UploadFn = (...a) => impl(...a);
