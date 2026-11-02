// The scenes: search, filters in the URL, sort, grid / list, save a view, select many.
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { clock, filterCount, hourSeed, listQuery, quality, RESOLUTIONS, SORTS } from '../lib/sphere'
import type { Ref, Scene } from '../lib/types'
import { Picker, SceneGrid, Stars, usePaged } from '../components/scene'
import { announceChange, useOnChange, useShell } from '../components/Layout'
import { Select } from '../components/Select'
import { Confirm, Empty, Loading, Modal, Field, Segmented, useDebounce, useToast, errMsg } from '../components/ui'
import { Icon } from '../components/icons'
import { fmtBytes } from '../lib/format'

export default function Scenes() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const { toast } = useToast()
  const { reload: reloadShell } = useShell()
  const [q, setQ] = useState(params.get('q') || '')
  const dq = useDebounce(q, 250)
  const [view, setView] = useState<'grid' | 'list'>(() => (localStorage.getItem('psp-view') as 'grid' | 'list') || 'grid')
  const [panel, setPanel] = useState(false)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [lastPick, setLastPick] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)
  const [bulk, setBulk] = useState(false)
  const [confirmTrash, setConfirmTrash] = useState(false)

  useEffect(() => { if ((params.get('q') || '') !== dq) { const p = new URLSearchParams(params); if (dq) p.set('q', dq); else p.delete('q'); setParams(p, { replace: true }) } }, [dq]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { localStorage.setItem('psp-view', view) }, [view])

  const query = listQuery(params)
  const { items, total, loading, error, more, next, reload } = usePaged<Scene>(`${V1}/scenes?${query}`, 60)
  useOnChange(reload)
  useEffect(() => { setSelected(new Set()) }, [query])

  const set = (k: string, v: string) => { const p = new URLSearchParams(params); if (v) p.set(k, v); else p.delete(k); setParams(p, { replace: true }) }
  const sort = params.get('sort') || settings?.default_sort || 'added'
  const sortDef = SORTS.find(s => s.value === sort)
  const dir = params.get('dir') || sortDef?.dir || 'desc'
  const trash = params.get('trash') === '1'
  const nf = filterCount(params)

  const pick = (id: number, shift: boolean) => {
    setSelected(prev => {
      const n = new Set(prev)
      if (shift && lastPick != null) {
        const a = items.findIndex(x => x.id === lastPick), b = items.findIndex(x => x.id === id)
        if (a >= 0 && b >= 0) for (let i = Math.min(a, b); i <= Math.max(a, b); i++) n.add(items[i].id)
      } else if (n.has(id)) n.delete(id); else n.add(id)
      return n
    })
    setLastPick(id)
  }
  const selecting = selected.size > 0
  const title = trash ? 'Trash' : params.get('vr') === 'yes' && nf === 1 ? 'VR' : params.get('favorite') ? 'Favourites' : params.get('progress') ? 'Continue watching' : params.get('unwatched') ? 'Not watched yet' : 'Scenes'

  const restore = async (id: number) => { try { await api.post(`${V1}/scenes/${id}/restore`); toast('Back in the library'); reload(); announceChange() } catch (e) { toast(errMsg(e), 'err') } }

  return (
    <div className="scenes-page">
      <div className="page-head">
        <div><h1>{title}</h1><p className="muted tnum">{loading && !items.length ? 'Loading…' : `${total.toLocaleString()} ${total === 1 ? 'scene' : 'scenes'}`}{trash && settings?.trash_days ? ` · deleted for good after ${settings.trash_days} days` : ''}</p></div>
        {!trash && <div className="row wrap">
          <button type="button" className="btn" onClick={() => navigate(`/scenes/${items[0]?.id}?${query}`)} disabled={!items.length} title="Play this list from the top"><Icon name="play" size={15} />Play all</button>
          <button type="button" className="btn" onClick={() => setSaving(true)}><Icon name="bookmark" size={15} />Save view</button>
        </div>}
      </div>

      <div className="toolbar">
        <div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search titles, performers, tags, studios, files…" aria-label="Search" /></div>
        <button type="button" className={`btn ${panel || nf ? 'active' : ''}`} onClick={() => setPanel(p => !p)} aria-expanded={panel}><Icon name="sliders" size={15} />Filters{nf > 0 && <span className="count-pill">{nf}</span>}</button>
        <Select ariaLabel="Sort" value={sort} onChange={v => { const p = new URLSearchParams(params); p.set('sort', v); p.delete('dir'); if (v === 'random') p.set('seed', hourSeed()); setParams(p, { replace: true }) }} options={SORTS.map(s => ({ value: s.value, label: s.label }))} />
        {sort !== 'random'
          ? <button type="button" className="btn icon-only" onClick={() => set('dir', dir === 'asc' ? 'desc' : 'asc')} aria-label={dir === 'asc' ? 'Ascending' : 'Descending'} title={dir === 'asc' ? 'Ascending' : 'Descending'}><Icon name={dir === 'asc' ? 'chevron-up' : 'chevron-down'} size={16} /></button>
          : <button type="button" className="btn icon-only" onClick={() => set('seed', String(Math.floor(Math.random() * 1e6)))} aria-label="Shuffle again" title="Shuffle again"><Icon name="shuffle" size={16} /></button>}
        <Segmented size="sm" value={view} onChange={setView} options={[{ value: 'grid', label: <Icon name="grid" size={15} />, title: 'Pictures' }, { value: 'list', label: <Icon name="list" size={15} />, title: 'List' }]} label="View" />
      </div>
      {panel && <FilterPanel />}
      <ActiveFilters />

      {selecting && (
        <div className="bulk-bar">
          <b>{selected.size} selected</b>
          <button type="button" className="btn sm" onClick={() => setSelected(new Set(items.map(x => x.id)))}>Select all {items.length}</button>
          <span className="spacer" />
          {!trash && <button type="button" className="btn sm primary" onClick={() => setBulk(true)}><Icon name="edit" size={14} />Edit</button>}
          {!trash && <button type="button" className="btn sm danger-text" onClick={() => setConfirmTrash(true)}><Icon name="trash" size={14} />Delete</button>}
          {trash && <button type="button" className="btn sm" onClick={async () => { for (const id of selected) await restore(id); setSelected(new Set()) }}><Icon name="undo" size={14} />Restore</button>}
          <button type="button" className="btn sm ghost" onClick={() => setSelected(new Set())}>Cancel</button>
        </div>
      )}

      {error && <div className="callout bad"><Icon name="alert" size={15} /><span>{error}</span></div>}
      {!loading && !items.length && !error && (
        <Empty title={trash ? 'The trash is empty' : nf || q ? 'Nothing matches' : 'No scenes yet'} icon={trash ? 'trash' : 'film'}>
          {trash ? 'Deleted scenes wait here before they are gone for good.' : nf || q ? <button type="button" className="link-btn" onClick={() => { setQ(''); setParams(new URLSearchParams(), { replace: true }) }}>Clear the filters</button> : <>Put videos into the library folder or <Link to="/downloads">download one from a link</Link>.</>}
        </Empty>
      )}
      {view === 'grid'
        ? <SceneGrid items={items} selected={selected} onSelect={pick} selecting={selecting} queue={query} />
        : <SceneTable items={items} selected={selected} onSelect={pick} queue={query} trash={trash} onRestore={restore} />}
      {loading && items.length > 0 && <Loading />}
      {more && !loading && <div className="load-more"><button type="button" className="btn" onClick={next}>Show more ({(total - items.length).toLocaleString()} left)</button></div>}

      {saving && <SaveView query={query} onClose={() => setSaving(false)} onSaved={() => { setSaving(false); reloadShell() }} />}
      {bulk && <BulkEdit ids={[...selected]} onClose={() => setBulk(false)} onDone={() => { setBulk(false); setSelected(new Set()); reload(); announceChange() }} />}
      {confirmTrash && <Confirm title={`Delete ${selected.size} ${selected.size === 1 ? 'scene' : 'scenes'}?`} confirmLabel="Move to trash"
        message={`The files go to the trash folder in the library and can be restored for ${settings?.trash_days || 30} days.`}
        onCancel={() => setConfirmTrash(false)}
        onConfirm={async () => { setConfirmTrash(false); try { const r = await api.post<{ changed: number; failed: string[] }>(`${V1}/scenes/bulk`, { ids: [...selected], trash: true }); toast(r.failed?.length ? `${r.failed.length} could not be moved` : 'Moved to the trash', r.failed?.length ? 'err' : 'ok'); setSelected(new Set()); reload(); announceChange() } catch (e) { toast(errMsg(e), 'err') } }} />}
    </div>
  )
}

