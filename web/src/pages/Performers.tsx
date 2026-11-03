// Performers: the list, one performer's page with their scenes, editing and lookups.
import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, postFile, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { genderLabel } from '../lib/sphere'
import { fmtDate } from '../lib/format'
import type { LookupPerformer, Performer, Ref, Scene } from '../lib/types'
import { PerformerCard, Picker, SceneGrid, Stars, initialsOf, usePaged } from '../components/scene'
import { announceChange, useCrumb, useOnChange } from '../components/Layout'
import { Select } from '../components/Select'
import { Confirm, Empty, Field, Loading, MenuButton, Modal, Segmented, errMsg, useDebounce, useToast, type MenuItem } from '../components/ui'
import { Icon } from '../components/icons'

const PSORTS = [{ value: 'name', label: 'Name' }, { value: 'scenes', label: 'Most scenes' }, { value: 'rating', label: 'Your rating' }, { value: 'o', label: 'O count' }, { value: 'age', label: 'Age' }, { value: 'added', label: 'Added' }, { value: 'random', label: 'Random' }]

export default function Performers() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const [q, setQ] = useState(params.get('q') || '')
  const dq = useDebounce(q, 250)
  const sort = params.get('sort') || 'name'
  const fav = params.get('favorite') === '1'
  const gender = params.get('gender') || ''
  const dir = sort === 'name' || sort === 'age' ? 'asc' : 'desc'
  const { items, total, loading, more, next, reload } = usePaged<Performer>(`${V1}/performers?q=${encodeURIComponent(dq)}&sort=${sort}&dir=${dir}${fav ? '&favorite=1' : ''}${gender ? `&gender=${gender}` : ''}`, 60)
  useOnChange(reload)
  const set = (k: string, v: string) => { const p = new URLSearchParams(params); if (v) p.set(k, v); else p.delete(k); setParams(p, { replace: true }) }
  const [creating, setCreating] = useState(params.get('new') === '1')
  return (
    <div>
      <div className="page-head">
        <div><h1>Performers</h1><p className="muted tnum">{total.toLocaleString()} {total === 1 ? 'person' : 'people'}</p></div>
        <div className="row wrap">
          <button type="button" className="btn" onClick={async () => { const r = await api.post<{ changed: number }>(`${V1}/library/autotag`, { performers: true }); announceChange(); reload(); alert(`${r.changed} scenes got performers from their file names.`) }} title="Find each performer's name (or alias) in file names"><Icon name="wand" size={15} />Find in file names</button>
          <button type="button" className="btn primary" onClick={() => setCreating(true)}><Icon name="plus" size={15} />New performer</button>
        </div>
      </div>
      <div className="toolbar">
        <div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search names and aliases…" aria-label="Search" /></div>
        <Select ariaLabel="Gender" value={gender} onChange={v => set('gender', v)} options={[{ value: '', label: 'Everyone' }, { value: 'female', label: 'Women' }, { value: 'male', label: 'Men' }, { value: 'trans_female', label: 'Trans women' }, { value: 'trans_male', label: 'Trans men' }, { value: 'non_binary', label: 'Non-binary' }]} />
        <Select ariaLabel="Sort" value={sort} onChange={v => set('sort', v)} options={PSORTS} />
        <button type="button" className={`btn ${fav ? 'active' : ''}`} onClick={() => set('favorite', fav ? '' : '1')}><Icon name="heart" size={15} />Favourites</button>
      </div>
      {!loading && !items.length && <Empty title={q || fav ? 'Nobody matches' : 'No performers yet'} icon="user">They are added when you edit or look up scenes, or with “New performer”.</Empty>}
      <div className={`pgrid size-${settings?.grid_size || 'm'}`}>{items.map(p => <PerformerCard key={p.id} p={p} />)}</div>
      {loading && <Loading />}
      {more && !loading && <div className="load-more"><button type="button" className="btn" onClick={next}>Show more</button></div>}
      {creating && <PerformerEditor onClose={() => { setCreating(false); set('new', '') }} onSaved={p => { setCreating(false); announceChange(); navigate(`/performers/${p.id}`) }} />}
    </div>
  )
}

