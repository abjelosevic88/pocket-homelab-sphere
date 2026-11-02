// Cards, ratings and pickers shared by the pages.
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { clock, quality } from '../lib/sphere'
import type { Performer, Ref, Scene } from '../lib/types'
import { Icon } from './icons'
import { useDismiss } from './ui'

/** Half-star rating, 0..10. Click the same value again to clear it. */
export function Stars({ value, onChange, size = 16, readOnly }: { value: number; onChange?: (v: number) => void; size?: number; readOnly?: boolean }) {
  const [hover, setHover] = useState(0)
  const shown = hover || value
  return (
    <span className={`stars-input ${readOnly ? 'ro' : ''}`} onMouseLeave={() => setHover(0)} role={readOnly ? 'img' : 'radiogroup'} aria-label={`Rating ${value / 2} of 5`}>
      {[1, 2, 3, 4, 5].map(i => {
        const full = shown >= i * 2, half = !full && shown === i * 2 - 1
        return (
          <span key={i} className="star-slot" style={{ width: size, height: size }}>
            <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden><path d="M12 2l3 7h7l-5.5 4.5 2 7.5-6.5-4.5-6.5 4.5 2-7.5L2 9h7z" className={full ? 'star-on' : 'star-off'} />{half && <path d="M12 2v14.5L5.5 21l2-7.5L2 9h7z" className="star-on" />}</svg>
            {!readOnly && <>
              <button type="button" className="star-hit left" aria-label={`${i - 0.5} stars`} onMouseEnter={() => setHover(i * 2 - 1)} onClick={() => onChange?.(value === i * 2 - 1 ? 0 : i * 2 - 1)} />
              <button type="button" className="star-hit right" aria-label={`${i} stars`} onMouseEnter={() => setHover(i * 2)} onClick={() => onChange?.(value === i * 2 ? 0 : i * 2)} />
            </>}
          </span>
        )
      })}
    </span>
  )
}

/** A scene's card: cover, a silent preview when pointed at, length, quality, VR. */
export function SceneCard({ sc, selected, onSelect, selecting, queue }: { sc: Scene; selected?: boolean; onSelect?: (id: number, shift: boolean) => void; selecting?: boolean; queue?: string }) {
  const { settings } = useApp()
  const [hot, setHot] = useState(false)
  const vid = useRef<HTMLVideoElement>(null)
  const timer = useRef(0)
  const preview = settings?.hover_preview !== false && sc.preview
  const enter = () => { if (preview) timer.current = window.setTimeout(() => setHot(true), 220) }
  const leave = () => { window.clearTimeout(timer.current); setHot(false) }
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const progress = sc.mine.resume_seconds > 0 && sc.duration ? Math.min(100, sc.mine.resume_seconds / sc.duration * 100) : 0
  const q = quality(sc)
  const to = `/scenes/${sc.id}${queue ? `?${queue}` : ''}`
  return (
    <div className={`scard ${selected ? 'selected' : ''} ${sc.missing_since ? 'is-missing' : ''}`} onMouseEnter={enter} onMouseLeave={leave}>
      <Link to={to} className="scard-link" onClick={e => { if (selecting && onSelect) { e.preventDefault(); onSelect(sc.id, e.shiftKey) } }}>
        <div className="scard-media">
          {sc.cover ? <img className="scard-img" src={sc.cover} alt="" loading="lazy" /> : <div className="scard-img scard-none"><Icon name="film" size={26} /></div>}
          {hot && preview && <video ref={vid} className="scard-prev" src={sc.preview} autoPlay muted loop playsInline onLoadedData={() => vid.current?.play().catch(() => {})} />}
          <span className="scard-tl">
            {sc.is_vr && <span className="pill vr" title={sc.vr_label}>VR</span>}
            {q && <span className="pill">{q}</span>}
          </span>
          {sc.duration > 0 && <span className="scard-dur tnum">{clock(sc.duration)}</span>}
          {sc.mine.favorite && <span className="scard-fav" title="Favourite"><Icon name="heart" size={14} /></span>}
          {progress > 0 && <span className="scard-progress"><span style={{ width: `${progress}%` }} /></span>}
          {sc.missing_since && <span className="scard-missing"><Icon name="alert" size={13} />File missing</span>}
        </div>
        <div className="scard-body">
          <div className="scard-title" title={sc.name}>{sc.name}</div>
          <div className="scard-meta">
            {sc.studio && <span className="ellipsis">{sc.studio.name}</span>}
            {sc.date && <span className="nowrap">{sc.date.slice(0, 4)}</span>}
            {sc.mine.rating > 0 && <span className="scard-rating nowrap"><Icon name="star" size={12} />{(sc.mine.rating / 2).toFixed(sc.mine.rating % 2 ? 1 : 0)}</span>}
            {sc.mine.o_count > 0 && <span className="nowrap" title="O count"><Icon name="droplet" size={12} />{sc.mine.o_count}</span>}
            {sc.mine.play_count > 0 && <span className="nowrap" title="Times watched"><Icon name="eye" size={12} />{sc.mine.play_count}</span>}
          </div>
          {sc.performers.length > 0 && <div className="scard-perfs ellipsis">{sc.performers.map(p => p.name).join(', ')}</div>}
        </div>
      </Link>
      {onSelect && <button type="button" className={`scard-check ${selected ? 'on' : ''}`} aria-label={selected ? 'Unselect' : 'Select'} onClick={e => onSelect(sc.id, e.shiftKey)}><Icon name={selected ? 'check-square' : 'square'} size={16} /></button>}
    </div>
  )
}

