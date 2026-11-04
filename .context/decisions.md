# Decisions (date · decision · why)
2026-11-04 · Record existing architecture, not new product decisions · Initialization indexes current contracts without changing application behavior.
2026-11-04 · Keep daemon and user/admin bridges separate · System operations belong behind the session and privilege boundary; see docs/ARCHITECTURE.md and map/server.md.
2026-11-04 · Preserve approved screens and shared UI/theme primitives · docs/DESIGN-RULES.md governs visual work rather than independent section styling.
2026-11-04 · Route visible frontend text through i18n · English is default and Italian is selectable; section changes must preserve localization.
2026-11-04 · Treat plugins as separate repositories and local plugins/ as development input · The current architecture describes signed marketplace distribution; the older Docker plan is historical.
2026-11-04 · Keep shared context in git and personal checkpoints ignored; keeper remains off · Maps and decisions are durable project knowledge, while now.md is session state.