export function PerformerPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const { toast } = useToast()
  const [p, setP] = useState<Performer | null>(null)
  const [edit, setEdit] = useState(false)
  const [lookup, setLookup] = useState(false)
  const [merge, setMerge] = useState(false)
  const [del, setDel] = useState(false)
  const load = () => api.get<Performer>(`${V1}/performers/${id}`).then(setP).catch(e => toast(errMsg(e), 'err'))
  useEffect(() => { load() }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  useCrumb(p?.name)
  const scenes = usePaged<Scene>(p ? `${V1}/scenes?performers=${p.id}&sort=date` : null, 60)
  if (!p) return <Loading />
  const mark = async (patch: { favorite?: boolean; rating?: number }) => { await api.put(`${V1}/performers/${p.id}/mine`, patch); setP({ ...p, ...patch }) }
  const facts: [string, string][] = ([
    ['Aliases', p.aliases], ['Gender', genderLabel(p.gender)], ['Born', p.birthdate ? `${fmtDate(p.birthdate)}${p.age ? ` (${p.age})` : ''}` : ''], ['Died', p.death_date ? fmtDate(p.death_date) : ''],
    ['From', p.country], ['Ethnicity', p.ethnicity], ['Hair', p.hair_color], ['Eyes', p.eye_color], ['Height', p.height_cm ? `${p.height_cm} cm` : ''], ['Weight', p.weight_kg ? `${p.weight_kg} kg` : ''],
    ['Measurements', p.measurements], ['Breasts', p.fake_tits], ['Tattoos', p.tattoos], ['Piercings', p.piercings], ['Career', p.career],
  ] as [string, string][]).filter(([, v]) => v)
  const menu = (): MenuItem[] => [
    { label: 'Edit', icon: 'edit', onSelect: () => setEdit(true) },
    { label: 'Look up on StashDB / TPDB', icon: 'search', onSelect: () => setLookup(true) },
    { label: 'Merge others into this one', icon: 'users', onSelect: () => setMerge(true) },
    { label: 'Find in file names', icon: 'wand', onSelect: async () => { const r = await api.post<{ changed: number }>(`${V1}/library/autotag`, { performers: true }); toast(`${r.changed} scenes changed`); scenes.reload(); load() } },
    'sep',
    { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setDel(true) },
  ]
  return (
    <div className="entity-page">
      <div className={`entity-hero ${settings?.discreet ? 'discreet' : ''}`}>
        <div className="entity-photo">{p.image ? <img src={p.image} alt="" /> : <span>{initialsOf(p.name)}</span>}</div>
        <div className="grow stack">
          <div className="row wrap" style={{ alignItems: 'flex-start' }}>
            <div className="grow"><h1>{p.name}{p.disambiguation && <span className="muted"> ({p.disambiguation})</span>}</h1>
              <p className="muted">{p.scenes} scenes{p.galleries ? ` · ${p.galleries} galleries` : ''}{p.o_count ? ` · ${p.o_count} O's` : ''}</p></div>
            <button type="button" className={`btn icon-only ghost ${p.favorite ? 'fav-on' : ''}`} onClick={() => mark({ favorite: !p.favorite })} aria-label="Favourite" title="Favourite"><Icon name="heart" size={18} /></button>
            <MenuButton label="More" items={menu} align="right" width={260}><Icon name="more" size={17} /></MenuButton>
          </div>
          <Stars value={p.rating} onChange={v => mark({ rating: v })} size={18} />
          {facts.length > 0 && <dl className="kv">{facts.map(([k, v]) => <div key={k} style={{ display: 'contents' }}><dt>{k}</dt><dd>{v}</dd></div>)}</dl>}
          {p.details && <p className="scene-details">{p.details}</p>}
          {p.urls.length > 0 && <div className="row wrap small">{p.urls.map(u => <a key={u} href={u} target="_blank" rel="noreferrer noopener">{u.replace(/^https?:\/\/(www\.)?/, '').split('/')[0]}</a>)}</div>}
        </div>
      </div>
      <div className="sec-head"><h2 className="sec-h"><Icon name="film" size={16} />Scenes</h2><Link to={`/scenes?performers=${p.id}`} className="small">Open as a list</Link></div>
      {!scenes.loading && !scenes.items.length && <p className="muted">No scenes yet.</p>}
      <SceneGrid items={scenes.items} queue={`performers=${p.id}&sort=date`} />
      {scenes.more && <div className="load-more"><button type="button" className="btn" onClick={scenes.next}>Show more</button></div>}
      {edit && <PerformerEditor p={p} onClose={() => setEdit(false)} onSaved={x => { setEdit(false); setP(x); announceChange() }} />}
      {lookup && <PerformerLookup p={p} onClose={() => setLookup(false)} onApplied={x => { setLookup(false); setP(x) }} />}
      {merge && <MergeModal target={p} onClose={() => setMerge(false)} onDone={x => { setMerge(false); setP(x); scenes.reload(); announceChange() }} />}
      {del && <Confirm title={`Delete ${p.name}?`} confirmLabel="Delete" message="The scenes stay; they just lose this performer." onCancel={() => setDel(false)} onConfirm={async () => { await api.del(`${V1}/performers/${p.id}`); announceChange(); navigate('/performers') }} />}
    </div>
  )
}