function SceneTable({ items, selected, onSelect, queue, trash, onRestore }: { items: Scene[]; selected: Set<number>; onSelect: (id: number, shift: boolean) => void; queue: string; trash: boolean; onRestore: (id: number) => void }) {
  if (!items.length) return null
  return (
    <div className="card"><table className="table compact scene-table">
      <thead><tr><th style={{ width: 28 }} /><th>Title</th><th className="hide-sm">Studio</th><th className="hide-sm">Performers</th><th>Date</th><th className="num">Length</th><th className="hide-sm">Quality</th><th className="hide-sm">Rating</th><th className="num hide-sm">Size</th>{trash && <th />}</tr></thead>
      <tbody>{items.map(sc => (
        <tr key={sc.id} className={selected.has(sc.id) ? 'selected' : ''}>
          <td><button type="button" className="icon-btn" aria-label="Select" onClick={e => onSelect(sc.id, e.shiftKey)}><Icon name={selected.has(sc.id) ? 'check-square' : 'square'} size={15} /></button></td>
          <td><Link to={`/scenes/${sc.id}?${queue}`} className="row" style={{ gap: 6 }}>{sc.is_vr && <span className="pill vr">VR</span>}<span className="ellipsis" style={{ maxWidth: 420 }}>{sc.name}</span></Link></td>
          <td className="hide-sm">{sc.studio ? <Link to={`/studios/${sc.studio.id}`}>{sc.studio.name}</Link> : <span className="muted">—</span>}</td>
          <td className="hide-sm ellipsis" style={{ maxWidth: 220 }}>{sc.performers.map(p => p.name).join(', ')}</td>
          <td className="nowrap muted">{sc.date || '—'}</td>
          <td className="num">{sc.duration ? clock(sc.duration) : ''}</td>
          <td className="hide-sm">{quality(sc)}</td>
          <td className="hide-sm">{sc.mine.rating ? <Stars value={sc.mine.rating} readOnly size={12} /> : ''}</td>
          <td className="num hide-sm muted">{fmtBytes(sc.size_bytes)}</td>
          {trash && <td className="right"><button type="button" className="btn sm" onClick={() => onRestore(sc.id)}>Restore</button></td>}
        </tr>
      ))}</tbody>
    </table></div>
  )
}

