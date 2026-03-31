/**
 * Normalized Levenshtein distance for detecting similar texts.
 */

function normalizeText(text: string): string {
  return text
    .toLowerCase()
    .trim()
    .replace(/[^\p{L}\p{N}\s]/gu, '')
    .replace(/\s+/g, ' ');
}

function levenshteinDistance(a: string, b: string): number {
  const m = a.length;
  const n = b.length;

  // Use single-row optimization for O(min(m,n)) space
  let prev = new Array<number>(n + 1);
  let curr = new Array<number>(n + 1);

  for (let j = 0; j <= n; j++) prev[j] = j;

  for (let i = 1; i <= m; i++) {
    curr[0] = i;
    for (let j = 1; j <= n; j++) {
      if (a[i - 1] === b[j - 1]) {
        curr[j] = prev[j - 1];
      } else {
        curr[j] = 1 + Math.min(prev[j], curr[j - 1], prev[j - 1]);
      }
    }
    [prev, curr] = [curr, prev];
  }

  return prev[n];
}

/** Returns similarity ratio between 0.0 and 1.0 */
export function textSimilarity(a: string, b: string): number {
  const na = normalizeText(a);
  const nb = normalizeText(b);

  if (na === nb) return 1.0;
  if (na.length === 0 || nb.length === 0) return 0.0;

  const maxLen = Math.max(na.length, nb.length);
  const dist = levenshteinDistance(na, nb);
  return 1 - dist / maxLen;
}

export interface DuplicatePair {
  entryA: { id: string; text: string };
  entryB: { id: string; text: string };
  similarity: number;
}

/** Find all pairs of texts with similarity above the threshold (default 0.6) */
export function findDuplicateTexts(
  entries: { id: string; text: string }[],
  threshold = 0.6,
): DuplicatePair[] {
  const pairs: DuplicatePair[] = [];

  for (let i = 0; i < entries.length; i++) {
    for (let j = i + 1; j < entries.length; j++) {
      const similarity = textSimilarity(entries[i].text, entries[j].text);
      if (similarity >= threshold) {
        pairs.push({
          entryA: entries[i],
          entryB: entries[j],
          similarity,
        });
      }
    }
  }

  // Sort by similarity descending
  pairs.sort((a, b) => b.similarity - a.similarity);
  return pairs;
}