export function PerformerEditor({ p, onClose, onSaved }: { p?: Performer; onClose: () => void; onSaved: (p: Performer) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState<Partial<Performer>>(p ? { ...p } : { name: '', gender: 'female', urls: [] })
  const [urls, setUrls] = useState((p?.urls || []).join('\n'))
  const [imageURL, setImageURL] = useState('')
  const [busy, setBusy] = useState(false)
  const txt = (k: keyof Performer, label: string, ph = '') => <Field label={label}><input value={String(f[k] ?? '')} onChange={e => setF({ ...f, [k]: e.target.value })} placeholder={ph} /></Field>
  const save = async () => {
    setBusy(true)
    try {
      const body = { ...f, urls: urls.split('\n').map(s => s.trim()).filter(Boolean), height_cm: Number(f.height_cm) || 0, weight_kg: Number(f.weight_kg) || 0, image_url: imageURL }
      const out = p ? await api.put<Performer>(`${V1}/performers/${p.id}`, body) : await api.post<Performer>(`${V1}/performers`, body)
      toast('Saved'); onSaved(out)
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const upload = async (file: File) => { if (!p) return; try { await postFile(`${V1}/performers/${p.id}/image`, file); toast('Picture saved'); onSaved(await api.get<Performer>(`${V1}/performers/${p.id}`)) } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <Modal title={p ? `Edit ${p.name}` : 'New performer'} onClose={onClose} size="lg" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={save} disabled={busy || !f.name}>Save</button></>}>
      <div className="grid" style={{ gap: 12 }}>
        <div className="two-col tight grid">{txt('name', 'Name')}{txt('disambiguation', 'To tell apart from others with that name', 'e.g. the country')}</div>
        {txt('aliases', 'Other names', 'comma between them')}
        <div className="three-col tight grid">
          <Field label="Gender"><Select ariaLabel="Gender" value={f.gender || ''} onChange={v => setF({ ...f, gender: v })} options={[{ value: '', label: '—' }, { value: 'female', label: 'Female' }, { value: 'male', label: 'Male' }, { value: 'trans_female', label: 'Trans woman' }, { value: 'trans_male', label: 'Trans man' }, { value: 'non_binary', label: 'Non-binary' }, { value: 'intersex', label: 'Intersex' }]} /></Field>
          {txt('birthdate', 'Born', '1995-04-30')}{txt('country', 'From')}
        </div>
        <div className="three-col tight grid">{txt('ethnicity', 'Ethnicity')}{txt('hair_color', 'Hair')}{txt('eye_color', 'Eyes')}</div>
        <div className="three-col tight grid">{txt('height_cm', 'Height (cm)')}{txt('weight_kg', 'Weight (kg)')}{txt('measurements', 'Measurements', '34C-24-34')}</div>
        <div className="three-col tight grid">{txt('fake_tits', 'Breasts', 'Natural / Augmented')}{txt('career', 'Career', '2014 - 2022')}{txt('death_date', 'Died')}</div>
        <div className="two-col tight grid">{txt('tattoos', 'Tattoos')}{txt('piercings', 'Piercings')}</div>
        <Field label="About"><textarea rows={3} value={f.details || ''} onChange={e => setF({ ...f, details: e.target.value })} /></Field>
        <Field label="Links" help="One per line."><textarea rows={2} value={urls} onChange={e => setUrls(e.target.value)} /></Field>
        <div className="row wrap">
          <Field label="Picture from a link"><input value={imageURL} onChange={e => setImageURL(e.target.value)} placeholder="https://…" /></Field>
          {p && <label className="btn sm" style={{ alignSelf: 'flex-end' }}><Icon name="upload" size={14} />Upload…<input type="file" accept="image/*" hidden onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) upload(file) }} /></label>}
        </div>
      </div>
    </Modal>
  )
}