/** The filters, all in the URL. */
function FilterPanel() {
  const [params, setParams] = useSearchParams()
  const set = (k: string, v: string) => { const p = new URLSearchParams(params); if (v) p.set(k, v); else p.delete(k); setParams(p, { replace: true }) }
  const [names, setNames] = useState<Record<string, Ref[]>>({})
  // the names of the chosen performers, tags and studios
  useEffect(() => {
    const load = async (kind: 'performers' | 'tags' | 'studios') => {
      const ids = (params.get(kind) || '').split(',').filter(Boolean)
      if (!ids.length) return [] as Ref[]
      const out: Ref[] = []
      for (const id of ids) { try { const r = await api.get<{ id: number; name: string }>(`${V1}/${kind}/${id}`); out.push({ id: r.id, name: r.name }) } catch { /* gone */ } }
      return out
    }
    Promise.all([load('performers'), load('tags'), load('studios')]).then(([p, t, s]) => setNames({ performers: p, tags: t, studios: s }))
  }, []) // eslint-disable-line react-hooks/exhaustive-deps
  const setRefs = (kind: string, v: Ref[]) => { setNames(n => ({ ...n, [kind]: v })); set(kind, v.map(x => x.id).join(',')) }
  const yesNo = (k: string) => <Segmented size="sm" value={params.get(k) || ''} onChange={v => set(k, v)} options={[{ value: '', label: 'Any' }, { value: 'yes', label: 'Yes' }, { value: 'no', label: 'No' }]} />
  const flag = (k: string, label: string) => <label className="check-row"><input type="checkbox" checked={params.get(k) === '1'} onChange={e => set(k, e.target.checked ? '1' : '')} />{label}</label>
  return (
    <div className="card pad filter-panel">
      <div className="filter-grid">
        <Field label="Performers"><Picker kind="performers" value={names.performers || []} onChange={v => setRefs('performers', v)} placeholder="Anyone" /></Field>
        <Field label={<span className="row" style={{ gap: 8 }}>Tags <label className="check-row small"><input type="checkbox" checked={params.get('tags_all') === '1'} onChange={e => set('tags_all', e.target.checked ? '1' : '')} />all of them</label></span>}><Picker kind="tags" value={names.tags || []} onChange={v => setRefs('tags', v)} placeholder="Any tag" /></Field>
        <Field label="Studios"><Picker kind="studios" value={names.studios || []} onChange={v => setRefs('studios', v)} placeholder="Any studio" /></Field>
        <Field label="VR">{yesNo('vr')}</Field>
        <Field label="Quality"><Select ariaLabel="Quality" value={params.get('min_h') || ''} onChange={v => set('min_h', v)} options={RESOLUTIONS} /></Field>
        <Field label="Length (minutes)"><div className="row"><input type="number" min={0} placeholder="from" value={params.get('min_dur') || ''} onChange={e => set('min_dur', e.target.value)} /><input type="number" min={0} placeholder="to" value={params.get('max_dur') || ''} onChange={e => set('max_dur', e.target.value)} /></div></Field>
        <Field label="Your rating, at least"><Stars value={Number(params.get('rating') || 0)} onChange={v => set('rating', v ? String(v) : '')} /></Field>
        <Field label="Organized">{yesNo('organized')}</Field>
        <Field label="Folder"><input value={params.get('folder') || ''} onChange={e => set('folder', e.target.value)} placeholder="e.g. Downloads" /></Field>
      </div>
      <div className="row wrap" style={{ gap: 16, marginTop: 12 }}>
        {flag('favorite', 'Favourites')}
        {flag('unwatched', 'Not watched')}
        {flag('watched', 'Watched')}
        {flag('progress', 'Started, not finished')}
        {flag('markers', 'Has markers')}
        {flag('missing', 'File missing')}
        {flag('hidden', 'Show hidden')}
        <span className="spacer" />
        <button type="button" className="btn sm ghost" onClick={() => { const p = new URLSearchParams(); for (const k of ['q', 'sort', 'dir', 'seed']) { const v = params.get(k); if (v) p.set(k, v) } setParams(p, { replace: true }); setNames({}) }}>Clear filters</button>
      </div>
    </div>
  )
}

