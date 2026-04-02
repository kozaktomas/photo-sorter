# Photo Filename Overlay in Book Editor

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show original filename/title on photo thumbnails throughout the Book Editor (pages, sections, unassigned pool, photo browser).

**Architecture:** Backend enriches section photo and page slot API responses with `title` and `file_name` from PhotoPrism (batch-fetched using existing `uid:xxx|uid:yyy` query pattern). Frontend components display filename as an overlay via `PhotoInfoOverlay` and as tooltips.

**Tech Stack:** Go (backend handlers), React + TypeScript + TailwindCSS (frontend), PhotoPrism REST API

---

## File Map

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/web/handlers/books.go` | Add `fetchPhotoNames` helper; enrich `sectionPhotoResponse` and `slotResponse` with title/file_name; update `GetSectionPhotos` and `GetBook` handlers |
| Modify | `web/src/types/index.ts` | Add optional `title`/`file_name` to `SectionPhoto` and `PageSlot` |
| Modify | `web/src/pages/BookEditor/PhotoInfoOverlay.tsx` | Add `fileName` prop, render as top overlay bar |
| Modify | `web/src/pages/BookEditor/SectionPhotoPool.tsx` | Pass `file_name` to `PhotoInfoOverlay` |
| Modify | `web/src/pages/BookEditor/UnassignedPool.tsx` | Pass `file_name` to `PhotoInfoOverlay` |
| Modify | `web/src/pages/BookEditor/PageSlot.tsx` | Show filename from slot data or `getPhoto` call |
| Modify | `web/src/pages/BookEditor/PageTemplate.tsx` | Pass `file_name` through to `PageSlotComponent` |
| Modify | `web/src/pages/BookEditor/PhotoBrowserModal.tsx` | Show filename overlay on thumbnails |

---

### Task 1: Backend — Add `fetchPhotoNames` helper and enrich response types

**Files:**
- Modify: `internal/web/handlers/books.go:84-109` (response types) and add new helper function

The existing `fetchPhotoDimensions` function (line 1612) already uses the batch UID pattern `"uid:" + strings.Join(batch, "|uid:")`. We create a similar helper that returns title + filename.

- [ ] **Step 1: Add `title` and `file_name` fields to `sectionPhotoResponse` and `slotResponse`**

In `internal/web/handlers/books.go`, update the two response structs:

```go
type sectionPhotoResponse struct {
	PhotoUID    string `json:"photo_uid"`
	Description string `json:"description"`
	Note        string `json:"note"`
	Title       string `json:"title"`
	FileName    string `json:"file_name"`
	AddedAt     string `json:"added_at"`
}

type slotResponse struct {
	SlotIndex   int     `json:"slot_index"`
	PhotoUID    string  `json:"photo_uid"`
	TextContent string  `json:"text_content"`
	CropX       float64 `json:"crop_x"`
	CropY       float64 `json:"crop_y"`
	CropScale   float64 `json:"crop_scale"`
	Title       string  `json:"title"`
	FileName    string  `json:"file_name"`
}
```

- [ ] **Step 2: Add `fetchPhotoNames` helper function**

Add after `fetchPhotoDimensions` (around line 1629):

```go
// photoNameInfo holds title and filename for a photo.
type photoNameInfo struct {
	Title    string
	FileName string
}

