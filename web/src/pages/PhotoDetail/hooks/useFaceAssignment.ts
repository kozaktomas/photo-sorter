import { useState, useEffect } from 'react';
import { applyFaceMatch } from '../../../api/client';
import type { PhotoFace, PhotoFacesResponse, FaceSuggestion, Subject, MatchAction } from '../../../types';

interface UseFaceAssignmentReturn {
  selectedFaceIndex: number | null;
  setSelectedFaceIndex: (index: number | null) => void;
  applyingFace: number | null;
  applyError: string | null;
  manualName: string;
  setManualName: (name: string) => void;
  showManualInput: boolean;
  setShowManualInput: (show: boolean) => void;
  filteredSubjects: Subject[];
  showAutocomplete: boolean;
  setShowAutocomplete: (show: boolean) => void;
  handleApplySuggestion: (face: PhotoFace, suggestion: FaceSuggestion) => Promise<void>;
  handleManualAssign: (face: PhotoFace, personName: string) => Promise<void>;
  handleSelectAutocomplete: (subject: Subject) => void;
  selectFirstUnassignedFace: (faces: PhotoFace[]) => void;
  reassigningFaceIndex: number | null;
  handleStartReassign: (faceIndex: number) => void;
  handleCancelReassign: () => void;
  handleUnassign: (face: PhotoFace) => Promise<void>;
}