/** The filters that are set, as chips that take them off. */
function ActiveFilters() {
  const [params, setParams] = useSearchParams()
  const labels: Record<string, (v: string) => string> = {
    vr: v => v === 'yes' ? 'VR' : 'Not VR', favorite: () => 'Favourites', unwatched: () => 'Not watched', watched: () => 'Watched', progress: () => 'Started',
    rating: v => `${Number(v) / 2}★ and up`, min_h: v => `${v}p and up`, max_h: v => `up to ${v}p`, min_dur: v => `${v}+ min`, max_dur: v => `under ${v} min`,
    organized: v => v === 'yes' ? 'Organized' : 'Not organized', markers: () => 'Has markers', folder: v => `In ${v}`, missing: () => 'File missing', hidden: () => 'With hidden',
    performers: v => `${v.split(',').length} performer${v.includes(',') ? 's' : ''}`, tags: v => `${v.split(',').length} tag${v.includes(',') ? 's' : ''}`,
    studios: v => `${v.split(',').length} studio${v.includes(',') ? 's' : ''}`, groups: () => 'In a group', tags_all: () => 'All tags',
  }
  const on = Object.keys(labels).filter(k => params.get(k))
  if (!on.length) return null
  return (
    <div className="filters">
      {on.map(k => <button key={k} type="button" className="chip sm active" onClick={() => { const p = new URLSearchParams(params); p.delete(k); setParams(p, { replace: true }) }}>{labels[k](params.get(k)!)}<Icon name="x" size={12} /></button>)}
    </div>
  )
}

