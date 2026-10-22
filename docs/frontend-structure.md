# Frontend structure

[English](frontend-structure.md) · [中文](frontend-structure.zh-CN.md)

The frontend is a Vue 3 single-page app built with Vite. It is intentionally framework-light: Composition API, Pinia, Vue Router, Axios, hand-written CSS, and KaTeX. There is no TypeScript, UI kit, SSR, or test runner.

The frontend only consumes the HTTP API. It has no knowledge of whether catalog items came from `searcher/` or another ingestion pipeline.

## 1. Directory map

```text
frontend/
├── index.html
├── vite.config.js              # @ alias, /api dev proxy, build output
├── package.json                # yarn / npm scripts
├── src/
│   ├── main.js                 # create app, Pinia, router, reveal directive, CSS
│   ├── App.vue                 # layout switch + global modals + toast
│   ├── api/
│   │   ├── client.js           # Axios instance, token storage, error normalization
│   │   ├── users.js            # auth + profile endpoints
│   │   ├── items.js            # GET /items/recommend
│   │   └── interactions.js     # interaction types + payload shaping
│   ├── components/
│   │   ├── SideBar.vue         # brand, nav, account state
│   │   ├── FeedStream.vue      # infinite card feed, bottom trigger, scroll sessions
│   │   ├── ItemCard.vue        # card, original link, click interaction
│   │   ├── ItemDetailPanel.vue # detail, abstract, like
│   │   ├── FeedLoader.vue      # loading / no-new-content / error footer
│   │   ├── OnboardingForm.vue  # profile fields + interests, onboarding/edit modes
│   │   ├── ProfilePanel.vue    # "My" panel: profile, summary, refresh/incremental
│   │   ├── SettingsPanel.vue   # placeholder settings panel
│   │   ├── AuthPanel.vue       # login / register / forgot password
│   │   ├── AppModal.vue        # teleported modal shell
│   │   ├── AppIcon.vue         # inline SVG icon set
│   │   ├── ToastHost.vue       # toast rendering
│   │   ├── SettingSwitch.vue   # toggle row
│   │   └── MathText.vue        # KaTeX/fallback text rendering
│   ├── composables/
│   │   └── useSmoothWheel.js   # rAF wheel easing for the feed scroller
│   ├── directives/
│   │   └── reveal.js           # v-reveal IntersectionObserver animation
│   ├── layouts/
│   │   ├── MainLayout.vue      # sidebar + content
│   │   └── BlankLayout.vue     # full-screen auth/onboarding shell
│   ├── router/
│   │   └── index.js            # routes + auth/onboarding guards
│   ├── stores/
│   │   ├── auth.js             # session, profile, async profile generation polling
│   │   ├── feed.js             # recommendation windows, dedupe, blocked state
│   │   ├── preferences.js      # summary/translation display toggles (localStorage)
│   │   └── ui.js               # panels, detail modal, view timer, toasts
│   ├── styles/
│   │   └── base.css            # colors, spacing, typography, shared buttons
│   ├── utils/
│   │   ├── format.js           # title/abstract/summary selection, dates, source labels
│   │   └── mathText.js         # math token parsing/rendering helpers
│   └── views/
│       ├── HomeView.vue        # feed page shell + scroller + translation toggle
│       ├── AuthView.vue        # /auth
│       └── OnboardingView.vue  # /welcome
└── README.md                   # original frontend notes (Chinese)
```

## 2. Boot and layout

`main.js` creates the app, installs Pinia and the router, registers `v-reveal`, imports KaTeX CSS and `base.css`, and mounts `#app`.

`App.vue` chooses the layout from `route.meta.layout`:

| Route | Layout | Access | Purpose |
| --- | --- | --- | --- |
| `/` | `MainLayout` | Requires login | Streaming feed |
| `/auth` | `BlankLayout` | Guests only | Login / register / forgot password |
| `/welcome` | `BlankLayout` | Requires login | Profile onboarding |
| `/:pathMatch(.*)*` | — | — | Redirect to `/` |

`MainLayout` is the sidebar plus content. `BlankLayout` is a full-screen, no-sidebar shell for auth/onboarding.

`App.vue` also hosts three global `AppModal`s (profile, settings, item detail) and `ToastHost`, so "My", "Settings", and detail are overlays rather than routes.

## 3. Routing and guards

`router.beforeEach`:

1. If logged in and the profile has not loaded, `await auth.loadProfile({ silent: true })`.
2. If the route requires auth and the user is not logged in, redirect to `/auth` with a `redirect` query.
3. If logged in, profile loaded, no profile detail, and the user has not skipped onboarding, redirect to `/welcome`.
4. If a guest visits `/auth` while logged in, send them to `/welcome` or `/`.
5. Otherwise continue.

