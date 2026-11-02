// Markers: every marked moment as a wall of short silent clips, by tag.
import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { clock } from '../lib/sphere'
import type { Marker, Tag } from '../lib/types'
import { usePaged } from '../components/scene'
import { useOnChange } from '../components/Layout'
import { Select } from '../components/Select'
import { Empty, Loading, useDebounce } from '../components/ui'
import { Icon } from '../components/icons'

export default function Markers() {
  const [params, setParams] = useSearchParams()
  const { settings } = useApp()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const tag = params.get('tag') || ''
  const sort = params.get('sort') || 'added'
  const [tags, setTags] = useState<Tag[]>([])
  useEffect(() => { api.get<Tag[]>(`${V1}/tags`).then(t => setTags(t.filter(x => x.markers > 0))).catch(() => {}) }, [])
  const { items, total, loading, more, next, reload } = usePaged<Marker>(`${V1}/markers?q=${encodeURIComponent(dq)}${tag ? `&tag=${tag}` : ''}&sort=${sort}`, 48)
  useOnChange(reload)
  const set = (k: string, v: string) => { const p = new URLSearchParams(params); if (v) p.set(k, v); else p.delete(k); setParams(p, { replace: true }) }
  return (
    <div>
      <div className="page-head"><div><h1>Markers</h1><p className="muted">{total} marked moments. Point at one to see it move; click to watch from there.</p></div></div>
      <div className="toolbar">
        <div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search markers and scenes…" /></div>
        <Select ariaLabel="Tag" value={tag} onChange={v => set('tag', v)} options={[{ value: '', label: 'Every tag' }, ...tags.map(t => ({ value: String(t.id), label: t.name, hint: String(t.markers) }))]} />
        <Select ariaLabel="Sort" value={sort} onChange={v => set('sort', v)} options={[{ value: 'added', label: 'Newest' }, { value: 'scene', label: 'By scene' }, { value: 'random', label: 'Random' }]} />
      </div>
      {!loading && !items.length && <Empty title="No markers yet" icon="marker">Open a scene, go to the moment and use Markers → “Mark this moment”.</Empty>}
      <div className={`marker-grid ${settings?.discreet ? 'discreet' : ''}`}>{items.map(m => <MarkerCard key={m.id} m={m} />)}</div>
      {loading && <Loading />}
      {more && !loading && <div className="load-more"><button type="button" className="btn" onClick={next}>Show more</button></div>}
    </div>
  )
}

function MarkerCard({ m }: { m: Marker }) {
  const [hot, setHot] = useState(false)
  const timer = useRef(0)
  return (
    <Link to={`/scenes/${m.scene_id}?t=${Math.floor(m.seconds)}`} className="mcard" onMouseEnter={() => { timer.current = window.setTimeout(() => setHot(true), 250) }} onMouseLeave={() => { window.clearTimeout(timer.current); setHot(false) }}>
      <img src={m.screenshot} alt="" loading="lazy" />
      {hot && <video src={m.preview} autoPlay muted loop playsInline />}
      <span className="scard-dur tnum">{clock(m.seconds)}</span>
      <span className="mcard-label"><b className="ellipsis">{m.title || m.tag?.name}</b><span className="muted ellipsis">{m.scene_name}</span></span>
    </Link>
  )
}
