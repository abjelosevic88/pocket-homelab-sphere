// Downloads, like MeTube: paste a link (a video, a playlist, a channel),
// pick quality and folder, and it lands in the library with its picture and
// facts. Follow a channel to fetch new videos as they come.
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtBytes, fmtDateTime, ago } from '../lib/format'
import type { Download, DownloadOptions, Ref, Subscription, ToolStatus } from '../lib/types'
import { Picker } from '../components/scene'
import { announceChange } from '../components/Layout'
import { Select } from '../components/Select'
import { Confirm, Empty, Field, Switch, copyText, errMsg, useToast } from '../components/ui'
import { Icon } from '../components/icons'

const QUALITY = [{ value: 'best', label: 'Best' }, { value: '2160', label: '2160p' }, { value: '1440', label: '1440p' }, { value: '1080', label: '1080p' }, { value: '720', label: '720p' }, { value: '480', label: '480p' }]
const FORMAT = [{ value: 'auto', label: 'Auto' }, { value: 'mp4', label: 'MP4' }, { value: 'mkv', label: 'MKV' }, { value: 'webm', label: 'WebM' }]

export default function Downloads() {
  const { settings } = useApp()
  const { toast } = useToast()
  const [params, setParams] = useSearchParams()
  const [url, setUrl] = useState(params.get('url') || '')
  const [list, setList] = useState<Download[]>([])
  const [subs, setSubs] = useState<Subscription[]>([])
  const [tool, setTool] = useState<ToolStatus | null>(null)
  const [folders, setFolders] = useState<string[]>([])
  const [adv, setAdv] = useState(() => localStorage.getItem('psp-dl-adv') === '1')
  const [busy, setBusy] = useState(false)
  const [o, setO] = useState<DownloadOptions>(() => {
    try { return JSON.parse(localStorage.getItem('psp-dl-opts') || '{}') } catch { return {} }
  })
  const [perfs, setPerfs] = useState<Ref[]>([])
  const [tags, setTags] = useState<Ref[]>([])
  const [studio, setStudio] = useState<Ref[]>([])
  const [check, setCheck] = useState(60)
  const [filter, setFilter] = useState('')
  const [clear, setClear] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => { localStorage.setItem('psp-dl-adv', adv ? '1' : '0') }, [adv])
  useEffect(() => { const { password: _p, ...keep } = o; localStorage.setItem('psp-dl-opts', JSON.stringify(keep)) }, [o]) // eslint-disable-line @typescript-eslint/no-unused-vars

  const load = () => {
    api.get<Download[]>(`${V1}/downloads`).then(setList).catch(() => {})
    api.get<Subscription[]>(`${V1}/subscriptions`).then(setSubs).catch(() => {})
  }
  useEffect(() => {
    load()
    api.get<ToolStatus>(`${V1}/downloads/tool`).then(setTool).catch(() => {})
    api.get<{ path: string }[]>(`${V1}/folders`).then(f => setFolders(f.map(x => x.path))).catch(() => {})
    input.current?.focus()
  }, [])
  const active = list.some(d => ['queued', 'downloading', 'processing'].includes(d.status) || (d.kind === 'auto' && d.status !== 'failed'))
  useEffect(() => { const t = window.setInterval(load, active ? 1500 : 10000); return () => window.clearInterval(t) }, [active])
  // a finished download changes the library
  const doneCount = list.filter(d => d.status === 'done').length
  useEffect(() => { if (doneCount) announceChange() }, [doneCount])
  // ?url=…&go=1 (the bookmarklet) queues at once
  useEffect(() => { if (params.get('url') && params.get('go') === '1') { start(); setParams({}, { replace: true }) } }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const opts = (): DownloadOptions => ({ ...o, performers: perfs.map(p => p.id), tags: tags.map(t => t.id), studio_id: studio[0]?.id || 0 })
  const start = async (paused = false) => {
    const links = url.split(/\s+/).filter(Boolean)
    if (!links.length) { input.current?.focus(); return }
    setBusy(true)
    try {
      await api.post(`${V1}/downloads`, { urls: links, options: { ...opts(), auto_start: paused ? false : o.auto_start } })
      setUrl(''); setParams({}, { replace: true }); load(); toast(links.length > 1 ? `${links.length} links queued` : 'Queued')
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const subscribe = async () => {
    const link = url.trim()
    if (!link) { input.current?.focus(); return }
    setBusy(true)
    try { await api.post(`${V1}/subscriptions`, { url: link, options: opts(), check_minutes: check, title_filter: filter }); setUrl(''); load(); toast('Following — new videos will be fetched as they come') } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const act = async (d: Download, action: string, extra: Record<string, unknown> = {}) => { try { await api.put(`${V1}/downloads/${d.id}`, { action, ...extra }); load() } catch (e) { toast(errMsg(e), 'err') } }
  const remove = async (d: Download) => { try { await api.del(`${V1}/downloads/${d.id}`); load() } catch (e) { toast(errMsg(e), 'err') } }

  const groups = useMemo(() => {
    const kids = list.filter(d => d.kind !== 'playlist')
    return {
      now: kids.filter(d => d.status === 'downloading' || d.status === 'processing' || (d.kind === 'auto' && d.status === 'queued')),
      waiting: kids.filter(d => (d.status === 'queued' && d.kind !== 'auto') || d.status === 'paused'),
      done: kids.filter(d => d.status === 'done'),
      failed: kids.filter(d => d.status === 'failed' || d.status === 'canceled'),
      lists: list.filter(d => d.kind === 'playlist'),
    }
  }, [list])
  const bookmarklet = `javascript:location.href='${(settings?.base_url || window.location.origin)}/downloads?go=1&url='+encodeURIComponent(location.href)`
  const yt = tool?.ytdlp
  const canDownload = settings?.downloads_open !== false

  return (
    <div className="downloads">
      <div className="page-head">
        <div><h1>Downloads</h1><p className="muted">Paste a link to a video, a playlist or a channel. It goes into your library with its picture, title, date and tags.</p></div>
        {yt && <span className={`badge ${yt.error ? '' : 'ok'}`} title={yt.error || `yt-dlp ${yt.version}${yt.updated_at ? ` · checked ${ago(yt.updated_at)}` : ''}`}>yt-dlp {yt.version || '—'}</span>}
      </div>
      {!canDownload && <div className="callout warn"><Icon name="lock" size={15} /><span>Downloads are for admins on this box.</span></div>}

      <div className="dl-form card pad">
        <div className="dl-input">
          <input ref={input} value={url} onChange={e => setUrl(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') start() }} placeholder="Enter video, channel or playlist URL (several: one per line or space)" aria-label="Link" disabled={!canDownload} />
          <button type="button" className="btn primary lg" onClick={() => start()} disabled={busy || !canDownload}><Icon name="download" size={16} />Download</button>
          <button type="button" className="btn lg" onClick={subscribe} disabled={busy || !canDownload} title="Check this channel or playlist for new videos"><Icon name="rss" size={16} />Subscribe</button>
        </div>
        <div className="dl-opts">
          <Field label="Quality"><Select ariaLabel="Quality" value={o.quality || settings?.download_quality || 'best'} onChange={v => setO({ ...o, quality: v })} options={QUALITY} /></Field>
          <Field label="Format"><Select ariaLabel="Format" value={o.format || settings?.download_format || 'auto'} onChange={v => setO({ ...o, format: v })} options={FORMAT} /></Field>
          <Field label="Folder"><FolderInput value={o.folder ?? settings?.download_folder ?? ''} folders={folders} onChange={v => setO({ ...o, folder: v })} /></Field>
        </div>
        <button type="button" className="link-btn small dl-adv-btn" onClick={() => setAdv(a => !a)} aria-expanded={adv}>Advanced options <Icon name={adv ? 'chevron-up' : 'chevron-down'} size={13} /></button>
        {adv && (
          <div className="dl-advanced">
            <div className="sub-h">Output</div>
            <div className="dl-grid">
              <Field label="Name prefix"><input value={o.name_prefix || ''} onChange={e => setO({ ...o, name_prefix: e.target.value })} placeholder="Default" /></Field>
              <Field label="One folder per video" help="With its picture and info, like the rest of the library."><Switch checked={o.own_folder ?? settings?.own_folder ?? true} onChange={v => setO({ ...o, own_folder: v })} label="One folder per video" /></Field>
              <Field label="Clip start"><input value={o.clip_start || ''} onChange={e => setO({ ...o, clip_start: e.target.value })} placeholder="e.g. 2:26" /></Field>
              <Field label="Clip end"><input value={o.clip_end || ''} onChange={e => setO({ ...o, clip_end: e.target.value })} placeholder="e.g. 3:24" /></Field>
            </div>
            <div className="sub-h">Put on the scene</div>
            <div className="dl-grid">
              <Field label="Performers"><Picker kind="performers" value={perfs} onChange={setPerfs} /></Field>
              <Field label="Tags"><Picker kind="tags" value={tags} onChange={setTags} /></Field>
              <Field label="Studio"><Picker kind="studios" single value={studio} onChange={setStudio} placeholder="From the site (uploader)" /></Field>
            </div>
            <div className="sub-h">Behaviour</div>
            <div className="dl-grid">
              <Field label="Start right away"><Switch checked={o.auto_start ?? true} onChange={v => setO({ ...o, auto_start: v })} label="Start right away" /></Field>
              <Field label="Items limit" help="Playlists and channels: only the newest this many."><input type="number" min={0} value={o.items_limit || ''} onChange={e => setO({ ...o, items_limit: Number(e.target.value) || 0 })} placeholder="All" /></Field>
              <Field label="Subscription check (minutes)"><input type="number" min={15} value={check} onChange={e => setCheck(Number(e.target.value) || 60)} /></Field>
              <Field label="Subscription title filter" help="A regular expression the title must match."><input value={filter} onChange={e => setFilter(e.target.value)} placeholder="Optional regex" /></Field>
              <Field label="Video password"><input type="password" value={o.password || ''} onChange={e => setO({ ...o, password: e.target.value })} placeholder="Optional" autoComplete="off" /></Field>
            </div>
            <p className="muted small">Sites that need a sign-in work with your browser's cookies: export a cookies.txt and add it under <Link to="/settings/downloads">Settings → Downloads</Link>{tool?.cookies ? ' (yours are there)' : ''}.</p>
          </div>
        )}
      </div>

      {list.length === 0 && subs.length === 0 && <Empty title="Nothing downloaded yet" icon="download">Paste a link above — or press ⌘V / Ctrl+V anywhere in the app with a link copied.</Empty>}

      <DlSection title="Downloading" icon="download" items={groups.now} act={act} remove={remove} />
      <DlSection title="Waiting" icon="clock" items={groups.waiting} act={act} remove={remove} />
      <DlSection title="Failed" icon="alert" items={groups.failed} act={act} remove={remove} />
      <DlSection title="Done" icon="check-circle" items={groups.done} act={act} remove={remove} extra={groups.done.length > 0 && <button type="button" className="btn sm ghost" onClick={() => setClear(true)}>Clear the list</button>} />

      {subs.length > 0 && (
        <section className="dl-sec">
          <div className="sec-head"><h2 className="sec-h"><Icon name="rss" size={16} />Following</h2></div>
          <div className="card">
            {subs.map(x => (
              <div key={x.id} className="dl-row">
                <Icon name="rss" size={16} className="muted" />
                <div className="grow min0">
                  <div className="ellipsis"><b>{x.name || x.url}</b></div>
                  <div className="muted small ellipsis">{x.url} · every {x.check_minutes} min · {x.found} fetched{x.last_checked_at ? ` · checked ${ago(x.last_checked_at)}` : ' · checking…'}{x.title_filter ? ` · only “${x.title_filter}”` : ''}</div>
                  {x.last_error && <div className="bad small">{x.last_error}</div>}
                </div>
                <Switch checked={x.enabled} onChange={async v => { await api.put(`${V1}/subscriptions/${x.id}`, { enabled: v }); load() }} label="On" size="sm" />
                <button type="button" className="btn sm" onClick={async () => { const r = await api.post<{ queued: number }>(`${V1}/subscriptions/${x.id}/check`); toast(r.queued ? `${r.queued} new` : 'Nothing new'); load() }}>Check now</button>
                <button type="button" className="icon-btn" aria-label="Stop following" onClick={async () => { await api.del(`${V1}/subscriptions/${x.id}`); load() }}><Icon name="trash" size={15} /></button>
              </div>
            ))}
          </div>
        </section>
      )}

      <section className="dl-sec">
        <div className="sec-head"><h2 className="sec-h"><Icon name="zap" size={16} />Quicker ways</h2></div>
        <div className="card pad stack small">
          <div><b>Paste anywhere.</b> With a link copied, press ⌘V / Ctrl+V on any page of the app — it opens here, filled in.</div>
          <div><b>From the browser.</b> Drag this to your bookmarks bar, then click it on a video page: <a className="btn sm" href={bookmarklet} onClick={e => { e.preventDefault(); copyText(bookmarklet).then(() => toast('Bookmarklet copied')) }}><Icon name="bookmark" size={13} />Send to Sphere</a></div>
          <div><b>From your phone.</b> A Shortcut can <code>POST {settings?.base_url || window.location.origin}/api/v1/downloads</code> with <code>{'{"url":"…"}'}</code> and a personal token (Settings → API &amp; shortcuts).</div>
        </div>
      </section>
      {clear && <Confirm title="Clear the finished downloads?" confirmLabel="Clear" danger={false} message="Only the list is cleared; the videos stay in your library." onCancel={() => setClear(false)} onConfirm={async () => { setClear(false); await api.post(`${V1}/downloads/clear`, { failed: false }); load() }} />}
    </div>
  )
}

function FolderInput({ value, folders, onChange }: { value: string; folders: string[]; onChange: (v: string) => void }) {
  return (
    <>
      <input list="psp-folders" value={value} onChange={e => onChange(e.target.value)} placeholder="The library itself" />
      <datalist id="psp-folders">{folders.slice(0, 300).map(f => <option key={f} value={f} />)}</datalist>
    </>
  )
}

function DlSection({ title, icon, items, act, remove, extra }: { title: string; icon: string; items: Download[]; act: (d: Download, a: string, extra?: Record<string, unknown>) => void; remove: (d: Download) => void; extra?: React.ReactNode }) {
  if (!items.length) return null
  return (
    <section className="dl-sec">
      <div className="sec-head"><h2 className="sec-h"><Icon name={icon} size={16} />{title} <span className="muted tnum">{items.length}</span></h2><span className="spacer" />{extra}</div>
      <div className="card">{items.map(d => <DlRow key={d.id} d={d} act={act} remove={remove} />)}</div>
    </section>
  )
}

function DlRow({ d, act, remove }: { d: Download; act: (d: Download, a: string, extra?: Record<string, unknown>) => void; remove: (d: Download) => void }) {
  const running = d.status === 'downloading' || d.status === 'processing'
  const label = d.kind === 'auto' && d.status !== 'failed' ? 'Reading the link…' : d.status === 'processing' ? 'Adding to the library…' : d.status === 'paused' ? 'Paused' : d.status === 'queued' ? 'Waiting' : ''
  return (
    <div className="dl-row">
      {d.thumbnail ? <img className="dl-thumb" src={d.thumbnail} alt="" referrerPolicy="no-referrer" loading="lazy" /> : <span className="dl-thumb none"><Icon name="film" size={16} /></span>}
      <div className="grow min0">
        <div className="ellipsis">{d.scene_id ? <Link to={`/scenes/${d.scene_id}`}><b>{d.title || d.url}</b></Link> : <b>{d.title || d.url}</b>}</div>
        <div className="muted small ellipsis">{d.title ? d.url : ''}{d.size_bytes ? ` · ${fmtBytes(d.size_bytes)}` : ''}{d.options.folder ? ` · ${d.options.folder}` : ''}{d.status === 'done' ? ` · ${fmtDateTime(d.updated_at)}` : ''}</div>
        {running && d.kind === 'video' && <div className="dl-progress"><span style={{ width: `${Math.max(2, d.percent)}%` }} /></div>}
        {(running || label) && <div className="muted small tnum">{label || `${d.percent.toFixed(0)}%`}{d.speed && ` · ${d.speed}`}{d.eta && ` · ${d.eta} left`}</div>}
        {d.error && d.status !== 'done' && <div className="bad small">{d.error}</div>}
      </div>
      {d.status === 'failed' && d.ask === 'list' && <button type="button" className="btn sm" onClick={() => act(d, 'all')}><Icon name="download" size={14} />Download all {d.found}</button>}
      {d.status === 'failed' && d.ask === 'pick' && (d.options.choices || []).length > 0 && (
        <span className="dl-picks">{(d.options.choices || []).slice(0, 6).map(c => <button key={c.url} type="button" className="btn sm" onClick={() => act(d, 'pick', { url: c.url })} title={c.url}><Icon name="download" size={13} />{c.label}</button>)}</span>
      )}
      {d.status === 'failed' && d.ask !== 'list' && d.ask !== 'pick' && <button type="button" className="btn sm" onClick={() => act(d, 'retry')}><Icon name="refresh" size={14} />Try again</button>}
      {d.status === 'paused' && <button type="button" className="btn sm" onClick={() => act(d, 'start')}><Icon name="play" size={14} />Start</button>}
      {d.status === 'queued' && d.kind === 'video' && <button type="button" className="btn sm ghost" onClick={() => act(d, 'pause')}>Pause</button>}
      {d.scene_id && <Link to={`/scenes/${d.scene_id}`} className="btn sm"><Icon name="play" size={14} />Watch</Link>}
      <button type="button" className="icon-btn" aria-label={running ? 'Cancel' : 'Remove from the list'} title={running ? 'Cancel' : 'Remove from the list'} onClick={() => remove(d)}><Icon name="x" size={15} /></button>
    </div>
  )
}
