// One scene: the player, and beside it its facts, the queue, markers, the
// file, your history, editing and looking it up.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, postFile, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { clock, quality } from '../lib/sphere'
import { fmtBytes, fmtDate, fmtDateTime } from '../lib/format'
import type { LookupScene, Marker, Ref, Scene, SceneDetail, SceneHistory } from '../lib/types'
import { Player, Scrubber, type PlayerHandle } from '../components/Player'
import { Picker, RefChips, Stars } from '../components/scene'
import { announceChange, useCrumb } from '../components/Layout'
import { Select } from '../components/Select'
import { Confirm, Empty, Field, Loading, MenuButton, Segmented, Switch, Tabs, copyText, errMsg, useIsMobile, useToast, type MenuItem } from '../components/ui'
import { Icon } from '../components/icons'

type Tab = 'details' | 'queue' | 'markers' | 'file' | 'history' | 'edit' | 'identify'

export default function ScenePage() {
  const { id } = useParams()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const [d, setD] = useState<SceneDetail | null>(null)
  const [error, setError] = useState('')
  const [tab, setTab] = useState<Tab>('details')
  const [markers, setMarkers] = useState<Marker[]>([])
  const [queue, setQueue] = useState<number[]>([])
  const [time, setTime] = useState(0)
  const [panel, setPanel] = useState(() => localStorage.getItem('psp-scene-panel') !== '0')
  const [cont, setCont] = useState(() => settings?.continue_queue ?? false)
  const player = useRef<PlayerHandle>(null)
  const sceneId = Number(id)
  const qs = params.toString()

  const load = useCallback(async () => {
    try { setD(await api.get<SceneDetail>(`${V1}/scenes/${sceneId}`)); setError('') } catch (e) { setError(errMsg(e)) }
  }, [sceneId])
  const loadMarkers = useCallback(() => { api.get<{ items: Marker[] }>(`${V1}/scenes/${sceneId}/markers`).then(r => setMarkers(r.items)).catch(() => {}) }, [sceneId])
  useEffect(() => { setD(null); load(); loadMarkers() }, [load, loadMarkers])
  useEffect(() => { localStorage.setItem('psp-scene-panel', panel ? '1' : '0') }, [panel])
  // the list this scene was opened from is the queue
  useEffect(() => {
    if (!qs) { setQueue([]); return }
    api.get<number[]>(`${V1}/scenes/ids?${qs}`).then(setQueue).catch(() => setQueue([]))
  }, [qs])
  useCrumb(d?.scene.name)

  const at = queue.indexOf(sceneId)
  const go = (sid: number | undefined) => { if (sid) navigate(`/scenes/${sid}${qs ? `?${qs}` : ''}`) }
  const prev = at > 0 ? queue[at - 1] : undefined
  const next = at >= 0 && at < queue.length - 1 ? queue[at + 1] : undefined

  const mine = async (patch: Partial<Scene['mine']>) => {
    if (!d) return
    try { const m = await api.put<Scene['mine']>(`${V1}/scenes/${d.scene.id}/mine`, patch); setD({ ...d, scene: { ...d.scene, mine: m } }) } catch (e) { toast(errMsg(e), 'err') }
  }
  const addO = async (remove = false) => {
    if (!d) return
    try {
      const r = remove ? await api.del<{ o_count: number }>(`${V1}/scenes/${d.scene.id}/o`) : await api.post<{ o_count: number }>(`${V1}/scenes/${d.scene.id}/o`)
      setD({ ...d, scene: { ...d.scene, mine: { ...d.scene.mine, o_count: r.o_count } } })
      if (!remove) toast(`O count ${r.o_count}`)
    } catch (e) { toast(errMsg(e), 'err') }
  }

  if (error) return <div className="callout bad"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!d) return <Loading />
  const sc = d.scene
  const startAt = settings?.resume_play !== false && sc.mine.resume_seconds > 5 && params.get('t') == null ? sc.mine.resume_seconds : Number(params.get('t') || 0)

  const tabs: { id: Tab; label: string }[] = [
    { id: 'details', label: 'Details' }, ...(queue.length > 1 ? [{ id: 'queue' as Tab, label: 'Queue' }] : []), { id: 'markers', label: `Markers${markers.length ? ` ${markers.length}` : ''}` },
    { id: 'file', label: 'File' }, { id: 'history', label: 'History' }, { id: 'edit', label: 'Edit' }, { id: 'identify', label: 'Look up' },
  ]
  const actions = (): MenuItem[] => [
    { label: 'Use this moment as the cover', icon: 'image', onSelect: async () => { try { await api.post(`${V1}/scenes/${sc.id}/cover`, { at: player.current?.time() || 0 }); toast('Cover set'); load() } catch (e) { toast(errMsg(e), 'err') } } },
    { label: 'Download the file', icon: 'download', onSelect: () => { window.location.href = `${V1}/scenes/${sc.id}/stream?download=1` } },
    { label: 'Copy a link to this moment', icon: 'link', onSelect: async () => { await copyText(`${window.location.origin}/scenes/${sc.id}?t=${Math.floor(player.current?.time() || 0)}`); toast('Link copied') } },
    ...(sc.source_url ? [{ label: 'Open the page it came from', icon: 'external', onSelect: () => window.open(sc.source_url, '_blank', 'noopener') }] : []),
    'sep',
    { label: 'Make pictures and preview again', icon: 'refresh', onSelect: async () => { await api.post(`${V1}/scenes/${sc.id}/regenerate`, { hash: true }); toast('Queued — this takes a minute or two'); } },
    { label: 'Read the file again', icon: 'search', onSelect: async () => { await api.post(`${V1}/scenes/${sc.id}/rescan`); toast('Read again'); load() } },
    { label: sc.mine.hidden ? 'Show in lists again' : 'Hide from lists', icon: sc.mine.hidden ? 'eye' : 'eye-off', onSelect: () => mine({ hidden: !sc.mine.hidden }) },
    'sep',
    { label: sc.trashed_at ? 'Restore from the trash' : 'Delete…', icon: sc.trashed_at ? 'undo' : 'trash', danger: !sc.trashed_at, onSelect: () => setTab(sc.trashed_at ? 'file' : 'file') },
  ]

  return (
    <div className={`scene-page ${panel && !mobile ? 'with-panel' : ''}`}>
      <div className="scene-main">
        <Player ref={player} key={sc.id} detail={d} settings={settings} markers={markers} startAt={startAt} autoPlay={settings?.auto_play !== false}
          hasPrev={!!prev} hasNext={!!next} onPrev={() => go(prev)} onNext={() => go(next)} onEnded={() => { if (cont && next) go(next) }}
          onO={() => addO()} onTime={setTime} />
        <Scrubber detail={d} time={time} markers={markers} onSeek={t => player.current?.seek(t)} />
        {!mobile && <button type="button" className="panel-toggle" onClick={() => setPanel(p => !p)} aria-label={panel ? 'Hide the details' : 'Show the details'} title={panel ? 'Hide the details' : 'Show the details'}><Icon name={panel ? 'chevron-right' : 'chevron-left'} size={14} /></button>}
      </div>
      {(panel || mobile) && (
        <aside className="scene-panel">
          <div className="scene-head">
            <h1 className="scene-title">{sc.name}</h1>
            <div className="scene-sub muted">
              {sc.studio && <Link to={`/studios/${sc.studio.id}`}>{sc.studio.name}</Link>}
              {sc.date && <span>{fmtDate(sc.date)}</span>}
              {sc.duration > 0 && <span className="tnum">{clock(sc.duration)}</span>}
              {quality(sc) && <span>{quality(sc)}{sc.fps ? ` · ${Math.round(sc.fps)} fps` : ''}</span>}
              {sc.is_vr && <span className="pill vr" title={sc.vr_label}>{sc.vr_label}</span>}
            </div>
            <div className="scene-acts">
              <Stars value={sc.mine.rating} onChange={v => mine({ rating: v })} size={18} />
              <span className="spacer" />
              <button type="button" className={`btn icon-only ghost ${sc.mine.favorite ? 'fav-on' : ''}`} onClick={() => mine({ favorite: !sc.mine.favorite })} aria-label={sc.mine.favorite ? 'Not a favourite' : 'Favourite'} title="Favourite"><Icon name="heart" size={17} /></button>
              <button type="button" className="btn ghost o-btn" onClick={() => addO()} onContextMenu={e => { e.preventDefault(); if (sc.mine.o_count) addO(true) }} title="Add an O (o) · right-click takes one back"><Icon name="droplet" size={16} /><span className="tnum">{sc.mine.o_count}</span></button>
              <span className="muted small tnum" title="Times watched"><Icon name="eye" size={14} />{sc.mine.play_count}</span>
              <MenuButton label="More" items={actions} align="right" width={270}><Icon name="more" size={17} /></MenuButton>
            </div>
          </div>
          <Tabs tabs={tabs} value={tab} onChange={setTab} />
          <div className="scene-tab">
            {tab === 'details' && <Details sc={sc} />}
            {tab === 'queue' && <Queue ids={queue} current={sc.id} qs={qs} cont={cont} setCont={setCont} />}
            {tab === 'markers' && <Markers sc={sc} markers={markers} time={time} onSeek={t => player.current?.seek(t)} reload={loadMarkers} />}
            {tab === 'file' && <FileTab d={d} reload={load} onGone={() => { announceChange(); navigate(qs ? `/scenes?${qs}` : '/scenes') }} />}
            {tab === 'history' && <History sc={sc} reload={load} />}
            {tab === 'edit' && <Edit sc={sc} time={time} onSaved={x => { setD(x); announceChange() }} />}
            {tab === 'identify' && <Identify sc={sc} onApplied={x => { setD(x); announceChange(); setTab('details') }} />}
          </div>
        </aside>
      )}
    </div>
  )
}