function SaveView({ query, onClose, onSaved }: { query: string; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [name, setName] = useState('')
  const [pinned, setPinned] = useState(true)
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    try { await api.post(`${V1}/filters`, { mode: 'scenes', name, query, pinned }); toast('View saved'); onSaved() } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <Modal title="Save this view" onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={name} onChange={e => setName(e.target.value)} autoFocus required maxLength={80} placeholder="4K VR, newest first" /></Field>
        <label className="check-row"><input type="checkbox" checked={pinned} onChange={e => setPinned(e.target.checked)} />Show it in the sidebar</label>
        <p className="muted small">Saves the search, the filters and the order.</p>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary">Save</button></div>
      </form>
    </Modal>
  )
}

function BulkEdit({ ids, onClose, onDone }: { ids: number[]; onClose: () => void; onDone: () => void }) {
  const { toast } = useToast()
  const [addTags, setAddTags] = useState<Ref[]>([])
  const [remTags, setRemTags] = useState<Ref[]>([])
  const [addPerfs, setAddPerfs] = useState<Ref[]>([])
  const [studio, setStudio] = useState<Ref[]>([])
  const [studioMode, setStudioMode] = useState<'keep' | 'set' | 'clear'>('keep')
  const [organized, setOrganized] = useState<'' | 'yes' | 'no'>('')
  const [rating, setRating] = useState(-1)
  const [fav, setFav] = useState<'' | 'yes' | 'no'>('')
  const [regen, setRegen] = useState(false)
  const [identify, setIdentify] = useState(false)
  const [busy, setBusy] = useState(false)
  const body = useMemo(() => ({
    ids, add_tags: addTags.map(x => x.id), remove_tags: remTags.map(x => x.id), add_performers: addPerfs.map(x => x.id),
    studio_id: studioMode === 'set' && studio[0] ? studio[0].id : studioMode === 'clear' ? 0 : undefined,
    organized: organized ? organized === 'yes' : undefined, rating: rating >= 0 ? rating : undefined, favorite: fav ? fav === 'yes' : undefined,
    regenerate: regen, identify,
  }), [ids, addTags, remTags, addPerfs, studio, studioMode, organized, rating, fav, regen, identify])
  const run = async () => {
    setBusy(true)
    try { const r = await api.post<{ changed: number }>(`${V1}/scenes/bulk`, body); toast(`${r.changed} scenes changed${identify ? ' — looking them up in the background' : ''}`); onDone() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={`Edit ${ids.length} scenes`} onClose={onClose} footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={run} disabled={busy}>Apply</button></>}>
      <div className="grid" style={{ gap: 14 }}>
        <Field label="Add performers"><Picker kind="performers" value={addPerfs} onChange={setAddPerfs} /></Field>
        <div className="two-col tight grid">
          <Field label="Add tags"><Picker kind="tags" value={addTags} onChange={setAddTags} /></Field>
          <Field label="Remove tags"><Picker kind="tags" value={remTags} onChange={setRemTags} /></Field>
        </div>
        <Field label="Studio"><div className="stack"><Segmented size="sm" value={studioMode} onChange={setStudioMode} options={[{ value: 'keep', label: 'Keep' }, { value: 'set', label: 'Set' }, { value: 'clear', label: 'Remove' }]} />{studioMode === 'set' && <Picker kind="studios" single value={studio} onChange={setStudio} />}</div></Field>
        <div className="two-col tight grid">
          <Field label="Organized"><Segmented size="sm" value={organized} onChange={setOrganized} options={[{ value: '', label: 'Keep' }, { value: 'yes', label: 'Yes' }, { value: 'no', label: 'No' }]} /></Field>
          <Field label="Favourite (yours)"><Segmented size="sm" value={fav} onChange={setFav} options={[{ value: '', label: 'Keep' }, { value: 'yes', label: 'Yes' }, { value: 'no', label: 'No' }]} /></Field>
        </div>
        <Field label="Your rating"><div className="row"><Stars value={Math.max(rating, 0)} onChange={setRating} />{rating >= 0 && <button type="button" className="link-btn small" onClick={() => setRating(-1)}>keep as is</button>}</div></Field>
        <label className="check-row"><input type="checkbox" checked={identify} onChange={e => setIdentify(e.target.checked)} />Look them up on StashDB / ThePornDB (fills the ones with exactly one match)</label>
        <label className="check-row"><input type="checkbox" checked={regen} onChange={e => setRegen(e.target.checked)} />Make the pictures and previews again</label>
      </div>
    </Modal>
  )
}
