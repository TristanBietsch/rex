# Prompt: add / refresh `feature-settings.md`

Copy everything below the line into a new agent chat (or paste as your message) when you want the settings feature doc created or brought in sync with the current mockup.

---

You are working in the Rex repo.

**Task:** Create or update **`docs/feature-settings.md`** so it is the canonical markdown spec for the TUI **settings** feature (entry points, appearance, audio, behavior, onboarding, advanced, persistence notes).

**Source of truth for UI copy and catalogs:** `docs/mockup.html`

- **Screen 8** — spinner gallery: five spinner types after `braille` (`ascii line`, `moon`, `pulse`, `blocks`). **Do not** include an “arrows” spinner; it was removed. Renumber so **blocks** is `#5`.
- **Screen 13** — settings rows: color scheme catalog `default · noir · paper`; spinner catalog `braille · ascii line · moon · pulse · blocks`; row density `compact · normal · roomy`; prompt glyph default `λ` with presets `> · % · ❯ · ▸ · ∴` and freeform single grapheme; plus reduce motion, blinking, help bar, audio, behavior, onboarding, advanced as shown.
- **Screen 14** — models sub-page: summarize briefly under Onboarding.

**Content rules:**

1. Use clear Markdown headings and tables where helpful.
2. Explicitly state that **`»` and `➜` are not** prompt presets (replaced by **`%`** and **`▸`**).
3. Explicitly state that the **arrows** spinner is **out of scope** / removed from the catalog.
4. Cross-link to `docs/mockup.html` for pixel-level UI reference.
5. Do not duplicate unrelated screens from the mockup; stay focused on settings and spinners that appear in settings.
6. If `docs/feature-settings.md` already exists, **merge**: keep any extra implementation notes that are still true; otherwise replace sections so they match the mockup.

**Deliverable:** `docs/feature-settings.md` committed-ready prose (no TODO placeholders for the catalogs above—those are decided).
