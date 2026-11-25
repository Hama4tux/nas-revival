# NAS Revival frontend

Static web UI for the NAS Revival administration API (`../docs/api/v1.md`). The Go backend serves the built files from `dist/` together with `/api/v1`. UI language is Czech (all texts in `src/i18n/cs.ts`).

## Stack

React 19 + TypeScript (strict) + Vite + Tailwind CSS 3 + shadcn/ui-style components (Radix Slot, class-variance-authority, in `src/components/ui`) + lucide-react icons. System font stack, no CDNs, no analytics, no chart library (inline-SVG sparklines). All dependency versions are pinned exactly; use `npm ci`.

## Scripts

- `npm run dev` – Vite dev server; `/api` is proxied to `https://localhost:8443` (`secure:false`, dev proxy only).
- `npm run dev:mock` – dev server with a fake in-browser API (see below); no backend needed.
- `npm run build` – typecheck + production build into `dist/`.
- `npm run typecheck` – `tsc --noEmit`.
- `npm test` – Vitest unit tests (formatting helpers, stale detection).

## Mock mode

`dev:mock` runs Vite with `--mode mock`, which loads `.env.mock` (`VITE_MOCK=1`). `src/main.tsx` then dynamically imports `src/mock/`, which replaces `window.fetch` for `/api/v1/*`. Log in with any username and password `demo`; five wrong attempts trigger the 429 / Retry-After path. The import is guarded by `import.meta.env.VITE_MOCK`, so the mock code is not part of the production bundle.

## Behaviour notes

- Non-GET requests send `X-Requested-With: nas-revival` and `credentials: same-origin`; any 401 returns to the login screen.
- `/overview` is polled every 5 s only while the tab is visible; data older than 15 s is flagged as stale. `/smart` is refreshed every 60 s.
- Theme follows `prefers-color-scheme`, with a toggle remembered in `localStorage` (failures ignored).
