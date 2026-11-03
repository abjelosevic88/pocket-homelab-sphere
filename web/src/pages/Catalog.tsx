// Studios, tags and groups: lists, pages with their scenes, editing.
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, postFile, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { longDuration } from '../lib/sphere'
import type { Group, Marker, Ref, Scene, Studio, Tag } from '../lib/types'
import { Picker, SceneGrid, Stars, usePaged } from '../components/scene'
import { announceChange, useCrumb, useOnChange } from '../components/Layout'
import { Confirm, Empty, Field, Loading, MenuButton, Modal, errMsg, useAsync, useDebounce, useToast, type MenuItem } from '../components/ui'
import { Icon } from '../components/icons'
import { FollowButton, MissingScenes } from '../components/Discover'

// ---------- studios ----------

export function Studios() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const { toast } = useToast()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 200)
  const { data, reload } = useAsync(() => api.get<Studio[]>(`${V1}/studios?q=${encodeURIComponent(dq)}`), [dq])
  useOnChange(reload)
  const [creating, setCreating] = useState(params.get('new') === '1')
  const list = useMemo(() => (data || []).slice().sort((a, b) => b.scenes - a.scenes || a.name.localeCompare(b.name)), [data])
  return (
    <div>
      <div className="page-head">
        <div><h1>Studios</h1><p className="muted">{data?.length ?? 0} studios</p></div>
        <div className="row wrap">
          <button type="button" className="btn" onClick={async () => { const r = await api.post<{ changed: number }>(`${V1}/library/autotag`, { studios: true }); toast(`${r.changed} scenes got a studio from their file names`); announceChange() }}><Icon name="wand" size={15} />Find in file names</button>
          <button type="button" className="btn primary" onClick={() => setCreating(true)}><Icon name="plus" size={15} />New studio</button>
        </div>
      </div>
      <div className="toolbar"><div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search studios…" /></div></div>
      {data && !list.length && <Empty title="No studios yet" icon="building">Studios come from looking scenes up, from downloads (the channel), or are added by hand.</Empty>}
      <div className={`logo-grid ${settings?.discreet ? 'discreet' : ''}`}>
        {list.map(s => (
          <Link key={s.id} to={`/studios/${s.id}`} className="logo-card">
            <div className="logo-media">{s.image ? <img src={s.image} alt="" loading="lazy" /> : <span>{s.name}</span>}{s.favorite && <span className="scard-fav"><Icon name="heart" size={13} /></span>}</div>
            <div className="logo-name ellipsis">{s.name}</div>
            <div className="muted small">{s.scenes} scenes{s.parent ? ` · ${s.parent.name}` : ''}</div>
          </Link>
        ))}
      </div>
      {creating && <StudioEditor onClose={() => { setCreating(false); setParams({}, { replace: true }) }} onSaved={s => { setCreating(false); announceChange(); navigate(`/studios/${s.id}`) }} />}
    </div>
  )
}