export function useFaceAssignment(
  uid: string | undefined,
  facesData: PhotoFacesResponse | null,
  subjects: Subject[],
  setFacesData: React.Dispatch<React.SetStateAction<PhotoFacesResponse | null>>
): UseFaceAssignmentReturn {
  const [selectedFaceIndex, setSelectedFaceIndex] = useState<number | null>(null);
  const [applyingFace, setApplyingFace] = useState<number | null>(null);
  const [applyError, setApplyError] = useState<string | null>(null);
  const [manualName, setManualName] = useState('');
  const [showManualInput, setShowManualInput] = useState(false);
  const [filteredSubjects, setFilteredSubjects] = useState<Subject[]>([]);
  const [showAutocomplete, setShowAutocomplete] = useState(false);
  const [reassigningFaceIndex, setReassigningFaceIndex] = useState<number | null>(null);

  // Filter subjects based on manual name input
  useEffect(() => {
    if (manualName.trim().length > 0) {
      const filtered = subjects.filter(s =>
        s.name.toLowerCase().includes(manualName.toLowerCase())
      ).slice(0, 5);
      setFilteredSubjects(filtered);
      setShowAutocomplete(filtered.length > 0);
    } else {
      setFilteredSubjects([]);
      setShowAutocomplete(false);
    }
  }, [manualName, subjects]);

  // Reset manual input and reassign state when selected face changes
  useEffect(() => {
    setManualName('');
    setShowManualInput(false);
    setShowAutocomplete(false);
    setApplyError(null);
    setReassigningFaceIndex(null);
  }, [selectedFaceIndex]);

  const selectFirstUnassignedFace = (faces: PhotoFace[]) => {
    const firstUnassigned = faces.findIndex(f => f.action !== 'already_done');
    if (firstUnassigned >= 0) {
      setSelectedFaceIndex(firstUnassigned);
    } else if (faces.length > 0) {
      setSelectedFaceIndex(0);
    }
  };

  const handleStartReassign = (faceIndex: number) => {
    setReassigningFaceIndex(faceIndex);
    setManualName('');
    setShowManualInput(false);
    setShowAutocomplete(false);
    setApplyError(null);
  };

  const handleCancelReassign = () => {
    setReassigningFaceIndex(null);
    setManualName('');
    setShowManualInput(false);
    setShowAutocomplete(false);
    setApplyError(null);
  };

  const handleUnassign = async (face: PhotoFace) => {
    if (!facesData || !uid) return;

    setApplyingFace(face.face_index);
    setApplyError(null);
    try {
      const response = await applyFaceMatch({
        photo_uid: uid,
        person_name: face.marker_name || '',
        action: 'unassign_person' as MatchAction,
        marker_uid: face.marker_uid,
        file_uid: facesData.file_uid,
        bbox_rel: face.bbox_rel,
        face_index: face.face_index,
      });

      if (!response.success) {
        setApplyError(response.error || 'Failed to unassign face');
        return;
      }

      setFacesData(prev => {
        if (!prev) return prev;
        return {
          ...prev,
          faces: prev.faces.map(f =>
            f.face_index === face.face_index
              ? { ...f, action: 'assign_person' as MatchAction, marker_name: '' }
              : f
          ),
        };
      });
    } catch (err) {
      setApplyError(err instanceof Error ? err.message : 'Failed to unassign face');
    } finally {
      setApplyingFace(null);
    }
  };

  const handleApplySuggestion = async (face: PhotoFace, suggestion: FaceSuggestion) => {
    if (!facesData || !uid) return;

    setApplyingFace(face.face_index);
    setApplyError(null);
    try {
      // When reassigning, use assign_person action since marker already exists
      const action = reassigningFaceIndex === face.face_index ? 'assign_person' as MatchAction : face.action;

      const response = await applyFaceMatch({
        photo_uid: uid,
        person_name: suggestion.person_name,
        action,
        marker_uid: face.marker_uid,
        file_uid: facesData.file_uid,
        bbox_rel: face.bbox_rel,
        face_index: face.face_index,
      });

      if (!response.success) {
        setApplyError(response.error || 'Failed to apply face assignment');
        return;
      }

      setFacesData(prev => {
        if (!prev) return prev;
        return {
          ...prev,
          faces: prev.faces.map(f =>
            f.face_index === face.face_index
              ? { ...f, action: 'already_done' as MatchAction, marker_name: suggestion.person_name, suggestions: [] }
              : f
          ),
        };
      });

      // Clear reassigning state after successful reassignment
      if (reassigningFaceIndex === face.face_index) {
        setReassigningFaceIndex(null);
      }
    } catch (err) {
      setApplyError(err instanceof Error ? err.message : 'Failed to apply face assignment');
    } finally {
      setApplyingFace(null);
    }
  };

  const handleManualAssign = async (face: PhotoFace, personName: string) => {
    if (!facesData || !personName.trim() || !uid) return;

    setApplyingFace(face.face_index);
    setApplyError(null);
    try {
      // When reassigning, use assign_person action since marker already exists
      const action = reassigningFaceIndex === face.face_index ? 'assign_person' as MatchAction : face.action;

      const response = await applyFaceMatch({
        photo_uid: uid,
        person_name: personName.trim(),
        action,
        marker_uid: face.marker_uid,
        file_uid: facesData.file_uid,
        bbox_rel: face.bbox_rel,
        face_index: face.face_index,
      });

      if (!response.success) {
        setApplyError(response.error || 'Failed to assign face');
        return;
      }

      setFacesData(prev => {
        if (!prev) return prev;
        return {
          ...prev,
          faces: prev.faces.map(f =>
            f.face_index === face.face_index
              ? { ...f, action: 'already_done' as MatchAction, marker_name: personName.trim(), suggestions: [] }
              : f
          ),
        };
      });

      setManualName('');
      setShowManualInput(false);
      setShowAutocomplete(false);

      // Clear reassigning state after successful reassignment
      if (reassigningFaceIndex === face.face_index) {
        setReassigningFaceIndex(null);
      }
    } catch (err) {
      setApplyError(err instanceof Error ? err.message : 'Failed to assign face');
    } finally {
      setApplyingFace(null);
    }
  };

  const handleSelectAutocomplete = (subject: Subject) => {
    setManualName(subject.name);
    setShowAutocomplete(false);
  };

  return {
    selectedFaceIndex,
    setSelectedFaceIndex,
    applyingFace,
    applyError,
    manualName,
    setManualName,
    showManualInput,
    setShowManualInput,
    filteredSubjects,
    showAutocomplete,
    setShowAutocomplete,
    handleApplySuggestion,
    handleManualAssign,
    handleSelectAutocomplete,
    selectFirstUnassignedFace,
    reassigningFaceIndex,
    handleStartReassign,
    handleCancelReassign,
    handleUnassign,
  };
}
