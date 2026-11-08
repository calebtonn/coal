const DAY = 24 * 3600;
const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ['year', 365 * DAY], ['month', 30 * DAY], ['week', 7 * DAY], ['day', DAY], ['hour', 3600], ['minute', 60],
];

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });

/** "3 days ago", "in 2 hours", "just now". */
export function relativeTime(iso: string, now: Date = new Date()): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return '';
  const seconds = Math.round((then - now.getTime()) / 1000);
  if (Math.abs(seconds) < 45) return 'just now';
  for (const [unit, size] of UNITS) {
    if (Math.abs(seconds) >= size) return rtf.format(Math.round(seconds / size), unit);
  }
  return rtf.format(Math.round(seconds / 60), 'minute');
}

export type SnippetPart = { text: string; bold: boolean };

/**
 * Split a search snippet on `<b>` and `</b>`. Everything else stays literal text,
 * so the caller renders it with normal (escaped) text interpolation.
 */
export function parseSnippet(snippet: string): SnippetPart[] {
  const parts: SnippetPart[] = [];
  let bold = false;
  for (const piece of snippet.split(/(<\/?b>)/)) {
    if (piece === '<b>') bold = true;
    else if (piece === '</b>') bold = false;
    else if (piece) parts.push({ text: piece, bold });
  }
  return parts;
}

export const formatSize = (b: number) =>
  b < 1024 ? `${b} B` : b < 1048576 ? `${(b / 1024).toFixed(1)} KB` : `${(b / 1048576).toFixed(1)} MB`;