export function StudioPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [s, setS] = useState<Studio | null>(null)
  const [edit, setEdit] = useState(false)
  const [del, setDel] = useState(false)
  useEffect(() => { api.get<Studio>(`${V1}/studios/${id}`).then(setS).catch(() => {}) }, [id])
  useCrumb(s?.name)
  const scenes = usePaged<Scene>(s ? `${V1}/scenes?studios=${s.id}&sort=date` : null)
  if (!s) return <Loading />
  const mark = async (patch: { favorite?: boolean; rating?: number }) => { await api.put(`${V1}/studios/${s.id}/mine`, patch); setS({ ...s, ...patch }) }
  const menu = (): MenuItem[] => [{ label: 'Edit', icon: 'edit', onSelect: () => setEdit(true) }, 'sep', { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setDel(true) }]
  return (
    <div className="entity-page">
      <div className="entity-hero">
        <div className="entity-logo">{s.image ? <img src={s.image} alt="" /> : <Icon name="building" size={32} />}</div>
        <div className="grow stack">
          <div className="row wrap"><div className="grow"><h1>{s.name}</h1><p className="muted">{s.scenes} scenes{s.parent && <> · part of <Link to={`/studios/${s.parent.id}`}>{s.parent.name}</Link></>}</p></div>
            <FollowButton kind="studio" id={s.id} />
            <button type="button" className={`btn icon-only ghost ${s.favorite ? 'fav-on' : ''}`} onClick={() => mark({ favorite: !s.favorite })} aria-label="Favourite"><Icon name="heart" size={18} /></button>
            <MenuButton label="More" items={menu} align="right"><Icon name="more" size={17} /></MenuButton></div>
          <Stars value={s.rating} onChange={v => mark({ rating: v })} size={18} />
          {s.url && <a href={s.url} target="_blank" rel="noreferrer noopener" className="small">{s.url.replace(/^https?:\/\//, '')}</a>}
          {s.details && <p className="scene-details">{s.details}</p>}
          {s.children && s.children.length > 0 && <div className="chips">{s.children.map(c => <Link key={c.id} to={`/studios/${c.id}`} className="ref-chip">{c.name}</Link>)}</div>}
        </div>
      </div>
      <div className="sec-head"><h2 className="sec-h"><Icon name="film" size={16} />Scenes</h2><Link to={`/scenes?studios=${s.id}`} className="small">Open as a list</Link></div>
      <SceneGrid items={scenes.items} queue={`studios=${s.id}&sort=date`} />
      {scenes.more && <div className="load-more"><button type="button" className="btn" onClick={scenes.next}>Show more</button></div>}
      <MissingScenes kind="studio" id={s.id} name={s.name} />
      {edit && <StudioEditor s={s} onClose={() => setEdit(false)} onSaved={x => { setEdit(false); setS(x); announceChange() }} />}
      {del && <Confirm title={`Delete ${s.name}?`} confirmLabel="Delete" message="The scenes stay; they just lose this studio." onCancel={() => setDel(false)} onConfirm={async () => { await api.del(`${V1}/studios/${s.id}`); announceChange(); navigate('/studios') }} />}
    </div>
  )
}

function StudioEditor({ s, onClose, onSaved }: { s?: Studio; onClose: () => void; onSaved: (s: Studio) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: s?.name || '', aliases: s?.aliases || '', url: s?.url || '', details: s?.details || '' })
  const [parent, setParent] = useState<Ref[]>(s?.parent ? [s.parent] : [])
  const [imageURL, setImageURL] = useState('')
  const save = async () => {
    try {
      const body = { ...f, parent_id: parent[0]?.id || 0, image_url: imageURL }
      onSaved(s ? await api.put<Studio>(`${V1}/studios/${s.id}`, body) : await api.post<Studio>(`${V1}/studios`, body)); toast('Saved')
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const upload = async (file: File) => { if (!s) return; try { await postFile(`${V1}/studios/${s.id}/image`, file); onSaved(await api.get<Studio>(`${V1}/studios/${s.id}`)) } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <Modal title={s ? `Edit ${s.name}` : 'New studio'} onClose={onClose} footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={save} disabled={!f.name}>Save</button></>}>
      <div className="grid" style={{ gap: 12 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus /></Field>
        <Field label="Other names" help="Comma between them; used to find the studio in file names."><input value={f.aliases} onChange={e => setF({ ...f, aliases: e.target.value })} /></Field>
        <Field label="Part of (network)"><Picker kind="studios" single value={parent} onChange={setParent} placeholder="No parent studio" /></Field>
        <Field label="Website"><input value={f.url} onChange={e => setF({ ...f, url: e.target.value })} placeholder="https://…" /></Field>
        <Field label="About"><textarea rows={3} value={f.details} onChange={e => setF({ ...f, details: e.target.value })} /></Field>
        <div className="row wrap"><Field label="Logo from a link"><input value={imageURL} onChange={e => setImageURL(e.target.value)} placeholder="https://…" /></Field>
          {s && <label className="btn sm" style={{ alignSelf: 'flex-end' }}><Icon name="upload" size={14} />Upload…<input type="file" accept="image/*" hidden onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) upload(file) }} /></label>}</div>
      </div>
    </Modal>
  )
}

// ---------- tags ----------

export function Tags() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 200)
  const [sort, setSort] = useState<'name' | 'scenes'>('scenes')
  const { data, reload } = useAsync(() => api.get<Tag[]>(`${V1}/tags?q=${encodeURIComponent(dq)}`), [dq])
  useOnChange(reload)
  const [creating, setCreating] = useState(params.get('new') === '1')
  const list = useMemo(() => (data || []).slice().sort((a, b) => sort === 'name' ? a.name.localeCompare(b.name) : (b.scenes + b.markers) - (a.scenes + a.markers) || a.name.localeCompare(b.name)), [data, sort])
  return (
    <div>
      <div className="page-head">
        <div><h1>Tags</h1><p className="muted">{data?.length ?? 0} tags</p></div>
        <div className="row wrap">
          <button type="button" className="btn" onClick={async () => { const r = await api.post<{ changed: number }>(`${V1}/library/autotag`, { tags: true }); toast(`${r.changed} scenes got tags from their file names`); announceChange() }}><Icon name="wand" size={15} />Find in file names</button>
          <button type="button" className="btn primary" onClick={() => setCreating(true)}><Icon name="plus" size={15} />New tag</button>
        </div>
      </div>
      <div className="toolbar">
        <div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search tags…" /></div>
        <button type="button" className={`btn ${sort === 'scenes' ? 'active' : ''}`} onClick={() => setSort(sort === 'scenes' ? 'name' : 'scenes')}><Icon name="sliders" size={15} />{sort === 'scenes' ? 'Most used first' : 'A to Z'}</button>
      </div>
      {data && !list.length && <Empty title="No tags yet" icon="tag">Tags come from looking scenes up and from downloads, or are added while editing.</Empty>}
      <div className="tag-cloud">
        {list.map(t => (
          <Link key={t.id} to={`/tags/${t.id}`} className={`tag-item ${t.favorite ? 'fav' : ''}`}>
            <Icon name="tag" size={13} /><span className="ellipsis">{t.name}</span><span className="muted small tnum">{t.scenes}{t.markers ? ` · ${t.markers}m` : ''}</span>
          </Link>
        ))}
      </div>
      {creating && <TagEditor onClose={() => { setCreating(false); setParams({}, { replace: true }) }} onSaved={t => { setCreating(false); announceChange(); navigate(`/tags/${t.id}`) }} />}
    </div>
  )
}