// fetchPhotoNames batch-fetches photo title and filename from PhotoPrism.
func fetchPhotoNames(pp *photoprism.PhotoPrism, uids []string) map[string]photoNameInfo {
	names := make(map[string]photoNameInfo, len(uids))
	const batchSize = 100
	for i := 0; i < len(uids); i += batchSize {
		end := min(i+batchSize, len(uids))
		batch := uids[i:end]
		query := "uid:" + strings.Join(batch, "|uid:")
		photos, err := pp.GetPhotosWithQuery(len(batch), 0, query, 0)
		if err != nil {
			log.Printf("fetchPhotoNames: failed to fetch photos: %v", err)
			continue
		}
		for _, p := range photos {
			names[p.UID] = photoNameInfo{Title: p.Title, FileName: p.FileName}
		}
	}
	return names
}
```

- [ ] **Step 3: Enrich `GetSectionPhotos` handler**

Update the `GetSectionPhotos` handler (line 557) to fetch photo names and merge them:

```go
func (h *BooksHandler) GetSectionPhotos(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	sectionID := chi.URLParam(r, "id")
	photos, err := bw.GetSectionPhotos(r.Context(), sectionID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get section photos")
		return
	}

	// Fetch photo names from PhotoPrism
	var photoNames map[string]photoNameInfo
	if len(photos) > 0 {
		pp := middleware.MustGetPhotoPrism(r.Context(), w)
		if pp == nil {
			return
		}
		uids := make([]string, len(photos))
		for i, p := range photos {
			uids[i] = p.PhotoUID
		}
		photoNames = fetchPhotoNames(pp, uids)
	}

	result := make([]sectionPhotoResponse, len(photos))
	for i, p := range photos {
		resp := sectionPhotoResponse{
			PhotoUID:    p.PhotoUID,
			Description: p.Description,
			Note:        p.Note,
			AddedAt:     p.AddedAt.Format("2006-01-02T15:04:05Z"),
		}
		if info, ok := photoNames[p.PhotoUID]; ok {
			resp.Title = info.Title
			resp.FileName = info.FileName
		}
		result[i] = resp
	}
	respondJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 4: Enrich `GetBook` handler's slot responses**

Update the `GetBook` handler (line 208) to enrich slots. Add photo name fetching between building the response and returning it:

```go
func (h *BooksHandler) GetBook(w http.ResponseWriter, r *http.Request) {
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}
	id := chi.URLParam(r, "id")
	book, err := bw.GetBook(r.Context(), id)
	if err != nil || book == nil {
		respondError(w, http.StatusNotFound, "book not found")
		return
	}

	chapters, err2 := bw.GetChapters(r.Context(), id)
	if err2 != nil {
		respondError(w, http.StatusInternalServerError, "failed to get chapters")
		return
	}
	sections, err2 := bw.GetSections(r.Context(), id)
	if err2 != nil {
		respondError(w, http.StatusInternalServerError, "failed to get sections")
		return
	}
	pages, err2 := bw.GetPages(r.Context(), id)
	if err2 != nil {
		respondError(w, http.StatusInternalServerError, "failed to get pages")
		return
	}

	resp := buildBookDetailResponse(book, chapters, sections, pages)

	// Enrich slot responses with photo names
	allPhotoUIDs := collectSlotPhotoUIDs(pages)
	if len(allPhotoUIDs) > 0 {
		pp := middleware.MustGetPhotoPrism(r.Context(), w)
		if pp == nil {
			return
		}
		photoNames := fetchPhotoNames(pp, allPhotoUIDs)
		for i := range resp.Pages {
			for j := range resp.Pages[i].Slots {
				uid := resp.Pages[i].Slots[j].PhotoUID
				if info, ok := photoNames[uid]; ok {
					resp.Pages[i].Slots[j].Title = info.Title
					resp.Pages[i].Slots[j].FileName = info.FileName
				}
			}
		}
	}

	respondJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 5: Verify the build compiles**

Run: `cd /home/pi/projects/photo-sorter && make build-go`
Expected: Build succeeds.

- [ ] **Step 6: Commit backend changes**

```bash
git add internal/web/handlers/books.go
git commit -m "feat: enrich book slot and section photo responses with title/filename"
```

---

### Task 2: Frontend — Update types

**Files:**
- Modify: `web/src/types/index.ts:434-461`

- [ ] **Step 1: Add `title` and `file_name` to `SectionPhoto`**

In `web/src/types/index.ts`, update the `SectionPhoto` interface (line 434):

```typescript
export interface SectionPhoto {
  photo_uid: string;
  description: string;
  note: string;
  title: string;
  file_name: string;
  added_at: string;
}
```

- [ ] **Step 2: Add `title` and `file_name` to `PageSlot`**

In the same file, update the `PageSlot` interface (line 454):

```typescript
export interface PageSlot {
  slot_index: number;
  photo_uid: string;
  text_content: string;
  crop_x: number;
  crop_y: number;
  crop_scale: number;
  title: string;
  file_name: string;
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /home/pi/projects/photo-sorter/web && npx tsc --noEmit`
Expected: No errors (new fields are optional in JSON and default to empty string).

- [ ] **Step 4: Commit**

```bash
git add web/src/types/index.ts
git commit -m "feat: add title and file_name to SectionPhoto and PageSlot types"
```

---

### Task 3: Frontend — Add filename display to PhotoInfoOverlay

**Files:**
- Modify: `web/src/pages/BookEditor/PhotoInfoOverlay.tsx`

The `PhotoInfoOverlay` component currently shows `description`, `note`, and `orientation` as overlays at the bottom of photo thumbnails. We add a `fileName` prop that renders at the very top of the overlay stack (before note and description).

- [ ] **Step 1: Add `fileName` prop and render it**

Replace the full content of `web/src/pages/BookEditor/PhotoInfoOverlay.tsx`:

```typescript
import { useTranslation } from 'react-i18next';
import { StickyNote } from 'lucide-react';

interface Props {
  description?: string;
  note?: string;
  fileName?: string;
  orientation?: 'L' | 'P' | null;
  compact?: boolean;
}

export function PhotoInfoOverlay({ description, note, fileName, orientation, compact }: Props) {
  const { t } = useTranslation('pages');
  const hasContent = description || note || orientation || fileName;
  if (!hasContent) return null;

  return (
    <div className="absolute inset-0 pointer-events-none flex flex-col justify-end">
      {orientation && (
        <span className={`absolute bottom-0.5 right-0.5 text-[9px] font-bold leading-none px-1 py-0.5 rounded z-10 ${
          orientation === 'L' ? 'bg-blue-600/80 text-blue-100' : 'bg-amber-600/80 text-amber-100'
        }`} style={note || description || fileName ? { bottom: `${(note ? 1.25 : 0) + (description ? (compact ? 1.25 : 2.25) : 0) + (fileName ? 1.25 : 0)}rem` } : undefined}>
          {orientation === 'L' ? t('books.editor.orientationLandscape') : t('books.editor.orientationPortrait')}
        </span>
      )}
      {fileName && (
        <div className="bg-slate-900/70 text-slate-300 text-[10px] px-1.5 py-0.5 truncate" title={fileName}>
          {fileName}
        </div>
      )}
      {note && (
        <div className="bg-amber-900/60 text-amber-200 text-[10px] px-1.5 py-0.5 flex items-center gap-1 line-clamp-1">
          <StickyNote className="h-2.5 w-2.5 flex-shrink-0" />
          <span className="truncate">{note}</span>
        </div>
      )}
      {description && (
        <div className={`bg-black/60 text-white text-xs px-1.5 py-0.5 ${compact ? 'line-clamp-1' : 'line-clamp-2'}`}>
          {description}
        </div>
      )}
    </div>
  );
}
```

Key changes:
- Added `fileName` prop
- Added `fileName` to the `hasContent` check
- Rendered `fileName` as a `bg-slate-900/70` overlay bar above the note bar, with `truncate` for ellipsis and `title` attribute for full-text tooltip
- Updated orientation badge bottom offset calculation to account for fileName row

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /home/pi/projects/photo-sorter/web && npx tsc --noEmit`
Expected: No errors (all existing callers pass optional props, new prop defaults to undefined).

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/BookEditor/PhotoInfoOverlay.tsx
git commit -m "feat: add fileName display to PhotoInfoOverlay"
```

---

### Task 4: Frontend — Wire filename into SectionPhotoPool

**Files:**
- Modify: `web/src/pages/BookEditor/SectionPhotoPool.tsx:54`

The `SectionPhoto` type now includes `file_name`. Pass it to `PhotoInfoOverlay` inside the `DraggablePhoto` component.

- [ ] **Step 1: Pass `fileName` to `PhotoInfoOverlay`**

In `SectionPhotoPool.tsx`, update line 54 where `PhotoInfoOverlay` is rendered inside `DraggablePhoto`:

Change:
```typescript
        <PhotoInfoOverlay description={photo.description} note={photo.note} />
```

To:
```typescript
        <PhotoInfoOverlay description={photo.description} note={photo.note} fileName={photo.file_name} />
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /home/pi/projects/photo-sorter/web && npx tsc --noEmit`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/BookEditor/SectionPhotoPool.tsx
git commit -m "feat: show filename in section photo pool"
```

---

### Task 5: Frontend — Wire filename into UnassignedPool

**Files:**
- Modify: `web/src/pages/BookEditor/UnassignedPool.tsx:15-56`

The `DraggablePhoto` in `UnassignedPool` receives `description` and `note` from the section photo lookup. Add `fileName` the same way.

- [ ] **Step 1: Add `fileName` prop to DraggablePhoto and pass to PhotoInfoOverlay**

In `UnassignedPool.tsx`, update the `DraggablePhoto` component:

Change the function signature (line 15):
```typescript
function DraggablePhoto({ uid, description, note }: {
  uid: string;
  description: string;
  note: string;
}) {
```

To:
```typescript
function DraggablePhoto({ uid, description, note, fileName }: {
  uid: string;
  description: string;
  note: string;
  fileName: string;
}) {
```

Update the `PhotoInfoOverlay` call (line 48-52):

Change:
```typescript
      <PhotoInfoOverlay
        description={description}
        note={note}
        orientation={orientation}
        compact
      />
```

To:
```typescript
      <PhotoInfoOverlay
        description={description}
        note={note}
        fileName={fileName}
        orientation={orientation}
        compact
      />
```

Update the `DraggablePhoto` usage (line 85-89) to pass `fileName`:

Change:
```typescript
            <DraggablePhoto
              key={uid}
              uid={uid}
              description={sp?.description ?? ''}
              note={sp?.note ?? ''}
            />
```

To:
```typescript
            <DraggablePhoto
              key={uid}
              uid={uid}
              description={sp?.description ?? ''}
              note={sp?.note ?? ''}
              fileName={sp?.file_name ?? ''}
            />
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /home/pi/projects/photo-sorter/web && npx tsc --noEmit`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/BookEditor/UnassignedPool.tsx
git commit -m "feat: show filename in unassigned photo pool"
```

---

### Task 6: Frontend — Wire filename into PageSlot and PageTemplate

**Files:**
- Modify: `web/src/pages/BookEditor/PageSlot.tsx:12-34,136-141`
- Modify: `web/src/pages/BookEditor/PageTemplate.tsx:154-176`

PageSlot already calls `getPhoto(photoUid)` for DPI computation. We piggyback on that to extract the filename. PageTemplate passes `file_name` from slot data or section photo lookup.

- [ ] **Step 1: Add `fileName` prop to PageSlotComponent and pass to PhotoInfoOverlay**

In `PageSlot.tsx`, add `fileName` to the Props interface:

Change (line 12-32):
```typescript
interface Props {
  pageId: string;
  slotIndex: number;
  photoUid: string;
  textContent?: string;
  cropX?: number;
  cropY?: number;
  cropScale?: number;
  format?: PageFormat;
  splitPosition?: number | null;
  onClear: () => void;
  onEditCrop?: () => void;
  description?: string;
  note?: string;
  onEditDescription?: () => void;
  onEditText?: () => void;
  onAddText?: () => void;
  chapterColor?: string;
  textPaddingClass?: string;
  className?: string;
}
```

To:
```typescript
interface Props {
  pageId: string;
  slotIndex: number;
  photoUid: string;
  textContent?: string;
  cropX?: number;
  cropY?: number;
  cropScale?: number;
  format?: PageFormat;
  splitPosition?: number | null;
  onClear: () => void;
  onEditCrop?: () => void;
  description?: string;
  note?: string;
  fileName?: string;
  onEditDescription?: () => void;
  onEditText?: () => void;
  onAddText?: () => void;
  chapterColor?: string;
  textPaddingClass?: string;
  className?: string;
}
```

Update the destructured props (line 34) to include `fileName`:

Change:
```typescript
export function PageSlotComponent({ pageId, slotIndex, photoUid, textContent, cropX, cropY, cropScale, format, splitPosition, onClear, onEditCrop, description, note, onEditDescription, onEditText, onAddText, chapterColor, textPaddingClass, className }: Props) {
```

To:
```typescript
export function PageSlotComponent({ pageId, slotIndex, photoUid, textContent, cropX, cropY, cropScale, format, splitPosition, onClear, onEditCrop, description, note, fileName, onEditDescription, onEditText, onAddText, chapterColor, textPaddingClass, className }: Props) {
```

Update the `PhotoInfoOverlay` call (line 136-140):

Change:
```typescript
          <PhotoInfoOverlay
            description={description}
            note={note}
            orientation={orientation}
          />
```

To:
```typescript
          <PhotoInfoOverlay
            description={description}
            note={note}
            fileName={fileName}
            orientation={orientation}
          />
```

- [ ] **Step 2: Pass `fileName` from PageTemplate to PageSlotComponent**

In `PageTemplate.tsx`, update the `PageSlotComponent` rendering (around line 155-176). The slot data now has `file_name` from the API, and section photos also have `file_name`. Use the slot's `file_name` directly from the page data, falling back to section photo lookup:

Change line 153:
```typescript
          const sp = uid ? photoLookup.get(uid) : undefined;
```

To:
```typescript
          const sp = uid ? photoLookup.get(uid) : undefined;
          const slot = page.slots.find(s => s.slot_index === i);
          const slotFileName = slot?.file_name || sp?.file_name || '';
```

Then pass `fileName` to `PageSlotComponent` — add after the `note` prop (around line 169):

Add this line:
```typescript
              fileName={slotFileName}
```

So the full `PageSlotComponent` call becomes:
```typescript
            <PageSlotComponent
              key={i}
              pageId={page.id}
              slotIndex={i}
              photoUid={uid}
              textContent={textContent}
              cropX={cropX}
              cropY={cropY}
              cropScale={cropScale}
              format={page.format}
              splitPosition={page.split_position}
              onClear={() => onClearSlot(i)}
              onEditCrop={uid && onEditCrop ? () => onEditCrop(i) : undefined}
              description={sp?.description ?? ''}
              note={sp?.note ?? ''}
              fileName={slotFileName}
              onEditDescription={uid && onEditDescription ? () => onEditDescription(uid) : undefined}
              onEditText={textContent && onEditText ? () => onEditText(i) : undefined}
              onAddText={!uid && !textContent && onAddText ? () => onAddText(i) : undefined}
              chapterColor={chapterColor}
              textPaddingClass={getTextSlotPaddingClass(page, i)}
              className={getSlotClasses(page.format, i)}
            />
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /home/pi/projects/photo-sorter/web && npx tsc --noEmit`
Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/BookEditor/PageSlot.tsx web/src/pages/BookEditor/PageTemplate.tsx
git commit -m "feat: show filename on book page slots"
```

---

### Task 7: Frontend — Show filename in PhotoBrowserModal

**Files:**
- Modify: `web/src/pages/BookEditor/PhotoBrowserModal.tsx:190-218`

The modal already fetches full `Photo` objects (which include `title` and `file_name`). We just need to display the filename as a tooltip and small overlay.

- [ ] **Step 1: Add filename overlay to photo thumbnails**

In `PhotoBrowserModal.tsx`, update the photo rendering (line 190-218). After the `img` tag (line 207) and inside the same `div`, add a filename overlay and title tooltip:

Change lines 201-217:
```typescript
                  onClick={() => !isExisting && toggleSelect(photo.uid)}
                >
                  <img
                    src={getThumbnailUrl(photo.uid, 'tile_100')}
                    alt=""
                    className="w-full aspect-square object-cover"
                    loading="lazy"
                  />
                  {!isExisting && (
                    <div className="absolute top-1 left-1">
                      {isSelected ? (
                        <CheckSquare className="h-4 w-4 text-rose-400" />
                      ) : (
                        <Square className="h-4 w-4 text-white/40" />
                      )}
                    </div>
                  )}
                </div>
```

To:
```typescript
                  onClick={() => !isExisting && toggleSelect(photo.uid)}
                  title={photo.file_name || photo.title}
                >
                  <img
                    src={getThumbnailUrl(photo.uid, 'tile_100')}
                    alt=""
                    className="w-full aspect-square object-cover"
                    loading="lazy"
                  />
                  {!isExisting && (
                    <div className="absolute top-1 left-1">
                      {isSelected ? (
                        <CheckSquare className="h-4 w-4 text-rose-400" />
                      ) : (
                        <Square className="h-4 w-4 text-white/40" />
                      )}
                    </div>
                  )}
                  <div className="absolute bottom-0 inset-x-0 bg-black/60 text-white text-[9px] px-1 py-0.5 truncate">
                    {photo.file_name || photo.title}
                  </div>
                </div>
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /home/pi/projects/photo-sorter/web && npx tsc --noEmit`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/BookEditor/PhotoBrowserModal.tsx
git commit -m "feat: show filename in photo browser modal"
```

---

### Task 8: Build, verify, and final commit

- [ ] **Step 1: Run full build**

Run: `cd /home/pi/projects/photo-sorter && make build`
Expected: Both frontend and backend build successfully.

- [ ] **Step 2: Run Go linter**

Run: `cd /home/pi/projects/photo-sorter && make lint`
Expected: No lint errors.

- [ ] **Step 3: Run tests**

Run: `cd /home/pi/projects/photo-sorter && make test`
Expected: All tests pass.

- [ ] **Step 4: Rebuild and restart dev server, check visually**

Run: `cd /home/pi/projects/photo-sorter && ./dev.sh`
Then use headless Chromium to spot-check:
```bash
chromium --headless --no-sandbox --screenshot=/tmp/book-editor.png --window-size=1280,800 http://localhost:8085
```

- [ ] **Step 5: Squash into a single feature commit if not already**

If all tasks were committed individually, the history is fine. If the user wants a single commit, squash. Otherwise leave as-is.
