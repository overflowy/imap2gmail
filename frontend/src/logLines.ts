export function stripRSS(line: string) {
  return line.replace(/\s+\[RSS [^\]]+\]$/, "");
}

export function splitLogContent(content: string) {
  const lines = content.split("\n").map(stripRSS);
  if (lines[lines.length - 1] === "") lines.pop();
  return lines;
}

export function mergeHistoricalAndLiveLines(historical: string[], live: string[]) {
  if (historical.length === 0) return live;
  if (live.length === 0) return historical;

  for (let overlap = Math.min(historical.length, live.length); overlap > 0; overlap--) {
    let matches = true;
    for (let i = 0; i < overlap; i++) {
      if (historical[historical.length - overlap + i] !== live[i]) {
        matches = false;
        break;
      }
    }
    if (matches) return [...historical, ...live.slice(overlap)];
  }

  return [...historical, ...live];
}