function PerformerLookup({ p, onClose, onApplied }: { p: Performer; onClose: () => void; onApplied: (p: Performer) => void }) {
  const { toast } = useToast()
  const [q, setQ] = useState(p.name)
  const [res, setRes] = useState<{ results: LookupPerformer[]; problems: string[] | null } | null>(null)
  const [busy, setBusy] = useState(false)
  const [overwrite, setOverwrite] = useState<'empty' | 'all'>('empty')
  const search = async () => { setBusy(true); try { setRes(await api.get(`${V1}/performers/lookup?q=${encodeURIComponent(q)}`)) } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) } }
  useEffect(() => { search() }, []) // eslint-disable-line react-hooks/exhaustive-deps
  const apply = async (r: LookupPerformer) => {
    try { onApplied(await api.post<Performer>(`${V1}/performers/${p.id}/apply`, { result: r, overwrite: overwrite === 'all', image: overwrite === 'all' })); toast('Filled in') } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <Modal title={`Look up ${p.name}`} onClose={onClose} size="lg">
      <div className="stack">
        <form className="row" onSubmit={e => { e.preventDefault(); search() }}><input value={q} onChange={e => setQ(e.target.value)} /><button className="btn" disabled={busy}>{busy ? 'Looking…' : 'Look up'}</button></form>
        <Segmented size="sm" value={overwrite} onChange={setOverwrite} options={[{ value: 'empty', label: 'Fill only what is empty' }, { value: 'all', label: 'Replace everything' }]} />
        {res?.problems?.map(x => <div key={x} className="callout warn"><Icon name="info" size={15} /><span>{x}</span></div>)}
        {res && !res.results.length && !busy && <p className="muted">Nobody found.</p>}
        <div className="lookup-list">
          {res?.results.map(r => (
            <button key={r.source + r.id + r.name} type="button" className="lookup-item" onClick={() => apply(r)}>
              {r.image ? <img src={r.image} alt="" className="tall" loading="lazy" referrerPolicy="no-referrer" /> : <span className="lookup-none"><Icon name="user" size={18} /></span>}
              <span className="grow">
                <span className="lookup-title">{r.name}{r.disambiguation && <span className="muted"> ({r.disambiguation})</span>}</span>
                <span className="muted small">{[genderLabel(r.gender), r.birthdate, r.country].filter(Boolean).join(' · ')}</span>
                {r.aliases && r.aliases.length > 0 && <span className="small ellipsis">aka {r.aliases.join(', ')}</span>}
                <span><span className="badge sm">{r.source === 'stashdb' ? 'StashDB' : 'TPDB'}</span></span>
              </span>
            </button>
          ))}
        </div>
      </div>
    </Modal>
  )
}

function MergeModal({ target, onClose, onDone }: { target: Performer; onClose: () => void; onDone: (p: Performer) => void }) {
  const { toast } = useToast()
  const [src, setSrc] = useState<Ref[]>([])
  const run = async () => { try { onDone(await api.post<Performer>(`${V1}/performers/${target.id}/merge`, { sources: src.map(s => s.id) })); toast('Merged') } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <Modal title={`Merge into ${target.name}`} onClose={onClose} footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={run} disabled={!src.length}>Merge</button></>}>
      <div className="stack">
        <p className="muted">Their scenes and galleries move to {target.name}; their names become aliases; they are then deleted.</p>
        <Picker kind="performers" value={src.filter(s => s.id !== target.id)} onChange={setSrc} placeholder="Who is the same person?" />
      </div>
    </Modal>
  )
}
