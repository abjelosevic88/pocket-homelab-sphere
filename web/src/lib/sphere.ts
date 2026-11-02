// Small helpers shared by the pages: lengths, sizes, the sort choices and the list's URL filters.
import type { Scene } from './types'

/** 3725 → "1:02:05"; 125 → "2:05". */
export function clock(sec: number): string {
  if (!isFinite(sec) || sec < 0) sec = 0
  const s = Math.floor(sec % 60), m = Math.floor(sec / 60) % 60, h = Math.floor(sec / 3600)
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
}

/** 5400 → "1 h 30 min". */
export function longDuration(sec: number): string {
  const m = Math.round(sec / 60)
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60), r = m % 60
  return r ? `${h} h ${r} min` : `${h} h`
}

/** "2160p", "4K VR" — what people call a video's quality. */
export function quality(sc: Pick<Scene, 'width' | 'height' | 'is_vr'>): string {
  const h = sc.is_vr ? Math.round(sc.width / 2) : sc.height
  if (!h) return ''
  if (sc.is_vr) {
    if (sc.width >= 7600) return '8K'
    if (sc.width >= 5700) return '6K'
    if (sc.width >= 5000) return '5K'
    if (sc.width >= 3800) return '4K'
    return `${sc.height}p`
  }
  if (h >= 4300) return '8K'
  if (h >= 2100) return '4K'
  if (h >= 1400) return '1440p'
  if (h >= 1000) return '1080p'
  if (h >= 700) return '720p'
  return `${h}p`
}

export const SORTS: { value: string; label: string; dir: 'asc' | 'desc' }[] = [
  { value: 'added', label: 'Added', dir: 'desc' },
  { value: 'date', label: 'Release date', dir: 'desc' },
  { value: 'title', label: 'Title', dir: 'asc' },
  { value: 'rating', label: 'Rating', dir: 'desc' },
  { value: 'played', label: 'Last watched', dir: 'desc' },
  { value: 'plays', label: 'Times watched', dir: 'desc' },
  { value: 'o', label: 'O count', dir: 'desc' },
  { value: 'duration', label: 'Length', dir: 'desc' },
  { value: 'resolution', label: 'Quality', dir: 'desc' },
  { value: 'size', label: 'File size', dir: 'desc' },
  { value: 'path', label: 'File path', dir: 'asc' },
  { value: 'random', label: 'Random', dir: 'desc' },
]

/** The list's filters live in the URL so a view can be saved, shared, reloaded and queued. */
export const FILTER_KEYS = ['q', 'performers', 'tags', 'tags_all', 'studios', 'groups', 'rating', 'favorite', 'unwatched', 'watched', 'progress', 'vr', 'min_h', 'max_h', 'min_dur', 'max_dur', 'organized', 'markers', 'folder', 'missing', 'trash', 'hidden', 'sort', 'dir', 'seed']

export function listQuery(params: URLSearchParams, extra: Record<string, string> = {}): string {
  const q = new URLSearchParams()
  for (const k of FILTER_KEYS) { const v = params.get(k); if (v) q.set(k, v) }
  for (const [k, v] of Object.entries(extra)) q.set(k, v)
  return q.toString()
}

/** How many filters are set (not counting the search and the sort). */
export function filterCount(p: URLSearchParams): number {
  return FILTER_KEYS.filter(k => !['q', 'sort', 'dir', 'seed'].includes(k) && p.get(k)).length
}

export const RESOLUTIONS = [{ value: '', label: 'Any quality' }, { value: '2160', label: '4K and up' }, { value: '1080', label: '1080p and up' }, { value: '720', label: '720p and up' }]

export function ratingStars(r: number): string { return r ? (r / 2).toFixed(r % 2 ? 1 : 0) : '' }

export function genderLabel(g: string): string {
  return ({ female: 'Female', male: 'Male', trans_female: 'Trans woman', trans_male: 'Trans man', non_binary: 'Non-binary', intersex: 'Intersex', transgender_female: 'Trans woman', transgender_male: 'Trans man' } as Record<string, string>)[g] || g
}

/** A seed for random order that stays the same for an hour (paging works). */
export function hourSeed(): string { const d = new Date(); return String(d.getFullYear() * 10000 + (d.getMonth() + 1) * 100 + d.getDate()) + String(d.getHours()) }