function Details({ sc }: { sc: Scene }) {
  return (
    <div className="stack">
      {sc.performers.length > 0 && <div><div className="sub-h">Performers</div><RefChips items={sc.performers} kind="performers" /></div>}
      {sc.details && <p className="scene-details">{sc.details}</p>}
      {sc.tags.length > 0 && <div><div className="sub-h">Tags</div><RefChips items={sc.tags} kind="tags" /></div>}
      {sc.groups && sc.groups.length > 0 && <div><div className="sub-h">Groups</div><RefChips items={sc.groups} kind="groups" /></div>}
      <dl className="kv">
        {sc.code && <><dt>Code</dt><dd>{sc.code}</dd></>}
        {sc.director && <><dt>Director</dt><dd>{sc.director}</dd></>}
        {sc.urls.length > 0 && <><dt>Links</dt><dd>{sc.urls.map(u => <div key={u}><a href={u} target="_blank" rel="noreferrer noopener" className="ellipsis">{u.replace(/^https?:\/\//, '')}</a></div>)}</dd></>}
        <dt>Added</dt><dd>{fmtDateTime(sc.created_at)}</dd>
        {sc.mine.last_played_at && <><dt>Last watched</dt><dd>{fmtDateTime(sc.mine.last_played_at)}</dd></>}
        {Object.keys(sc.stash_ids).length > 0 && <><dt>Matched on</dt><dd>{Object.keys(sc.stash_ids).map(k => k.replace(/^https?:\/\//, '').replace(/\/graphql$/, '')).join(', ')}</dd></>}
        <dt>Organized</dt><dd>{sc.organized ? 'Yes' : 'No'}</dd>
      </dl>
      {!sc.performers.length && !sc.tags.length && !sc.details && <p className="muted small">Nothing filled in yet — use <b>Look up</b> to find it on StashDB / ThePornDB, or <b>Edit</b>.</p>}
    </div>
  )
}

function Queue({ ids, current, qs, cont, setCont }: { ids: number[]; current: number; qs: string; cont: boolean; setCont: (v: boolean) => void }) {
  const at = ids.indexOf(current)
  const window_ = useMemo(() => ids.slice(Math.max(0, at - 5), at + 25), [ids, at])
  const [scenes, setScenes] = useState<Scene[]>([])
  useEffect(() => { if (window_.length) api.get<{ items: Scene[] }>(`${V1}/scenes?ids=${window_.join(',')}&per=40&hidden=1`).then(r => { const by = new Map(r.items.map(s => [s.id, s])); setScenes(window_.map(i => by.get(i)).filter(Boolean) as Scene[]) }).catch(() => {}) }, [window_])
  return (
    <div className="stack">
      <div className="row"><Switch checked={cont} onChange={setCont} label="Play the next one" /><span className="small">Play the next one when this ends</span><span className="spacer" /><span className="muted small tnum">{at + 1} of {ids.length}</span></div>
      <div className="queue-list">
        {scenes.map(s => (
          <Link key={s.id} to={`/scenes/${s.id}?${qs}`} className={`queue-item ${s.id === current ? 'current' : ''}`}>
            {s.cover ? <img src={s.cover} alt="" loading="lazy" /> : <span className="queue-none"><Icon name="film" size={16} /></span>}
            <span className="grow"><span className="queue-title ellipsis">{s.name}</span><span className="muted small">{[s.studio?.name, s.duration ? clock(s.duration) : ''].filter(Boolean).join(' · ')}</span></span>
            {s.id === current && <Icon name="play" size={14} />}
          </Link>
        ))}
      </div>
    </div>
  )
}

function Markers({ sc, markers, time, onSeek, reload }: { sc: Scene; markers: Marker[]; time: number; onSeek: (t: number) => void; reload: () => void }) {
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<Marker> | null>(null)
  const [tag, setTag] = useState<Ref[]>([])
  const [newTag, setNewTag] = useState<string[]>([])
  const start = (m?: Marker) => { setEdit(m ? { ...m } : { seconds: Math.floor(time), title: '' }); setTag(m?.tag ? [m.tag] : []); setNewTag([]) }
  const save = async () => {
    if (!edit) return
    const body = { title: edit.title || '', seconds: Number(edit.seconds) || 0, end_seconds: edit.end_seconds || null, primary_tag_id: tag[0]?.id || 0, tag_name: newTag[0] || '' }
    try {
      if (edit.id) await api.put(`${V1}/markers/${edit.id}`, body); else await api.post(`${V1}/scenes/${sc.id}/markers`, body)
      setEdit(null); reload(); announceChange()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const del = async (m: Marker) => { try { await api.del(`${V1}/markers/${m.id}`); reload(); announceChange() } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <div className="stack">
      {!edit && <button type="button" className="btn primary sm" onClick={() => start()} style={{ alignSelf: 'flex-start' }}><Icon name="plus" size={14} />Mark this moment ({clock(time)})</button>}
      {edit && (
        <div className="card pad stack">
          <div className="two-col tight grid">
            <Field label="Starts at (seconds)"><div className="row"><input type="number" min={0} value={edit.seconds ?? 0} onChange={e => setEdit({ ...edit, seconds: Number(e.target.value) })} /><button type="button" className="btn sm" onClick={() => setEdit({ ...edit, seconds: Math.floor(time) })} title="Now">Now</button></div></Field>
            <Field label="Ends at (optional)"><div className="row"><input type="number" min={0} value={edit.end_seconds ?? ''} onChange={e => setEdit({ ...edit, end_seconds: e.target.value ? Number(e.target.value) : null })} /><button type="button" className="btn sm" onClick={() => setEdit({ ...edit, end_seconds: Math.floor(time) })}>Now</button></div></Field>
          </div>
          <Field label="Tag"><Picker kind="tags" single value={tag} onChange={setTag} newNames={newTag} onNewNames={setNewTag} placeholder="What happens here" /></Field>
          <Field label="Title (optional)"><input value={edit.title || ''} onChange={e => setEdit({ ...edit, title: e.target.value })} /></Field>
          <div className="row"><button type="button" className="btn primary sm" onClick={save}>Save</button><button type="button" className="btn sm" onClick={() => setEdit(null)}>Cancel</button></div>
        </div>
      )}
      {markers.length === 0 && !edit && <p className="muted small">No markers yet. Mark moments to jump to them, see them on the timeline, and find them under Markers.</p>}
      <div className="marker-list">
        {markers.map(m => (
          <div key={m.id} className="marker-row">
            <button type="button" className="marker-time tnum" onClick={() => onSeek(m.seconds)}>{clock(m.seconds)}</button>
            <span className="grow ellipsis">{m.title || m.tag?.name}{m.title && m.tag && <span className="muted"> · {m.tag.name}</span>}</span>
            <button type="button" className="icon-btn" onClick={() => start(m)} aria-label="Edit"><Icon name="edit" size={14} /></button>
            <button type="button" className="icon-btn" onClick={() => del(m)} aria-label="Delete"><Icon name="trash" size={14} /></button>
          </div>
        ))}
      </div>
    </div>
  )
}

function FileTab({ d, reload, onGone }: { d: SceneDetail; reload: () => void; onGone: () => void }) {
  const { user, settings } = useApp()
  const { toast } = useToast()
  const [del, setDel] = useState(false)
  const f = d.file
  const sc = d.scene
  const canDelete = user?.role === 'admin' || sc.added_by === user?.id
  const rows: [string, React.ReactNode][] = [
    ['File', <code key="p" className="break">{f.path}</code>], ['Size', fmtBytes(f.size_bytes)], ['Length', clock(f.duration)],
    ['Picture', f.width ? `${f.width} × ${f.height}${f.fps ? ` · ${f.fps} fps` : ''}` : '—'], ['Video', f.video_codec || '—'], ['Sound', f.audio_codec || 'none'],
    ['Container', f.container || '—'], ['Bit rate', f.bitrate ? `${(f.bitrate / 1e6).toFixed(1)} Mbit/s` : '—'],
    ['Plays in the browser', d.playback.direct_ok ? 'as it is (seeks instantly)' : d.playback.remux_ok ? 'repackaged on the fly' : 'converted on the fly'],
    ['Fingerprints', <span key="h" className="mono small">oshash {f.oshash || '—'}<br />phash {f.phash || (sc.gen_state === 'failed' ? '—' : 'being made')}</span>],
    ['Made files', sc.gen_state === 'done' ? `${sc.has_sprite ? 'scrubber' : ''}${sc.has_sprite && sc.has_preview ? ' · ' : ''}${sc.has_preview ? 'preview' : ''}` || '—' : sc.gen_state === 'failed' ? <span key="e" className="bad">failed: {sc.gen_error}</span> : f.working ? `making the ${f.working} now…` : 'waiting'],
  ]
  return (
    <div className="stack">
      {sc.missing_since && <div className="callout warn"><Icon name="alert" size={15} /><span>The file has not been in the library since {fmtDateTime(sc.missing_since)}. If it was moved, it is found again by its fingerprint.</span></div>}
      <dl className="kv">{rows.map(([k, v]) => <div key={k} style={{ display: 'contents' }}><dt>{k}</dt><dd>{v}</dd></div>)}</dl>
      <div className="row wrap">
        <a className="btn sm" href={`${V1}/scenes/${sc.id}/stream?download=1`}><Icon name="download" size={14} />Download</a>
        <button type="button" className="btn sm" onClick={async () => { await api.post(`${V1}/scenes/${sc.id}/regenerate`, { hash: true }); toast('Queued'); reload() }}><Icon name="refresh" size={14} />Make pictures again</button>
        {sc.trashed_at
          ? <button type="button" className="btn sm" onClick={async () => { try { await api.post(`${V1}/scenes/${sc.id}/restore`); toast('Restored'); reload(); announceChange() } catch (e) { toast(errMsg(e), 'err') } }}><Icon name="undo" size={14} />Restore</button>
          : canDelete && <button type="button" className="btn sm danger-text" onClick={() => setDel(true)}><Icon name="trash" size={14} />Delete</button>}
      </div>
      {del && <Confirm title="Delete this scene?" confirmLabel="Move to trash" message={`The file (with its picture and info files) goes to the trash folder in the library. It can be restored for ${settings?.trash_days || 30} days.`}
        onCancel={() => setDel(false)} onConfirm={async () => { setDel(false); try { await api.del(`${V1}/scenes/${sc.id}`); toast('Moved to the trash'); onGone() } catch (e) { toast(errMsg(e), 'err') } }} />}
    </div>
  )
}

function History({ sc, reload }: { sc: Scene; reload: () => void }) {
  const { toast } = useToast()
  const [h, setH] = useState<SceneHistory | null>(null)
  const load = useCallback(() => { api.get<SceneHistory>(`${V1}/scenes/${sc.id}/history`).then(setH).catch(() => {}) }, [sc.id])
  useEffect(load, [load])
  if (!h) return <Loading />
  return (
    <div className="stack">
      <div className="row wrap small muted"><span>Watched {sc.mine.play_count}×</span><span>· {Math.round(sc.mine.play_seconds / 60)} min in all</span>{sc.mine.resume_seconds > 0 && <span>· stopped at {clock(sc.mine.resume_seconds)}</span>}
        {sc.mine.resume_seconds > 0 && <button type="button" className="link-btn small" onClick={async () => { await api.put(`${V1}/scenes/${sc.id}/mine`, { resume_seconds: 0 }); reload() }}>start from the beginning next time</button>}</div>
      <div className="sub-h">Sittings</div>
      {h.plays.length === 0 ? <p className="muted small">Not watched yet.</p> : (
        <table className="table compact"><tbody>{h.plays.map(p => (
          <tr key={p.id}><td>{fmtDateTime(p.started_at)}</td><td className="num tnum">{clock(p.seconds)}</td><td className="muted">{p.device}</td><td>{p.counted ? <Icon name="check" size={13} /> : ''}</td>
            <td className="right"><button type="button" className="icon-btn" aria-label="Remove" onClick={async () => { await api.del(`${V1}/plays/${p.id}`); load(); reload() }}><Icon name="x" size={13} /></button></td></tr>
        ))}</tbody></table>
      )}
      <div className="sub-h">O's</div>
      {h.os.length === 0 ? <p className="muted small">None.</p> : <ul className="set-list">{h.os.map(o => <li key={o}>{fmtDateTime(o)}</li>)}</ul>}
      {h.os.length > 0 && <button type="button" className="btn sm" style={{ alignSelf: 'flex-start' }} onClick={async () => { try { await api.del(`${V1}/scenes/${sc.id}/o`); load(); reload() } catch (e) { toast(errMsg(e), 'err') } }}>Take the last one back</button>}
    </div>
  )
}

function Edit({ sc, time, onSaved }: { sc: Scene; time: number; onSaved: (d: SceneDetail) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ title: sc.title, code: sc.code, director: sc.director, date: sc.date, details: sc.details, urls: sc.urls.join('\n'), organized: sc.organized })
  const [perfs, setPerfs] = useState<Ref[]>(sc.performers)
  const [newPerfs, setNewPerfs] = useState<string[]>([])
  const [tags, setTags] = useState<Ref[]>(sc.tags)
  const [newTags, setNewTags] = useState<string[]>([])
  const [studio, setStudio] = useState<Ref[]>(sc.studio ? [sc.studio] : [])
  const [newStudio, setNewStudio] = useState<string[]>([])
  const [groups, setGroups] = useState<Ref[]>(sc.groups || [])
  const [vr, setVr] = useState({ projection: sc.projection || 'flat', stereo: sc.stereo || 'mono', fov: sc.fov || 180 })
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try {
      const body: Record<string, unknown> = { ...f, urls: f.urls.split('\n').map(s => s.trim()).filter(Boolean), performers: perfs.map(p => p.id), new_performers: newPerfs, tags: tags.map(t => t.id), new_tags: newTags,
        studio_id: studio[0]?.id || 0, studio: newStudio[0] || '', groups: Object.fromEntries(groups.map((g, i) => [String(g.id), Number(g.extra) || i + 1])) }
      if (vr.projection !== (sc.projection || 'flat') || vr.stereo !== (sc.stereo || 'mono') || vr.fov !== (sc.fov || 180)) Object.assign(body, { projection: vr.projection, stereo: vr.stereo, fov: vr.fov })
      onSaved(await api.put<SceneDetail>(`${V1}/scenes/${sc.id}`, body))
      toast('Saved')
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const coverFile = async (file: File) => { try { await postFile(`${V1}/scenes/${sc.id}/cover`, file); onSaved(await api.get<SceneDetail>(`${V1}/scenes/${sc.id}`)); toast('Cover set') } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <div className="stack">
      <Field label="Title"><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} placeholder={sc.name} /></Field>
      <div className="two-col tight grid">
        <Field label="Date"><input value={f.date} onChange={e => setF({ ...f, date: e.target.value })} placeholder="2024-05-31" /></Field>
        <Field label="Code"><input value={f.code} onChange={e => setF({ ...f, code: e.target.value })} /></Field>
      </div>
      <Field label="Studio"><Picker kind="studios" single value={studio} onChange={v => { setStudio(v); setNewStudio([]) }} newNames={newStudio} onNewNames={setNewStudio} placeholder="No studio" /></Field>
      <Field label="Performers"><Picker kind="performers" value={perfs} onChange={setPerfs} newNames={newPerfs} onNewNames={setNewPerfs} /></Field>
      <Field label="Tags"><Picker kind="tags" value={tags} onChange={setTags} newNames={newTags} onNewNames={setNewTags} /></Field>
      <Field label="Groups"><Picker kind="groups" value={groups} onChange={setGroups} /></Field>
      <Field label="Details"><textarea rows={4} value={f.details} onChange={e => setF({ ...f, details: e.target.value })} /></Field>
      <Field label="Director"><input value={f.director} onChange={e => setF({ ...f, director: e.target.value })} /></Field>
      <Field label="Links" help="One per line."><textarea rows={2} value={f.urls} onChange={e => setF({ ...f, urls: e.target.value })} /></Field>
      <label className="check-row"><input type="checkbox" checked={f.organized} onChange={e => setF({ ...f, organized: e.target.checked })} />Organized (everything is filled in)</label>
      <div className="card pad stack">
        <div className="sub-h" style={{ margin: 0 }}>VR shape {sc.vr_set ? <span className="badge sm">set by hand</span> : <span className="badge sm">found from the file</span>}</div>
        <Field label="Projection"><Select ariaLabel="Projection" value={vr.projection} onChange={v => setVr({ ...vr, projection: v as typeof vr.projection })} options={[{ value: 'flat', label: 'Flat (not VR)' }, { value: 'equirect180', label: 'VR 180°' }, { value: 'equirect360', label: 'VR 360°' }, { value: 'fisheye', label: 'Fisheye lens' }]} /></Field>
        {vr.projection !== 'flat' && <Field label="Eyes"><Segmented size="sm" value={vr.stereo} onChange={v => setVr({ ...vr, stereo: v })} options={[{ value: 'sbs', label: 'Side by side' }, { value: 'tb', label: 'Top / bottom' }, { value: 'mono', label: 'One picture' }]} /></Field>}
        {vr.projection === 'fisheye' && <Field label="Lens"><Select ariaLabel="Lens" value={String(vr.fov)} onChange={v => setVr({ ...vr, fov: Number(v) })} options={[{ value: '180', label: '180°' }, { value: '190', label: '190° (RF52, Canon)' }, { value: '200', label: '200° (MKX200)' }, { value: '220', label: '220° (MKX220, VRCA220)' }]} /></Field>}
        {sc.vr_set && <button type="button" className="link-btn small" style={{ alignSelf: 'flex-start' }} onClick={async () => { onSaved(await api.put<SceneDetail>(`${V1}/scenes/${sc.id}`, { vr_auto: true })); toast('Found from the file again') }}>Work it out from the file again</button>}
      </div>
      <div className="row wrap">
        <span className="sub-h" style={{ margin: 0 }}>Cover</span>
        <button type="button" className="btn sm" onClick={async () => { try { await api.post(`${V1}/scenes/${sc.id}/cover`, { at: time }); onSaved(await api.get<SceneDetail>(`${V1}/scenes/${sc.id}`)); toast('Cover set') } catch (e) { toast(errMsg(e), 'err') } }}><Icon name="image" size={14} />This moment ({clock(time)})</button>
        <label className="btn sm"><Icon name="upload" size={14} />Upload…<input type="file" accept="image/*" hidden onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) coverFile(file) }} /></label>
      </div>
      <div className="row"><button type="button" className="btn primary" onClick={save} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Identify({ sc, onApplied }: { sc: Scene; onApplied: (d: SceneDetail) => void }) {
  const { toast } = useToast()
  const [q, setQ] = useState(sc.title || sc.name)
  const [res, setRes] = useState<{ results: LookupScene[]; problems: string[] | null } | null>(null)
  const [busy, setBusy] = useState(false)
  const [pick, setPick] = useState<LookupScene | null>(null)
  const [fields, setFields] = useState({ title: true, date: true, details: true, code: true, director: true, urls: true, studio: true, performers: true, tags: true, cover: true, organized: true })
  const search = async () => {
    setBusy(true); setPick(null)
    try { setRes(await api.get(`${V1}/scenes/${sc.id}/identify?q=${encodeURIComponent(q)}`)) } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  useEffect(() => { search() }, []) // eslint-disable-line react-hooks/exhaustive-deps
  const apply = async () => {
    if (!pick) return
    setBusy(true)
    try { onApplied(await api.post<SceneDetail>(`${V1}/scenes/${sc.id}/identify`, { result: pick, fields })); toast('Filled in') } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack">
      <form className="row" onSubmit={e => { e.preventDefault(); search() }}><input value={q} onChange={e => setQ(e.target.value)} placeholder="Title, studio, performer…" /><button className="btn" disabled={busy}>{busy ? 'Looking…' : 'Look up'}</button></form>
      <p className="muted small">The file’s fingerprint is checked first (exact matches are marked); the words help when it is not known yet.</p>
      {res?.problems?.map(p => <div key={p} className="callout warn"><Icon name="info" size={15} /><span>{p}{p.includes('Settings') && <> — <Link to="/settings/lookups">open Settings</Link></>}</span></div>)}
      {res && !res.results.length && !busy && <Empty title="Nothing found" icon="search">Try other words — a title, the studio and a performer usually work.</Empty>}
      <div className="lookup-list">
        {res?.results.map(r => (
          <button key={r.source + r.id} type="button" className={`lookup-item ${pick === r ? 'on' : ''}`} onClick={() => setPick(pick === r ? null : r)}>
            {r.image ? <img src={r.image} alt="" loading="lazy" referrerPolicy="no-referrer" /> : <span className="lookup-none"><Icon name="film" size={18} /></span>}
            <span className="grow">
              <span className="lookup-title">{r.title || 'Untitled'}</span>
              <span className="muted small">{[r.studio?.name, r.date, r.duration ? clock(r.duration) : ''].filter(Boolean).join(' · ')}</span>
              <span className="small ellipsis">{r.performers.map(p => p.name).join(', ')}</span>
              <span className="row" style={{ gap: 4 }}><span className="badge sm">{r.source === 'stashdb' ? 'StashDB' : 'ThePornDB'}</span>{r.matched && <span className="badge sm accent">{r.matched === 'OSHASH' ? 'exact file' : 'looks the same'}</span>}</span>
            </span>
          </button>
        ))}
      </div>
      {pick && (
        <div className="card pad stack">
          <div className="sub-h" style={{ margin: 0 }}>Take from “{pick.title}”</div>
          <div className="check-grid">
            {(Object.keys(fields) as (keyof typeof fields)[]).map(k => <label key={k} className="check-row"><input type="checkbox" checked={fields[k]} onChange={e => setFields({ ...fields, [k]: e.target.checked })} />{({ title: 'Title', date: 'Date', details: 'Description', code: 'Code', director: 'Director', urls: 'Links', studio: 'Studio', performers: 'Performers (with photos)', tags: 'Tags', cover: 'Cover', organized: 'Mark organized' } as Record<string, string>)[k]}</label>)}
          </div>
          <div className="row"><button type="button" className="btn primary" onClick={apply} disabled={busy}>Use this</button></div>
        </div>
      )}
    </div>
  )
}
