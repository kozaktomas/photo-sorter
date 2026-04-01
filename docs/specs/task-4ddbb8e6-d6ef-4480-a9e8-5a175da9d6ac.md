## Summary

When all (or some) photo slots on a book page share the same caption/description, merge them into a single caption entry with combined slot numbers instead of repeating the same text for each slot.

## Current Behavior

In the PDF footer captions, each photo slot gets its own caption line:
```
1: Rodinná fotografie u domu
2: Rodinná fotografie u domu
3: Rodinná fotografie u domu
```

## Desired Behavior

When captions are identical, merge them into one entry with combined numbers:
```
1–3: Rodinná fotografie u domu
```

For partially matching captions, group the identical ones:
```
1, 3: Rodinná fotografie u domu
2: Děti na zahradě
```

Use en-dash (`–`) for consecutive ranges (e.g. `1–3`) and commas for non-consecutive (e.g. `1, 3`). If the page has only one photo slot, no number prefix is needed at all (current behavior should already handle this).

## Implementation

The caption merging logic belongs in `internal/latex/latex.go`, in the caption-building phase of `buildContentPage` / `buildSlots`. The `footerCaptions` slice (of `slotCaption` structs) is assembled per-slot — add a post-processing step that groups captions by text, merges slot indices, and formats the combined prefix.

### Steps

1. After `footerCaptions` is built, group entries by caption text (preserve order of first occurrence)
2. For each group, format the slot numbers:
   - Single slot: just the number (e.g. `1`)
   - Consecutive range: use en-dash (e.g. `1–3`)
   - Non-consecutive: use commas (e.g. `1, 3`)
   - Mixed: combine ranges and singles (e.g. `1–3, 5`)
3. Replace the original `footerCaptions` slice with the merged version
4. If only one unique caption exists and it covers all slots, consider whether the number prefix is still useful (keep it for consistency)

### Affected Code

- `internal/latex/latex.go` — caption merging logic after `buildSlots`
- `internal/latex/latex_test.go` — add tests for caption merging (all same, partially same, all different, consecutive vs non-consecutive ranges)

### Edge Cases

- All captions different → no change
- Some empty captions → don't merge empty with non-empty
- Single photo page → no change needed
- Text-only slots (no caption) → skip them in grouping

Run `make check` to verify.