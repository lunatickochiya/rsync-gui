# Translations

- `rsync-gui_zh_CN.ts` — Simplified Chinese translations (editable source).
- `rsync-gui_zh_CN.qm` — compiled catalog embedded into the executable as
  `:/i18n/rsync-gui_zh_CN.qm` (committed so a normal build does not require Qt Linguist).

Regenerate after changing the `.ts`:

```bash
lupdate -recursive ../src -ts rsync-gui_zh_CN.ts   # optional, after source string changes
lrelease rsync-gui_zh_CN.ts -qm rsync-gui_zh_CN.qm
cmake --build ../build
```

On Debian/Ubuntu, `lupdate`/`lrelease` are in `qt6-l10n-tools`.
The application loads this catalog unconditionally (Simplified Chinese edition).
Qt's own standard dialog strings are loaded from `qt_zh_CN.qm` when
`qt6-translations-l10n` is installed.