`needsOnboarding` is computed as:

```text
isLoggedIn && profileLoaded && !hasProfileDetail && !onboardingDismissed
```

`onboardingDismissed` is persisted in localStorage (`dcrec.onboarding_skipped`), not in the user account. Skipping onboarding once suppresses the guard on that browser until the flag is cleared.

## 4. Auth and onboarding flow

```
/auth  ->  login (email + password)
       ->  register: send email code -> submit code -> tokens
       ->  forgot: send email code -> PATCH new password

/welcome -> OnboardingForm
         -> POST /users/info        (save fields; no profile task)
         -> PUT  /users/summary     (enqueue profile generation)
         -> poll GET /users/info every 2.5 s, up to 45 s
         -> show the generated summary
```

The `auth` store owns:

- `accessToken`, `username`, `email`, `profile`, `profileLoaded`
- `register`, `login`, `logout`, `resetPassword`
- `loadProfile`
- `saveProfile(patch, { regenerate: 'client' | 'server' | 'none' })`
- `generateSummary({ mode: 'regenerate' | 'incremental' })`
- `pollSummary` / `awaitSummaryUpdate`

`saveProfile` behavior:

| `regenerate` | API | Profile work |
| --- | --- | --- |
| `client` | `POST /users/info` | Frontend explicitly calls `PUT /users/summary` (first onboarding) |
| `server` | `PUT /users/info` | Backend enqueues `user_summary:new_info` and the client polls |
| `none` | `POST /users/info` | Save only |

Login uses email as the unique credential. The username is a display nickname only; the backend's `GET /users/info` does not return email/username, so the SPA stores both locally for display.

## 5. Feed mechanics

### `stores/feed.js`

The feed is a list of **windows**, not pages:

- `items` — accumulated cards, appended in order.
- `source` — `llm` / `coarse`.
- `outline` — description of the latest LLM window, used as the `scroll` interaction detail.
- `loading`, `error`, `loadedOnce`, `lastRoundAdded`, `requestCount`.
- `blocked` — set only on `401` or network failure (`status === 0`); a normal "no new content" response does **not** block.
- `seen` — a `Set` of item keys for client-side dedupe.

`loadMore()` calls `GET /items/recommend`, filters out already-seen IDs, appends only fresh items, and records how many were added. It never reorders existing cards. There is no permanent "end of feed" state: every scroll to the bottom triggers a real request.

### `components/FeedStream.vue`

- Measures the grid and clamps columns to 1–3; card width is fixed at 320 px.
- Uses an `IntersectionObserver` sentinel plus capture-phase `scroll` and `wheel` listeners.
- A request is triggered when the user reaches within 320 px of the bottom, with an 800 ms cooldown.
- `MAX_CHAIN = 6` prevents an infinite chain when the backend keeps returning new content faster than the viewport fills.
- On a new batch, it only chains another request if `added > 0` and the viewport is still at the bottom.
- `scroll` interactions are recorded only when `source === 'llm'`; the detail is the window `outline`, the duration is seconds, capped at 5 minutes.
- Opening the detail modal or a panel ends the current scroll session.

### Interaction emission

| Event | Where | `detail` | `long_ts` |
| --- | --- | --- | --- |
| `click` (card) | `FeedStream.openDetail` | `card` | 0 |
| `click` (original link) | `ItemCard.handleOriginal` | `link` | 0 |
| `scroll` | `FeedStream.endScrollSession` | Window `outline` | Seconds, cap 300 |
| `like` | `ItemDetailPanel.toggleLike` | Empty | 0 |
| `view` | `ui.finishDetailView` when the detail modal closes | Empty | Seconds, cap 180 |

All interaction requests are fire-and-forget (`.catch(() => {})`) so a failed analytics call never blocks the UI.

## 6. Stores

| Store | Responsibility | Persistence |
| --- | --- | --- |
| `auth` | Session, profile, login/register, profile generation polling | Tokens/username/email/onboarding flag in localStorage |
| `feed` | Recommendation windows, dedupe, loading/blocked state | In-memory; reset on logout |
| `preferences` | `preferSummary`, `translateChinese` | localStorage |
| `ui` | Active panel, detail item, detail view timer, toast | In-memory |

`preferences` defaults: show the enriched summary (`preferSummary=true`) and hide Chinese translation (`translateChinese=false`). Translation only affects non-Chinese items; Chinese items always show their original text.

## 7. Components

