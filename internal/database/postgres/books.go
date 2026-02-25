package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// BookRepository provides PostgreSQL-backed photo book storage
type BookRepository struct {
	pool *Pool
}

// NewBookRepository creates a new BookRepository
func NewBookRepository(pool *Pool) *BookRepository {
	return &BookRepository{pool: pool}
}

func newID() string {
	return uuid.New().String()
}

// --- Books ---

func (r *BookRepository) CreateBook(ctx context.Context, book *database.PhotoBook) error {
	if book.ID == "" {
		book.ID = newID()
	}
	now := time.Now()
	book.CreatedAt = now
	book.UpdatedAt = now
	_, err := r.pool.Exec(ctx,
		`INSERT INTO photo_books (id, title, description, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		book.ID, book.Title, book.Description, book.CreatedAt, book.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create book: %w", err)
	}
	return nil
}

func (r *BookRepository) GetBook(ctx context.Context, id string) (*database.PhotoBook, error) {
	var b database.PhotoBook
	err := r.pool.QueryRow(ctx,
		`SELECT id, title, description, created_at, updated_at FROM photo_books WHERE id = $1`, id).
		Scan(&b.ID, &b.Title, &b.Description, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get book: %w", err)
	}
	return &b, nil
}

func (r *BookRepository) ListBooks(ctx context.Context) ([]database.PhotoBook, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, title, description, created_at, updated_at FROM photo_books ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	defer rows.Close()
	var books []database.PhotoBook
	for rows.Next() {
		var b database.PhotoBook
		if err := rows.Scan(&b.ID, &b.Title, &b.Description, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan book: %w", err)
		}
		books = append(books, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate books: %w", err)
	}
	return books, nil
}

func (r *BookRepository) ListBooksWithCounts(ctx context.Context) ([]database.PhotoBookWithCounts, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT pb.id, pb.title, pb.description, pb.created_at, pb.updated_at,
			(SELECT COUNT(*) FROM book_sections WHERE book_id = pb.id) as section_count,
			(SELECT COUNT(*) FROM book_pages WHERE book_id = pb.id) as page_count,
			COALESCE((SELECT SUM(cnt) FROM (
				SELECT COUNT(*) as cnt FROM section_photos
				WHERE section_id IN (SELECT id FROM book_sections WHERE book_id = pb.id)
			) t), 0) as photo_count
		 FROM photo_books pb ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list books with counts: %w", err)
	}
	defer rows.Close()
	var books []database.PhotoBookWithCounts
	for rows.Next() {
		var b database.PhotoBookWithCounts
		if err := rows.Scan(&b.ID, &b.Title, &b.Description, &b.CreatedAt, &b.UpdatedAt,
			&b.SectionCount, &b.PageCount, &b.PhotoCount); err != nil {
			return nil, fmt.Errorf("scan book with counts: %w", err)
		}
		books = append(books, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate books with counts: %w", err)
	}
	return books, nil
}

func (r *BookRepository) UpdateBook(ctx context.Context, book *database.PhotoBook) error {
	book.UpdatedAt = time.Now()
	_, err := r.pool.Exec(ctx,
		`UPDATE photo_books SET title = $1, description = $2, updated_at = $3 WHERE id = $4`,
		book.Title, book.Description, book.UpdatedAt, book.ID)
	if err != nil {
		return fmt.Errorf("update book: %w", err)
	}
	return nil
}

func (r *BookRepository) DeleteBook(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM photo_books WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete book: %w", err)
	}
	return nil
}

// --- Chapters ---

func (r *BookRepository) GetChapters(ctx context.Context, bookID string) ([]database.BookChapter, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, book_id, title, sort_order, created_at, updated_at
		 FROM book_chapters WHERE book_id = $1 ORDER BY sort_order`, bookID)
	if err != nil {
		return nil, fmt.Errorf("get chapters: %w", err)
	}
	defer rows.Close()
	var chapters []database.BookChapter
	for rows.Next() {
		var c database.BookChapter
		if err := rows.Scan(&c.ID, &c.BookID, &c.Title, &c.SortOrder, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan chapter: %w", err)
		}
		chapters = append(chapters, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chapters: %w", err)
	}
	return chapters, nil
}

func (r *BookRepository) CreateChapter(ctx context.Context, chapter *database.BookChapter) error {
	if chapter.ID == "" {
		chapter.ID = newID()
	}
	now := time.Now()
	chapter.CreatedAt = now
	chapter.UpdatedAt = now

	// Auto-assign sort_order as max+1
	var maxOrder sql.NullInt64
	if err := r.pool.QueryRow(ctx,
		`SELECT MAX(sort_order) FROM book_chapters WHERE book_id = $1`, chapter.BookID).
		Scan(&maxOrder); err != nil {
		return fmt.Errorf("get max chapter sort order: %w", err)
	}
	if maxOrder.Valid {
		chapter.SortOrder = int(maxOrder.Int64) + 1
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO book_chapters (id, book_id, title, sort_order, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		chapter.ID, chapter.BookID, chapter.Title, chapter.SortOrder, chapter.CreatedAt, chapter.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create chapter: %w", err)
	}
	return nil
}

func (r *BookRepository) UpdateChapter(ctx context.Context, chapter *database.BookChapter) error {
	chapter.UpdatedAt = time.Now()
	_, err := r.pool.Exec(ctx,
		`UPDATE book_chapters SET title = $1, updated_at = $2 WHERE id = $3`,
		chapter.Title, chapter.UpdatedAt, chapter.ID)
	if err != nil {
		return fmt.Errorf("update chapter: %w", err)
	}
	return nil
}

func (r *BookRepository) DeleteChapter(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM book_chapters WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete chapter: %w", err)
	}
	return nil
}

func (r *BookRepository) ReorderChapters(ctx context.Context, bookID string, chapterIDs []string) error {
	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for i, id := range chapterIDs {
		_, err := tx.ExecContext(ctx,
			`UPDATE book_chapters SET sort_order = $1, updated_at = NOW() WHERE id = $2 AND book_id = $3`,
			i, id, bookID)
		if err != nil {
			return fmt.Errorf("reorder chapter %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reorder chapters: %w", err)
	}
	return nil
}

// --- Sections ---

func (r *BookRepository) CreateSection(ctx context.Context, section *database.BookSection) error {
	if section.ID == "" {
		section.ID = newID()
	}
	now := time.Now()
	section.CreatedAt = now
	section.UpdatedAt = now

	// Auto-assign sort_order as max+1
	var maxOrder sql.NullInt64
	if err := r.pool.QueryRow(ctx,
		`SELECT MAX(sort_order) FROM book_sections WHERE book_id = $1`, section.BookID).
		Scan(&maxOrder); err != nil {
		return fmt.Errorf("get max sort order: %w", err)
	}
	if maxOrder.Valid {
		section.SortOrder = int(maxOrder.Int64) + 1
	}

	var chapterID *string
	if section.ChapterID != "" {
		chapterID = &section.ChapterID
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO book_sections (id, book_id, chapter_id, title, sort_order, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		section.ID, section.BookID, chapterID, section.Title, section.SortOrder, section.CreatedAt, section.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create section: %w", err)
	}
	return nil
}

func (r *BookRepository) GetSection(ctx context.Context, id string) (*database.BookSection, error) {
	var s database.BookSection
	err := r.pool.QueryRow(ctx,
		`SELECT id, book_id, COALESCE(chapter_id, ''), title, sort_order, created_at, updated_at
		 FROM book_sections WHERE id = $1`, id).
		Scan(&s.ID, &s.BookID, &s.ChapterID, &s.Title, &s.SortOrder, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get section: %w", err)
	}
	return &s, nil
}

func (r *BookRepository) GetSections(ctx context.Context, bookID string) ([]database.BookSection, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT s.id, s.book_id, COALESCE(s.chapter_id, ''), s.title, s.sort_order, s.created_at, s.updated_at,
		        COALESCE((SELECT COUNT(*) FROM section_photos WHERE section_id = s.id), 0) as photo_count
		 FROM book_sections s
		 WHERE s.book_id = $1
		 ORDER BY s.sort_order`, bookID)
	if err != nil {
		return nil, fmt.Errorf("get sections: %w", err)
	}
	defer rows.Close()
	var sections []database.BookSection
	for rows.Next() {
		var s database.BookSection
		if err := rows.Scan(&s.ID, &s.BookID, &s.ChapterID, &s.Title, &s.SortOrder, &s.CreatedAt, &s.UpdatedAt, &s.PhotoCount); err != nil {
			return nil, fmt.Errorf("scan section: %w", err)
		}
		sections = append(sections, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sections: %w", err)
	}
	return sections, nil
}

func (r *BookRepository) UpdateSection(ctx context.Context, section *database.BookSection) error {
	section.UpdatedAt = time.Now()
	var chapterID *string
	if section.ChapterID != "" {
		chapterID = &section.ChapterID
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE book_sections SET title = $1, chapter_id = $2, updated_at = $3 WHERE id = $4`,
		section.Title, chapterID, section.UpdatedAt, section.ID)
	if err != nil {
		return fmt.Errorf("update section: %w", err)
	}
	return nil
}

func (r *BookRepository) DeleteSection(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM book_sections WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete section: %w", err)
	}
	return nil
}

func (r *BookRepository) ReorderSections(ctx context.Context, bookID string, sectionIDs []string) error {
	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for i, id := range sectionIDs {
		_, err := tx.ExecContext(ctx,
			`UPDATE book_sections SET sort_order = $1, updated_at = NOW() WHERE id = $2 AND book_id = $3`,
			i, id, bookID)
		if err != nil {
			return fmt.Errorf("reorder section %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reorder sections: %w", err)
	}
	return nil
}

// --- Section Photos ---

func (r *BookRepository) GetSectionPhotos(ctx context.Context, sectionID string) ([]database.SectionPhoto, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, section_id, photo_uid, description, note, added_at
		 FROM section_photos WHERE section_id = $1 ORDER BY added_at`, sectionID)
	if err != nil {
		return nil, fmt.Errorf("get section photos: %w", err)
	}
	defer rows.Close()
	var photos []database.SectionPhoto
	for rows.Next() {
		var p database.SectionPhoto
		if err := rows.Scan(&p.ID, &p.SectionID, &p.PhotoUID, &p.Description, &p.Note, &p.AddedAt); err != nil {
			return nil, fmt.Errorf("scan section photo: %w", err)
		}
		photos = append(photos, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate section photos: %w", err)
	}
	return photos, nil
}

func (r *BookRepository) CountSectionPhotos(ctx context.Context, sectionID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM section_photos WHERE section_id = $1`, sectionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count section photos: %w", err)
	}
	return count, nil
}

func (r *BookRepository) AddSectionPhotos(ctx context.Context, sectionID string, photoUIDs []string) error {
	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, uid := range photoUIDs {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO section_photos (section_id, photo_uid) VALUES ($1, $2) ON CONFLICT (section_id, photo_uid) DO NOTHING`,
			sectionID, uid)
		if err != nil {
			return fmt.Errorf("add photo %s: %w", uid, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit add section photos: %w", err)
	}
	return nil
}

func (r *BookRepository) RemoveSectionPhotos(ctx context.Context, sectionID string, photoUIDs []string) error {
	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, uid := range photoUIDs {
		_, err := tx.ExecContext(ctx,
			`DELETE FROM section_photos WHERE section_id = $1 AND photo_uid = $2`,
			sectionID, uid)
		if err != nil {
			return fmt.Errorf("remove photo %s: %w", uid, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit remove section photos: %w", err)
	}
	return nil
}

func (r *BookRepository) UpdateSectionPhoto(ctx context.Context, sectionID string, photoUID string, description string, note string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE section_photos SET description = $1, note = $2 WHERE section_id = $3 AND photo_uid = $4`,
		description, note, sectionID, photoUID)
	if err != nil {
		return fmt.Errorf("update section photo: %w", err)
	}
	return nil
}

// --- Pages ---

func (r *BookRepository) CreatePage(ctx context.Context, page *database.BookPage) error {
	if page.ID == "" {
		page.ID = newID()
	}
	now := time.Now()
	page.CreatedAt = now
	page.UpdatedAt = now

	// Auto-assign sort_order as max+1
	var maxOrder sql.NullInt64
	if err := r.pool.QueryRow(ctx,
		`SELECT MAX(sort_order) FROM book_pages WHERE book_id = $1`, page.BookID).
		Scan(&maxOrder); err != nil {
		return fmt.Errorf("get max sort order: %w", err)
	}
	if maxOrder.Valid {
		page.SortOrder = int(maxOrder.Int64) + 1
	}

	var sectionID *string
	if page.SectionID != "" {
		sectionID = &page.SectionID
	}

	if page.Style == "" {
		page.Style = "modern"
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO book_pages (id, book_id, section_id, format, style, description, sort_order, split_position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		page.ID, page.BookID, sectionID, page.Format, page.Style, page.Description, page.SortOrder, page.SplitPosition, page.CreatedAt, page.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create page: %w", err)
	}
	return nil
}

func (r *BookRepository) GetPage(ctx context.Context, pageID string) (*database.BookPage, error) {
	var p database.BookPage
	err := r.pool.QueryRow(ctx,
		`SELECT id, book_id, COALESCE(section_id, ''), format, style, description, sort_order, split_position, created_at, updated_at
		 FROM book_pages WHERE id = $1`, pageID).
		Scan(&p.ID, &p.BookID, &p.SectionID, &p.Format, &p.Style, &p.Description, &p.SortOrder, &p.SplitPosition, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get page: %w", err)
	}
	slots, err := r.GetPageSlots(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	p.Slots = slots
	return &p, nil
}

func (r *BookRepository) GetPages(ctx context.Context, bookID string) ([]database.BookPage, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, book_id, COALESCE(section_id, ''), format, style, description, sort_order, split_position, created_at, updated_at
		 FROM book_pages WHERE book_id = $1 ORDER BY sort_order`, bookID)
	if err != nil {
		return nil, fmt.Errorf("get pages: %w", err)
	}
	defer rows.Close()
	var pages []database.BookPage
	for rows.Next() {
		var p database.BookPage
		if err := rows.Scan(&p.ID, &p.BookID, &p.SectionID, &p.Format, &p.Style, &p.Description, &p.SortOrder, &p.SplitPosition, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan page: %w", err)
		}
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pages: %w", err)
	}

	// Batch-load all slots for the book in one query
	slotRows, err := r.pool.Query(ctx,
		`SELECT ps.page_id, ps.slot_index, COALESCE(ps.photo_uid, ''), COALESCE(ps.text_content, ''), ps.crop_x, ps.crop_y, ps.crop_scale
		 FROM page_slots ps
		 JOIN book_pages bp ON bp.id = ps.page_id
		 WHERE bp.book_id = $1
		 ORDER BY ps.page_id, ps.slot_index`, bookID)
	if err != nil {
		return nil, fmt.Errorf("get all slots for book: %w", err)
	}
	defer slotRows.Close()
	slotsByPage := make(map[string][]database.PageSlot)
	for slotRows.Next() {
		var pageID string
		var s database.PageSlot
		if err := slotRows.Scan(&pageID, &s.SlotIndex, &s.PhotoUID, &s.TextContent, &s.CropX, &s.CropY, &s.CropScale); err != nil {
			return nil, fmt.Errorf("scan slot: %w", err)
		}
		slotsByPage[pageID] = append(slotsByPage[pageID], s)
	}
	if err := slotRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slots: %w", err)
	}

	for i := range pages {
		pages[i].Slots = slotsByPage[pages[i].ID]
	}
	return pages, nil
}

func (r *BookRepository) UpdatePage(ctx context.Context, page *database.BookPage) error {
	page.UpdatedAt = time.Now()
	var sectionID *string
	if page.SectionID != "" {
		sectionID = &page.SectionID
	}
	if page.Style == "" {
		page.Style = "modern"
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE book_pages SET section_id = $1, format = $2, style = $3, description = $4, split_position = $5, updated_at = $6 WHERE id = $7`,
		sectionID, page.Format, page.Style, page.Description, page.SplitPosition, page.UpdatedAt, page.ID)
	if err != nil {
		return fmt.Errorf("update page: %w", err)
	}
	return nil
}

func (r *BookRepository) DeletePage(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM book_pages WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete page: %w", err)
	}
	return nil
}

func (r *BookRepository) ReorderPages(ctx context.Context, bookID string, pageIDs []string) error {
	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for i, id := range pageIDs {
		_, err := tx.ExecContext(ctx,
			`UPDATE book_pages SET sort_order = $1, updated_at = NOW() WHERE id = $2 AND book_id = $3`,
			i, id, bookID)
		if err != nil {
			return fmt.Errorf("reorder page %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reorder pages: %w", err)
	}
	return nil
}

// --- Slots ---

func (r *BookRepository) GetPageSlots(ctx context.Context, pageID string) ([]database.PageSlot, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT slot_index, COALESCE(photo_uid, ''), COALESCE(text_content, ''), crop_x, crop_y, crop_scale FROM page_slots WHERE page_id = $1 ORDER BY slot_index`, pageID)
	if err != nil {
		return nil, fmt.Errorf("get page slots: %w", err)
	}
	defer rows.Close()
	var slots []database.PageSlot
	for rows.Next() {
		var s database.PageSlot
		if err := rows.Scan(&s.SlotIndex, &s.PhotoUID, &s.TextContent, &s.CropX, &s.CropY, &s.CropScale); err != nil {
			return nil, fmt.Errorf("scan slot: %w", err)
		}
		slots = append(slots, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate page slots: %w", err)
	}
	return slots, nil
}

func (r *BookRepository) AssignSlot(ctx context.Context, pageID string, slotIndex int, photoUID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO page_slots (page_id, slot_index, photo_uid, text_content, crop_x, crop_y, crop_scale) VALUES ($1, $2, $3, '', 0.5, 0.5, 1.0)
		 ON CONFLICT (page_id, slot_index) DO UPDATE SET photo_uid = EXCLUDED.photo_uid, text_content = '', crop_x = 0.5, crop_y = 0.5, crop_scale = 1.0`,
		pageID, slotIndex, photoUID)
	if err != nil {
		return fmt.Errorf("assign slot: %w", err)
	}
	return nil
}

func (r *BookRepository) AssignTextSlot(ctx context.Context, pageID string, slotIndex int, textContent string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO page_slots (page_id, slot_index, photo_uid, text_content, crop_x, crop_y, crop_scale) VALUES ($1, $2, NULL, $3, 0.5, 0.5, 1.0)
		 ON CONFLICT (page_id, slot_index) DO UPDATE SET photo_uid = NULL, text_content = EXCLUDED.text_content, crop_x = 0.5, crop_y = 0.5, crop_scale = 1.0`,
		pageID, slotIndex, textContent)
	if err != nil {
		return fmt.Errorf("assign text slot: %w", err)
	}
	return nil
}

func (r *BookRepository) ClearSlot(ctx context.Context, pageID string, slotIndex int) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM page_slots WHERE page_id = $1 AND slot_index = $2`,
		pageID, slotIndex)
	if err != nil {
		return fmt.Errorf("clear slot: %w", err)
	}
	return nil
}