export function TagPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [t, setT] = useState<Tag | null>(null)
  const [edit, setEdit] = useState(false)
  const [merge, setMerge] = useState(false)
  const [del, setDel] = useState(false)
  const [markers, setMarkers] = useState<Marker[]>([])
  useEffect(() => { api.get<Tag>(`${V1}/tags/${id}`).then(setT).catch(() => {}); api.get<{ items: Marker[] }>(`${V1}/markers?tag=${id}&per=60`).then(r => setMarkers(r.items)).catch(() => {}) }, [id])
  useCrumb(t?.name)
  const scenes = usePaged<Scene>(t ? `${V1}/scenes?tags=${t.id}&sort=date` : null)
  if (!t) return <Loading />
  const menu = (): MenuItem[] => [{ label: 'Edit', icon: 'edit', onSelect: () => setEdit(true) }, { label: 'Merge other tags into this one', icon: 'layers', onSelect: () => setMerge(true) }, 'sep', { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setDel(true) }]
  return (
    <div className="entity-page">
      <div className="page-head">
        <div><h1><Icon name="tag" size={20} /> {t.name}</h1><p className="muted">{t.scenes} scenes · {t.markers} markers{t.parent && <> · under <Link to={`/tags/${t.parent.id}`}>{t.parent.name}</Link></>}{t.aliases && ` · also “${t.aliases}”`}</p></div>
        <div className="row">
          <button type="button" className={`btn icon-only ghost ${t.favorite ? 'fav-on' : ''}`} onClick={async () => { await api.put(`${V1}/tags/${t.id}/mine`, { favorite: !t.favorite }); setT({ ...t, favorite: !t.favorite }) }} aria-label="Favourite"><Icon name="heart" size={18} /></button>
          <MenuButton label="More" items={menu} align="right"><Icon name="more" size={17} /></MenuButton>
        </div>
      </div>
      {t.description && <p className="scene-details">{t.description}</p>}
      {t.children && t.children.length > 0 && <div className="chips mb">{t.children.map(c => <Link key={c.id} to={`/tags/${c.id}`} className="ref-chip">{c.name}</Link>)}</div>}
      {markers.length > 0 && <>
        <div className="sec-head"><h2 className="sec-h"><Icon name="marker" size={16} />Markers</h2><Link to={`/markers?tag=${t.id}`} className="small">See all</Link></div>
        <div className="marker-grid small-grid">{markers.slice(0, 12).map(m => <Link key={m.id} to={`/scenes/${m.scene_id}?t=${Math.floor(m.seconds)}`} className="mcard"><img src={m.screenshot} alt="" loading="lazy" /><span className="mcard-label ellipsis">{m.scene_name}</span></Link>)}</div>
      </>}
      <div className="sec-head"><h2 className="sec-h"><Icon name="film" size={16} />Scenes</h2><Link to={`/scenes?tags=${t.id}`} className="small">Open as a list</Link></div>
      <SceneGrid items={scenes.items} queue={`tags=${t.id}&sort=date`} />
      {scenes.more && <div className="load-more"><button type="button" className="btn" onClick={scenes.next}>Show more</button></div>}
      {edit && <TagEditor t={t} onClose={() => setEdit(false)} onSaved={x => { setEdit(false); setT(x); announceChange() }} />}
      {merge && <TagMerge target={t} onClose={() => setMerge(false)} onDone={x => { setMerge(false); setT(x); scenes.reload(); announceChange(); toast('Merged') }} />}
      {del && <Confirm title={`Delete the tag “${t.name}”?`} confirmLabel="Delete" message="Scenes and markers lose it; nothing else changes." onCancel={() => setDel(false)} onConfirm={async () => { await api.del(`${V1}/tags/${t.id}`); announceChange(); navigate('/tags') }} />}
    </div>
  )
}