| Component | Notes |
| --- | --- |
| `SideBar` | Brand, "My / Home / Settings", account card, login/logout |
| `AppModal` | Teleport to body, backdrop click/Escape close, scroll lock, focus handling |
| `AuthPanel` | Three modes: login, register, forgot; email-code two-step |
| `OnboardingForm` | Profile fields, 10-interest cap, custom interest input, `onboarding`/`edit` modes |
| `ProfilePanel` | Identity card, generated summary, basic info, interests, incremental/regenerate, logout |
| `FeedStream` | Grid, sentinel, bottom trigger, scroll sessions, empty/error states |
| `ItemCard` | Source/date, title, 160-char summary/abstract, tags, original link |
| `ItemDetailPanel` | Full title, authors, tags, "recommendation reason" summary, abstract, like |
| `MathText` | Renders `$...$`/`$$...$$` with KaTeX; falls back to plain text |
| `SettingSwitch` | Accessible toggle row |
| `FeedLoader` | Skeleton/loading, no-new-content, retry |
| `ToastHost` | Transient toast |
| `AppIcon` | Inline SVG icons; no icon package |

## 8. API layer

`api/client.js`:

- `baseURL: '/api'`, timeout 25 s, JSON content type.
- Request interceptor adds `Authorization: Bearer <accessToken>`.
- Response interceptor maps errors to `ApiError` with `status` and `payload`.
- `401` clears the session and calls the registered unauthorized handler (`App.vue` resets auth/feed/ui).
- Error message extraction supports both `{kind,msg}` and `{error}` envelopes and maps Go field names to Chinese labels.

`api/users.js` exposes `sendSignUpCode`, `signUp`, `logIn`, `sendResetCode`, `resetPassword`, `fetchProfile`, `fillProfile`, `updateProfile`, `requestRegenerateSummary`, `requestIncrementalSummary`.

`api/items.js` exposes `fetchRecommendations()`; it intentionally has no cursor parameter because the backend serves one window per request.

`api/interactions.js` defines the four types and caps `view`/`scroll` durations before sending.

## 9. Styling and UX

- `styles/base.css` defines CSS variables for color, radius, shadow, spacing, and shared `.btn` styles.
- No CSS framework; each component uses scoped CSS.
- Responsive breakpoints collapse the card grid and profile grids.
- Motion is CSS-based; `v-reveal` adds a staggered reveal, and `useSmoothWheel` eases wheel scrolling in the feed container.
- Accessibility basics are present (labels, `aria-*`, keyboard activation on cards/modals), but there has been no full audit.

## 10. Adding or changing a page

1. Add a `views/YourView.vue`.
2. Register it in `router/index.js` with `meta.layout` (`main` or `blank`) and `meta.requiresAuth`/`guestOnly` as needed.
3. Add any API function in `src/api/` (do not call Axios directly from a component).
4. Add store state if the data outlives the component.
5. If it is a sub-page rather than a route, add a panel key to `ui` and an `AppModal` in `App.vue` — this is how "My" and "Settings" work.

## 11. Build and deploy

```bash
yarn install
yarn dev       # http://127.0.0.1:5173, proxies /api to :8080
yarn build     # frontend/dist
yarn preview
```

Vite proxies `/api` to `VITE_API_PROXY_TARGET` (or `VITE_API_TARGET`), defaulting to `http://127.0.0.1:8080`. In production, serve `dist` and proxy `/api` to the backend on the same origin; the backend has no CORS. See [Getting started](getting-started.md#5-production-build).

## 12. Known limitations

1. **No TypeScript, unit tests, or E2E tests.** Refactors rely on manual verification.
2. **Client-side dedupe only.** The backend may legitimately return items the client has already seen; the UI hides them, so one window can add fewer than `LIMIT_TOP_K` cards.
3. **No virtualization.** A very long session keeps every card in the DOM.
4. **`like` is local-only.** `ItemDetailPanel` toggles a local `liked` ref and sends the interaction, but the state is not persisted or rehydrated.
5. **Onboarding skip is browser-local.** It is stored in localStorage, not on the user record.
6. **Settings is a placeholder.** `SettingsPanel.vue` has no real preferences beyond the feed's translation toggle.
7. **No SSR or SEO.** The app is a client-rendered SPA; the feed requires auth.
8. **API mappings are manual.** Keep `src/api/*.js` in sync with `backend/api/route/*.go`.
9. **No offline/error boundary.** A top-level render error still blanks the app.
10. **Build tooling is lockfile-sensitive.** `yarn.lock` is Yarn 1.x; npm will generate a different tree.
