/** Compact relative time in Chinese, e.g. 刚刚 / 5 分钟前 / 3 小时前 / 2 天前. */
export function relTime(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  const diff = Date.now() - t;
  if (diff < 60_000) return '刚刚';
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`;
  if (diff < 7 * 86_400_000) return `${Math.floor(diff / 86_400_000)} 天前`;
  return new Date(t).toLocaleDateString();
}

/** Clock time (HH:MM) for message meta rows. */
export function hhmm(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
}

/** Whether the timestamp is within the last 24 hours (overview "recently done" cutoff). */
export function within24h(iso: string): boolean {
  const t = Date.parse(iso);
  return !Number.isNaN(t) && Date.now() - t < 86_400_000;
}
