# TUI Wireframe Fixtures

Canonical corpus for cockpit wireframe contracts.

## Layout

Families are grouped by folder. Filenames keep their prefix for fast lookup.

- `first-run/`: `F-*`
- `responsive/`: `R-*`
- `traffic/`: `T-*`
- `clients/`: `C-*`
- `routing-credentials/`: `CR-*`
- `routing-validation/`: `RV-*`
- `compatibility/`: `I-*`

Prefix is convenience. Folder is ownership.

## Contract Notes

- Traffic is metadata-first evidence.
- Collapsed traffic shows pulse summary.
- Expanded traffic shows bounded newest-first rows in `time | op | route | timing | result`.
- Request ID appears in opened detail, not top-line rows.
- `429` stays `429`.
- `ERR` is Swobu-originated structured error only.
- Body scroll and local list scroll are distinct grammars.

## Matching Policy

- Literal text by default.
- Use `.` only for intentionally volatile single-cell values.
- Escape literal dots as `\.`.
- Keep wildcard spans narrow.