func (r *BookRepository) SwapSlots(ctx context.Context, pageID string, slotA int, slotB int) error {
	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("swap slots begin tx: %w", err)
	}
	defer tx.Rollback()

	// Read current slot data (photo_uid + text_content + crop) for both slots
	rows, err := tx.QueryContext(ctx,
		`SELECT slot_index, photo_uid, COALESCE(text_content, ''), crop_x, crop_y, crop_scale FROM page_slots WHERE page_id = $1 AND slot_index IN ($2, $3)`,
		pageID, slotA, slotB)
	if err != nil {
		return fmt.Errorf("swap slots read: %w", err)
	}
	slotDataMap, err := scanSlotRows(rows)
	if err != nil {
		return fmt.Errorf("swap slots scan: %w", err)
	}

	if len(slotDataMap) != 2 {
		return fmt.Errorf("swap slots: expected 2 slots, found %d", len(slotDataMap))
	}

	// Delete both slots to avoid unique constraint violations
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM page_slots WHERE page_id = $1 AND slot_index IN ($2, $3)`,
		pageID, slotA, slotB); err != nil {
		return fmt.Errorf("swap slots delete: %w", err)
	}

	// Re-insert with swapped data (crop travels with photo)
	dataA := slotDataMap[slotA]
	dataB := slotDataMap[slotB]
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO page_slots (page_id, slot_index, photo_uid, text_content, crop_x, crop_y, crop_scale) VALUES ($1, $2, $3, $4, $5, $6, $7), ($1, $8, $9, $10, $11, $12, $13)`,
		pageID, slotA, dataB.photoUID, dataB.textContent, dataB.cropX, dataB.cropY, dataB.cropScale, slotB, dataA.photoUID, dataA.textContent, dataA.cropX, dataA.cropY, dataA.cropScale); err != nil {
		return fmt.Errorf("swap slots insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("swap slots commit: %w", err)
	}
	return nil
}

