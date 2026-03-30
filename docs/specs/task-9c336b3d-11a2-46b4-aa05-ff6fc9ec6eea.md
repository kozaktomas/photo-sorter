# AI Text Review and Rewrite in PhotoDescriptionDialog

Add two AI-powered text tools to the PhotoDescriptionDialog modal. Both operate on the description textarea content. Uses GPT-4.1-mini (already configured in the project via `OPENAI_TOKEN`).

## Requirements

### Button 1: "Kontrola textu" (Text Check)

A button next to or below the description textarea. On click:

1. Sends the current description text to GPT-4.1-mini
2. Shows results in a panel/card below the textarea:
   - **Readability score** — percentage (0-100%) indicating how clear and readable the text is
   - **Corrected text** — version with fixed spelling, diacritics, and grammar (Czech language)
   - **List of changes** — brief summary of what was corrected (e.g. "opravena diakritika: 'zivot' → 'život'")
3. An "Přijmout" (Accept) button that replaces the textarea content with the corrected version
4. If no corrections needed, show a success message (e.g. "Text je v pořádku")

### Button 2: "Upravit délku" (Adjust Length)

A button next to the first one. Before calling AI, the user selects the desired adjustment:

1. A control to set how much to lengthen or shorten — e.g. a slider or select with options like: "Zkrátit hodně", "Zkrátit mírně", "Prodloužit mírně", "Prodloužit hodně" (or a percentage-based slider, e.g. 50% to 200% of current length)
2. On click, sends the current description text + desired length adjustment to GPT-4.1-mini
3. Shows the rewritten text in a panel below the textarea
4. An "Přijmout" (Accept) button that replaces the textarea content with the new version

### Backend API

Add a new endpoint (or two) for the AI text operations:

- `POST /api/v1/text/check` — body: `{ text: string }` → response: `{ corrected_text: string, readability_score: number, changes: string[] }`
- `POST /api/v1/text/rewrite` — body: `{ text: string, target_length: string }` → response: `{ rewritten_text: string }`

The `target_length` parameter accepts values like `"much_shorter"`, `"shorter"`, `"longer"`, `"much_longer"`.

### AI Prompts

Create prompts in `internal/ai/prompts/`:

- `text_check.txt` — Instruct the model to: check Czech text for spelling, diacritics, and grammar errors; return corrected version; rate readability 0-100%; list specific changes made
- `text_rewrite.txt` — Instruct the model to: rewrite Czech text to the requested length while preserving meaning and style; keep the same tone and factual content

Both prompts should specify that the text is a photo book description in Czech.

### Implementation Notes

- Use the existing OpenAI client from `internal/ai/` — specifically the chat completion API with `gpt-4.1-mini`
- Both buttons should show a loading spinner while waiting for the API response
- No automatic calls — everything is triggered only by user click (to save tokens)
- The AI result panels should be dismissible (close/hide without accepting)
- If the description textarea is empty, disable both buttons
- All UI labels in Czech (use i18n keys in `cs` and `en` locale files)

### Handler Structure

Add a new handler file `internal/web/handlers/text.go` with a `TextHandler` struct that holds the OpenAI client. Register routes in the web server setup.

## Files to create/modify

- `internal/ai/prompts/text_check.txt` — new prompt
- `internal/ai/prompts/text_rewrite.txt` — new prompt
- `internal/web/handlers/text.go` — new handler for text AI endpoints
- `internal/web/server.go` (or wherever routes are registered) — register new routes
- `web/src/pages/BookEditor/PhotoDescriptionDialog.tsx` — add buttons and result panels
- `web/src/api/client.ts` — add API client methods for text check/rewrite
- `web/src/i18n/locales/cs/pages.json` — Czech labels
- `web/src/i18n/locales/en/pages.json` — English labels