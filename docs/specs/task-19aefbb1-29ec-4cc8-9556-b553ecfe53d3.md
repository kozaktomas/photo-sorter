## Cíl

Rozšířit MCP server (`internal/mcp/`) o nástroje pro správu štítků (labels). Přidat nástroje podle vzoru existujících book nástrojů.

## Nástroje k implementaci

- `list_labels` — seznam štítků (s filtrováním, řazením)
- `get_label` — detail štítku podle UID
- `update_label` — přejmenovat štítek
- `delete_labels` — smazat štítky
- `add_photo_label` — přidat štítek k fotce
- `remove_photo_label` — odebrat štítek z fotky

## Implementace

1. Vytvořit `internal/mcp/labels.go` podle vzoru `internal/mcp/books.go`
2. Použít existující metody z `internal/photoprism/labels.go`
3. Registrovat nástroje v `internal/mcp/server.go`

## Kontext

- MCP server: `internal/mcp/server.go`, `internal/mcp/books.go`
- PhotoPrism label API: `internal/photoprism/labels.go`
- Web handlery: `internal/web/handlers/labels.go`

## Ověření

1. `make lint` — Go checks
2. `make test` — testy
3. Manuální test: spustit MCP server, ověřit funkčnost nástrojů