func (r *BookRepository) UpdateSlotCrop(ctx context.Context, pageID string, slotIndex int, cropX, cropY, cropScale float64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE page_slots SET crop_x = $1, crop_y = $2, crop_scale = $3 WHERE page_id = $4 AND slot_index = $5`,
		cropX, cropY, cropScale, pageID, slotIndex)
	if err != nil {
		return fmt.Errorf("update slot crop: %w", err)
	}
	return nil
}

func (r *BookRepository) GetPhotoBookMemberships(ctx context.Context, photoUID string) ([]database.PhotoBookMembership, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT pb.id, pb.title, bs.id, bs.title
		 FROM section_photos sp
		 JOIN book_sections bs ON sp.section_id = bs.id
		 JOIN photo_books pb ON bs.book_id = pb.id
		 WHERE sp.photo_uid = $1
		 ORDER BY pb.title, bs.sort_order`, photoUID)
	if err != nil {
		return nil, fmt.Errorf("get photo book memberships: %w", err)
	}
	defer rows.Close()
	var memberships []database.PhotoBookMembership
	for rows.Next() {
		var m database.PhotoBookMembership
		if err := rows.Scan(&m.BookID, &m.BookTitle, &m.SectionID, &m.SectionTitle); err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		memberships = append(memberships, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memberships: %w", err)
	}
	return memberships, nil
}

// slotData holds photo_uid, text_content, and crop values for swap operations.
type slotData struct {
	photoUID    *string // nil means NULL in DB
	textContent string
	cropX       float64
	cropY       float64
	cropScale   float64
}

func scanSlotRows(rows *sql.Rows) (map[int]slotData, error) {
	defer rows.Close()
	result := make(map[int]slotData)
	for rows.Next() {
		var idx int
		var uid *string
		var text string
		var cropX, cropY, cropScale float64
		if err := rows.Scan(&idx, &uid, &text, &cropX, &cropY, &cropScale); err != nil {
			return nil, fmt.Errorf("scan slot row: %w", err)
		}
		result[idx] = slotData{photoUID: uid, textContent: text, cropX: cropX, cropY: cropY, cropScale: cropScale}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slot rows: %w", err)
	}
	return result, nil
}