function TagEditor({ t, onClose, onSaved }: { t?: Tag; onClose: () => void; onSaved: (t: Tag) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: t?.name || '', aliases: t?.aliases || '', description: t?.description || '' })
  const [parent, setParent] = useState<Ref[]>(t?.parent ? [t.parent] : [])
  const save = async () => { try { const body = { ...f, parent_id: parent[0]?.id || 0 }; onSaved(t ? await api.put<Tag>(`${V1}/tags/${t.id}`, body) : await api.post<Tag>(`${V1}/tags`, body)); toast('Saved') } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <Modal title={t ? `Edit “${t.name}”` : 'New tag'} onClose={onClose} size="sm" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={save} disabled={!f.name}>Save</button></>}>
      <div className="grid" style={{ gap: 12 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus /></Field>
        <Field label="Other names" help="Comma between them."><input value={f.aliases} onChange={e => setF({ ...f, aliases: e.target.value })} /></Field>
        <Field label="Under" help="Scenes of this tag also show under the one above it."><Picker kind="tags" single value={parent} onChange={setParent} placeholder="Nothing" /></Field>
        <Field label="Description"><textarea rows={2} value={f.description} onChange={e => setF({ ...f, description: e.target.value })} /></Field>
      </div>
    </Modal>
  )
}

function TagMerge({ target, onClose, onDone }: { target: Tag; onClose: () => void; onDone: (t: Tag) => void }) {
  const { toast } = useToast()
  const [src, setSrc] = useState<Ref[]>([])
  return (
    <Modal title={`Merge into “${target.name}”`} onClose={onClose} footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" disabled={!src.length} onClick={async () => { try { onDone(await api.post<Tag>(`${V1}/tags/${target.id}/merge`, { sources: src.map(s => s.id) })) } catch (e) { toast(errMsg(e), 'err') } }}>Merge</button></>}>
      <div className="stack"><p className="muted">Their scenes, markers and galleries move to “{target.name}”; their names become other names of it.</p><Picker kind="tags" value={src.filter(s => s.id !== target.id)} onChange={setSrc} /></div>
    </Modal>
  )
}

// ---------- groups ----------

export function Groups() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 200)
  const { data, reload } = useAsync(() => api.get<Group[]>(`${V1}/groups?q=${encodeURIComponent(dq)}`), [dq])
  useOnChange(reload)
  const [creating, setCreating] = useState(params.get('new') === '1')
  return (
    <div>
      <div className="page-head">
        <div><h1>Groups</h1><p className="muted">Movies and series: scenes that belong together, in order.</p></div>
        <button type="button" className="btn primary" onClick={() => setCreating(true)}><Icon name="plus" size={15} />New group</button>
      </div>
      <div className="toolbar"><div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search groups…" /></div></div>
      {data && !data.length && <Empty title="No groups yet" icon="layers">Make one, then add scenes to it from a scene's Edit tab.</Empty>}
      <div className={`pgrid ${settings?.discreet ? 'discreet' : ''}`}>
        {data?.map(g => (
          <Link key={g.id} to={`/groups/${g.id}`} className="pcard">
            <div className="pcard-media">{g.front ? <img src={g.front} alt="" loading="lazy" /> : <div className="pcard-none"><Icon name="layers" size={26} /></div>}</div>
            <div className="pcard-body"><div className="pcard-name ellipsis">{g.name}</div><div className="scard-meta"><span>{g.scenes} scenes</span>{g.duration > 0 && <span>{longDuration(g.duration)}</span>}{g.date && <span>{g.date.slice(0, 4)}</span>}</div></div>
          </Link>
        ))}
      </div>
      {creating && <GroupEditor onClose={() => { setCreating(false); setParams({}, { replace: true }) }} onSaved={g => { setCreating(false); announceChange(); navigate(`/groups/${g.id}`) }} />}
    </div>
  )
}

