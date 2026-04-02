## Problem

The MCP `get_photo` tool doesn't include the original filename in its response. PhotoPrism stores both `FileName` (current path) and `OriginalName` (original upload filename) but the MCP tool doesn't expose them.

## Solution

Already partially implemented in `internal/mcp/photos.go`. The change adds `file_name` and `original_name` fields to the `get_photo` result map.

### What was already done

In `internal/mcp/photos.go`, the result map in `handleGetPhoto` (~line 198) was updated to include:
```go
"file_name":      photo.FileName,
"original_name":  photo.OriginalName,
```

### What remains

1. **Update tool description** (line 40): Change from:
   ```go
   mcp.WithDescription("Get photo metadata (title, description, date, GPS, camera, faces, labels)")
   ```
   To:
   ```go
   mcp.WithDescription("Get photo metadata (title, description, filename, date, GPS, camera, faces, labels)")
   ```

2. **Build and verify**: `make lint && make test`

3. **Update docs**: Update `CLAUDE.md` MCP section if it lists tool descriptions.

## Files to Modify

- `internal/mcp/photos.go` — tool description (line 40), result map already updated