export function SceneGrid({ items, size, ...rest }: { items: Scene[]; size?: string; selected?: Set<number>; onSelect?: (id: number, shift: boolean) => void; selecting?: boolean; queue?: string }) {
  const { settings } = useApp()
  return (
    <div className={`sgrid size-${size || settings?.grid_size || 'm'} ${settings?.discreet ? 'discreet' : ''}`}>
      {items.map(sc => <SceneCard key={sc.id} sc={sc} selected={rest.selected?.has(sc.id)} onSelect={rest.onSelect} selecting={rest.selecting} queue={rest.queue} />)}
    </div>
  )
}

/** A row of scenes with a heading, scrolling sideways. */
export function Shelf({ title, icon, items, more }: { title: string; icon: string; items: Scene[]; more?: string }) {
  const { settings } = useApp()
  if (!items.length) return null
  return (
    <section className="shelf">
      <div className="sec-head"><h2 className="sec-h"><Icon name={icon} size={16} />{title}</h2>{more && <Link to={more} className="small">See all</Link>}</div>
      <div className={`shelf-row ${settings?.discreet ? 'discreet' : ''}`}>{items.map(sc => <SceneCard key={sc.id} sc={sc} />)}</div>
    </section>
  )
}

export function initialsOf(name: string) { return name.split(/\s+/).filter(Boolean).slice(0, 2).map(w => w[0]?.toUpperCase()).join('') }

export function PerformerCard({ p }: { p: Performer }) {
  const { settings } = useApp()
  return (
    <Link to={`/performers/${p.id}`} className={`pcard ${settings?.discreet ? 'discreet' : ''}`}>
      <div className="pcard-media">
        {p.image ? <img src={p.image} alt="" loading="lazy" /> : <div className="pcard-none">{initialsOf(p.name)}</div>}
        {p.favorite && <span className="scard-fav"><Icon name="heart" size={14} /></span>}
      </div>
      <div className="pcard-body">
        <div className="pcard-name ellipsis" title={p.name}>{p.name}{p.disambiguation && <span className="muted"> ({p.disambiguation})</span>}</div>
        <div className="scard-meta">
          <span>{p.scenes} {p.scenes === 1 ? 'scene' : 'scenes'}</span>
          {p.age > 0 && <span>{p.age}</span>}
          {p.country && <span className="ellipsis">{p.country}</span>}
        </div>
      </div>
    </Link>
  )
}

/** Chips of performers/tags with links. */
export function RefChips({ items, kind, icon }: { items: Ref[]; kind: 'performers' | 'tags' | 'studios' | 'groups'; icon?: string }) {
  if (!items.length) return null
  return (
    <div className="chips">
      {items.map(r => (
        <Link key={r.id} to={`/${kind}/${r.id}`} className={`ref-chip ${kind}`}>
          {kind === 'performers' && (r.image ? <img src={r.image} alt="" /> : <span className="ref-ini">{initialsOf(r.name)}</span>)}
          {icon && <Icon name={icon} size={12} />}{r.name}
        </Link>
      ))}
    </div>
  )
}

// ---------- the picker ----------

interface PickOpt { id: number; name: string; hint?: string }