export function GroupPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [g, setG] = useState<Group | null>(null)
  const [edit, setEdit] = useState(false)
  const [del, setDel] = useState(false)
  const [scenes, setScenes] = useState<Scene[]>([])
  const load = () => {
    api.get<Group>(`${V1}/groups/${id}`).then(setG).catch(() => {})
    api.get<number[]>(`${V1}/scenes/ids?group=${id}`).then(ids => ids.length ? api.get<{ items: Scene[] }>(`${V1}/scenes?ids=${ids.join(',')}&per=500&hidden=1`).then(r => { const by = new Map(r.items.map(s => [s.id, s])); setScenes(ids.map(i => by.get(i)).filter(Boolean) as Scene[]) }) : setScenes([])).catch(() => {})
  }
  useEffect(load, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  useCrumb(g?.name)
  if (!g) return <Loading />
  const setImage = async (file: File, back: boolean) => { try { await postFile(`${V1}/groups/${g.id}/image?side=${back ? 'back' : 'front'}`, file); load() } catch (e) { toast(errMsg(e), 'err') } }
  const menu = (): MenuItem[] => [{ label: 'Edit', icon: 'edit', onSelect: () => setEdit(true) }, 'sep', { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setDel(true) }]
  return (
    <div className="entity-page">
      <div className="entity-hero">
        <label className="entity-photo" title="Change the front cover">{g.front ? <img src={g.front} alt="" /> : <Icon name="layers" size={32} />}<input type="file" accept="image/*" hidden onChange={e => { const f = e.target.files?.[0]; e.target.value = ''; if (f) setImage(f, false) }} /></label>
        <div className="grow stack">
          <div className="row wrap"><div className="grow"><h1>{g.name}</h1><p className="muted">{g.scenes} scenes{g.duration > 0 && ` · ${longDuration(g.duration)}`}{g.studio && <> · <Link to={`/studios/${g.studio.id}`}>{g.studio.name}</Link></>}{g.date && ` · ${g.date}`}</p></div>
            <button type="button" className="btn" onClick={() => scenes[0] && navigate(`/scenes/${scenes[0].id}?groups=${g.id}&sort=path`)} disabled={!scenes.length}><Icon name="play" size={15} />Play</button>
            <MenuButton label="More" items={menu} align="right"><Icon name="more" size={17} /></MenuButton></div>
          <Stars value={g.rating} onChange={async v => { await api.put(`${V1}/groups/${g.id}/mine`, { rating: v }); setG({ ...g, rating: v }) }} size={18} />
          {g.director && <p className="small">Directed by {g.director}</p>}
          {g.synopsis && <p className="scene-details">{g.synopsis}</p>}
        </div>
      </div>
      <SceneGrid items={scenes} />
      {edit && <GroupEditor g={g} onClose={() => setEdit(false)} onSaved={x => { setEdit(false); setG(x); load(); announceChange() }} />}
      {del && <Confirm title={`Delete “${g.name}”?`} confirmLabel="Delete" message="The scenes stay." onCancel={() => setDel(false)} onConfirm={async () => { await api.del(`${V1}/groups/${g.id}`); announceChange(); navigate('/groups') }} />}
    </div>
  )
}

function GroupEditor({ g, onClose, onSaved }: { g?: Group; onClose: () => void; onSaved: (g: Group) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: g?.name || '', aliases: g?.aliases || '', date: g?.date || '', director: g?.director || '', synopsis: g?.synopsis || '' })
  const [studio, setStudio] = useState<Ref[]>(g?.studio ? [g.studio] : [])
  const save = async () => { try { const body = { ...f, studio_id: studio[0]?.id || 0 }; onSaved(g ? await api.put<Group>(`${V1}/groups/${g.id}`, body) : await api.post<Group>(`${V1}/groups`, body)); toast('Saved') } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <Modal title={g ? `Edit “${g.name}”` : 'New group'} onClose={onClose} footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={save} disabled={!f.name}>Save</button></>}>
      <div className="grid" style={{ gap: 12 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus /></Field>
        <div className="two-col tight grid"><Field label="Date"><input value={f.date} onChange={e => setF({ ...f, date: e.target.value })} placeholder="2024-05-31" /></Field><Field label="Director"><input value={f.director} onChange={e => setF({ ...f, director: e.target.value })} /></Field></div>
        <Field label="Studio"><Picker kind="studios" single value={studio} onChange={setStudio} /></Field>
        <Field label="Other names"><input value={f.aliases} onChange={e => setF({ ...f, aliases: e.target.value })} /></Field>
        <Field label="Synopsis"><textarea rows={3} value={f.synopsis} onChange={e => setF({ ...f, synopsis: e.target.value })} /></Field>
      </div>
    </Modal>
  )
}
