# Disable Auto-Process Checkbox by Default on Upload Page

On the Upload page, the "Automaticky zpracovat (výpočet embeddingů a detekce obličejů)" checkbox is currently enabled by default. Change it to be unchecked by default.

## Requirements

- The auto-process checkbox on the Upload page should be `false` / unchecked by default
- User can still manually enable it before uploading

## Files to modify

- Look for the upload page component (likely `web/src/pages/Upload/` or similar) and change the default state of the auto-process checkbox from `true` to `false`