/** Many-of picker with search; new names can be typed in (returned separately). */
export function Picker({ kind, value, onChange, newNames, onNewNames, placeholder, single }: {
  kind: 'performers' | 'tags' | 'studios' | 'groups'; value: Ref[]; onChange: (v: Ref[]) => void
  newNames?: string[]; onNewNames?: (v: string[]) => void; placeholder?: string; single?: boolean
}) {
  const [q, setQ] = useState('')
  const [open, setOpen] = useState(false)
  const [opts, setOpts] = useState<PickOpt[]>([])
  const [hl, setHl] = useState(0)
  const box = useRef<HTMLDivElement>(null)
  useDismiss(box, () => setOpen(false), open)
  useEffect(() => {
    if (!open) return
    const t = window.setTimeout(() => {
      const url = kind === 'performers' ? `${V1}/performers?per=30&sort=scenes&dir=desc&q=${encodeURIComponent(q)}` : `${V1}/${kind}?q=${encodeURIComponent(q)}`
      api.get<unknown>(url).then(r => {
        const list = (Array.isArray(r) ? r : (r as { items: unknown[] }).items) as { id: number; name: string; disambiguation?: string; scenes?: number }[]
        setOpts(list.slice(0, 40).map(x => ({ id: x.id, name: x.name + (x.disambiguation ? ` (${x.disambiguation})` : ''), hint: x.scenes != null ? String(x.scenes) : undefined })))
        setHl(0)
      }).catch(() => {})
    }, 150)
    return () => window.clearTimeout(t)
  }, [q, open, kind])
  const chosen = useMemo(() => new Set(value.map(v => v.id)), [value])
  const avail = opts.filter(o => !chosen.has(o.id))
  const canCreate = !!onNewNames && q.trim().length > 1 && !opts.some(o => o.name.toLowerCase() === q.trim().toLowerCase()) && !(newNames || []).some(n => n.toLowerCase() === q.trim().toLowerCase())
  const pick = (o: PickOpt) => { onChange(single ? [{ id: o.id, name: o.name }] : [...value, { id: o.id, name: o.name }]); setQ(''); if (single) setOpen(false) }
  const create = () => { if (!onNewNames) return; onNewNames(single ? [q.trim()] : [...(newNames || []), q.trim()]); if (single) onChange([]); setQ(''); if (single) setOpen(false) }
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setHl(h => Math.min(h + 1, avail.length - (canCreate ? 0 : 1))) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setHl(h => Math.max(h - 1, 0)) }
    else if (e.key === 'Enter') { e.preventDefault(); if (hl < avail.length) pick(avail[hl]); else if (canCreate) create() }
    else if (e.key === 'Backspace' && !q && value.length) onChange(value.slice(0, -1))
    else if (e.key === 'Escape') setOpen(false)
  }
  return (
    <div ref={box} className={`picker ${open ? 'open' : ''}`}>
      <div className="picker-box" onClick={() => setOpen(true)}>
        {value.map(v => <span key={v.id} className="picker-chip">{v.name}<button type="button" aria-label={`Remove ${v.name}`} onClick={e => { e.stopPropagation(); onChange(value.filter(x => x.id !== v.id)) }}><Icon name="x" size={12} /></button></span>)}
        {(newNames || []).map(n => <span key={'n' + n} className="picker-chip new" title="New — created when you save">{n}<button type="button" aria-label={`Remove ${n}`} onClick={e => { e.stopPropagation(); onNewNames?.((newNames || []).filter(x => x !== n)) }}><Icon name="x" size={12} /></button></span>)}
        <input value={q} onChange={e => { setQ(e.target.value); setOpen(true) }} onFocus={() => setOpen(true)} onKeyDown={onKey} placeholder={value.length || newNames?.length ? '' : placeholder || 'Search…'} />
      </div>
      {open && (avail.length > 0 || canCreate) && (
        <div className="sel-pop picker-pop">
          <div className="sel-list">
            {avail.map((o, i) => <div key={o.id} className={`sel-opt ${i === hl ? 'hl' : ''}`} onMouseEnter={() => setHl(i)} onMouseDown={e => { e.preventDefault(); pick(o) }}><span className="sel-lbl">{o.name}</span>{o.hint && <span className="sel-hint">{o.hint}</span>}</div>)}
            {canCreate && <div className={`sel-opt action ${hl === avail.length ? 'hl' : ''}`} onMouseEnter={() => setHl(avail.length)} onMouseDown={e => { e.preventDefault(); create() }}><Icon name="plus" size={14} /><span className="sel-lbl">Add “{q.trim()}”</span></div>}
          </div>
        </div>
      )}
    </div>
  )
}

/** Infinite list paging: "Load more" with the next page appended. */
export function usePaged<T>(url: string | null, per = 60) {
  const [items, setItems] = useState<T[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [tick, setTick] = useState(0)
  useEffect(() => { setPage(1) }, [url])
  useEffect(() => {
    if (!url) return
    let gone = false
    setLoading(true)
    const sep = url.includes('?') ? '&' : '?'
    api.get<{ items: T[]; total: number }>(`${url}${sep}page=${page}&per=${per}`).then(r => {
      if (gone) return
      setItems(prev => page === 1 ? r.items : [...prev, ...r.items])
      setTotal(r.total)
      setError('')
    }).catch(e => { if (!gone) setError((e as Error).message) }).finally(() => { if (!gone) setLoading(false) })
    return () => { gone = true }
  }, [url, page, per, tick])
  return { items, total, loading, error, more: items.length < total, next: () => setPage(p => p + 1), reload: () => { setPage(1); setTick(t => t + 1) }, setItems }
}
