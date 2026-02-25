import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionSidebar } from './SectionSidebar';
import { SectionPhotoPool } from './SectionPhotoPool';
import type { BookDetail, SectionPhoto } from '../../types';

interface Props {
  book: BookDetail;
  sectionPhotos: Record<string, SectionPhoto[]>;
  loadSectionPhotos: (sectionId: string) => void;
  onRefresh: () => void;
}

export function SectionsTab({ book, sectionPhotos, loadSectionPhotos, onRefresh }: Props) {
  const { t } = useTranslation('pages');
  const [selectedId, setSelectedId] = useState<string | null>(
    book.sections.length > 0 ? book.sections[0].id : null
  );

  // Load photos when selection changes
  useEffect(() => {
    if (selectedId && !sectionPhotos[selectedId]) {
      loadSectionPhotos(selectedId);
    }
  }, [selectedId, sectionPhotos, loadSectionPhotos]);

  // Update selection if sections change
  useEffect(() => {
    if (selectedId && !book.sections.find(s => s.id === selectedId)) {
      setSelectedId(book.sections.length > 0 ? book.sections[0].id : null);
    }
  }, [book.sections, selectedId]);

  if (book.sections.length === 0 && !selectedId) {
    return (
      <div className="flex gap-4">
        <SectionSidebar
          bookId={book.id}
          chapters={book.chapters || []}
          sections={book.sections}
          selectedId={selectedId}
          onSelect={setSelectedId}
          onRefresh={onRefresh}
        />
        <div className="flex-1 text-center text-slate-500 py-12">
          {t('books.editor.noSections')}
        </div>
      </div>
    );
  }

  return (
    <div className="flex gap-4">
      <SectionSidebar
        bookId={book.id}
        chapters={book.chapters || []}
        sections={book.sections}
        selectedId={selectedId}
        onSelect={setSelectedId}
        onRefresh={onRefresh}
      />
      {selectedId && (
        <SectionPhotoPool
          sectionId={selectedId}
          photos={sectionPhotos[selectedId] || []}
          onRefresh={onRefresh}
          onReloadPhotos={() => loadSectionPhotos(selectedId)}
        />
      )}
    </div>
  );
}
