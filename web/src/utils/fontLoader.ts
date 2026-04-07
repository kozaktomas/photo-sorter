const loadedFonts = new Set<string>();

export function loadGoogleFont(family: string, spec: string): void {
  const key = `${family}:${spec}`;
  if (loadedFonts.has(key)) return;
  loadedFonts.add(key);
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = `https://fonts.googleapis.com/css2?family=${family}:${spec}&display=swap`;
  document.head.appendChild(link);
}

export function loadFontByInfo(font: { google_family: string; google_spec: string }): void {
  loadGoogleFont(font.google_family, font.google_spec);
}
