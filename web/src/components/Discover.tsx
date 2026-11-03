// Discover: scenes you do not have yet (from StashDB / TPDB), the wishlist and following.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { clock } from '../lib/sphere'
import { fmtDate } from '../lib/format'
import type { DiscoverItem, DiscoverPage } from '../lib/types'
import { announceChange } from './Layout'
import { Icon } from './icons'
import { Segmented, Spinner, Switch, errMsg, useToast } from './ui'

const SOURCE: Record<string, string> = { stashdb: 'StashDB', tpdb: 'TPDB' }

/** One scene from a lookup site: what it is, and what you can do with it. */
export function RemoteCard({ item, onChange, onDismiss }: { item: DiscoverItem; onChange: (it: DiscoverItem) => void; onDismiss?: () => void }) {
  const { toast } = useToast()
  const { settings } = useApp()
  const sc = item.scene
  const link = (sc.urls || []).find(u => /^https?:\/\//.test(u))
  const want = async () => {
    try {
      if (item.wanted_id) { await api.del(`${V1}/wanted/${item.wanted_id}`); onChange({ ...item, wanted_id: 0 }) }
      else { const r = await api.post<{ id: number }>(`${V1}/wanted`, { scene: sc }); onChange({ ...item, wanted_id: r.id }); toast('On your wishlist — ticked off when the file arrives') }
      announceChange()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const download = async () => {
    if (!link) return
    // exactly this one video: a page of short clips, or a file much shorter than the scene, is not it
    try { await api.post(`${V1}/downloads`, { url: link, options: { single: true, expect_seconds: Math.round(sc.duration || 0), performers: [], tags: [] } }); toast('Sent to Downloads — the full scene usually needs your cookies for that site') } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <div className={`rcard ${item.have_id ? 'have' : ''} ${settings?.discreet ? 'discreet' : ''}`}>
      <div className="rcard-media">
        {sc.image ? <img src={sc.image} alt="" loading="lazy" referrerPolicy="no-referrer" /> : <span className="rcard-none"><Icon name="film" size={22} /></span>}
        {sc.duration > 0 && <span className="scard-dur tnum">{clock(sc.duration)}</span>}
        {item.have_id > 0 && <span className="rcard-have"><Icon name="check" size={12} />In your library</span>}
      </div>
      <div className="rcard-body">
        <div className="scard-title" title={sc.title}>{sc.title || 'Untitled'}</div>
        <div className="scard-meta">{sc.studio && <span className="ellipsis">{sc.studio.name}</span>}{sc.date && <span className="nowrap">{fmtDate(sc.date)}</span>}</div>
        {sc.performers.length > 0 && <div className="scard-perfs ellipsis">{sc.performers.map(p => p.name).join(', ')}</div>}
        <div className="rcard-acts">
          {item.have_id > 0
            ? <Link to={`/scenes/${item.have_id}`} className="btn sm"><Icon name="play" size={13} />Watch</Link>
            : <button type="button" className={`btn sm ${item.wanted_id ? 'active' : ''}`} onClick={want} title={item.wanted_id ? 'Take it off the wishlist' : 'Put it on your wishlist'}><Icon name="bookmark" size={13} />{item.wanted_id ? 'Wanted' : 'Want'}</button>}
          {!item.have_id && link && <button type="button" className="btn sm ghost" onClick={download} title="Try to download this scene from its page (studio sites need your cookies — Settings → Downloads); clips and previews are not kept"><Icon name="download" size={13} /></button>}
          {link && <a className="btn sm ghost" href={link} target="_blank" rel="noreferrer noopener" title="Open its page"><Icon name="external" size={13} /></a>}
          {onDismiss && <button type="button" className="btn sm ghost" onClick={onDismiss} title="Seen it — hide"><Icon name="x" size={13} /></button>}
          <span className="spacer" />
          <span className="badge sm">{SOURCE[sc.source] || sc.source}</span>
        </div>
      </div>
    </div>
  )
}

/** "Scenes you don't have" on a performer's or studio's page. */
export function MissingScenes({ kind, id, name }: { kind: 'performer' | 'studio'; id: number; name: string }) {
  const [open, setOpen] = useState(false)
  const [source, setSource] = useState('')
  const [data, setData] = useState<DiscoverPage | null>(null)
  const [items, setItems] = useState<DiscoverItem[]>([])
  const [page, setPage] = useState(1)
  const [busy, setBusy] = useState(false)
  const [showHave, setShowHave] = useState(false)
  const load = useCallback(async (p: number, src: string, fresh = false) => {
    setBusy(true)
    try {
      const r = await api.get<DiscoverPage>(`${V1}/${kind}s/${id}/discover?page=${p}${src ? `&source=${src}` : ''}${fresh ? '&fresh=1' : ''}`)
      setData(r); setItems(prev => p === 1 ? r.items : [...prev, ...r.items]); setPage(p)
      if (r.source) setSource(r.source)
    } catch (e) { setData({ problem: errMsg(e), sources: [], items: [] }) } finally { setBusy(false) }
  }, [kind, id])
  useEffect(() => { setOpen(false); setItems([]); setData(null) }, [kind, id])
  const shown = showHave ? items : items.filter(i => !i.have_id)
  const haveCount = items.filter(i => i.have_id).length
  const more = data?.total != null && items.length < data.total
  if (!open) return (
    <div className="discover-start card pad">
      <Icon name="sparkles" size={18} />
      <div className="grow"><b>Scenes {kind === 'performer' ? 'with' : 'from'} {name} you don't have</b><div className="muted small">Looked up on StashDB / TPDB with your keys. Put them on your wishlist, open their page or try to download them.</div></div>
      <button type="button" className="btn primary" onClick={() => { setOpen(true); load(1, source) }}>Find them</button>
    </div>
  )
  return (
    <section className="discover-sec">
      <div className="sec-head">
        <h2 className="sec-h"><Icon name="sparkles" size={16} />Scenes you don't have{data?.total != null && <span className="muted tnum small"> · {data.total} on {SOURCE[source] || source}, {haveCount} of the shown in your library</span>}</h2>
        <span className="spacer" />
        {(data?.sources?.length || 0) > 1 && <Segmented size="sm" value={source} onChange={v => { setSource(v); load(1, v) }} options={data!.sources.map(s => ({ value: s, label: SOURCE[s] || s }))} />}
        <span className="row small"><Switch size="sm" checked={showHave} onChange={setShowHave} label="Show the ones I have" />Show the ones I have</span>
        <button type="button" className="btn sm ghost" onClick={() => load(1, source, true)} disabled={busy} title="Ask again (lists are kept for 6 hours)"><Icon name="refresh" size={14} /></button>
      </div>
      {data?.problem && <div className="callout warn"><Icon name="info" size={15} /><span>{data.problem}{/Settings/.test(data.problem) && <> — <Link to="/settings/lookups">open Settings</Link></>}</span></div>}
      {busy && !items.length && <div className="loading"><Spinner /> Looking it up…</div>}
      {!busy && data && !data.problem && !shown.length && <p className="muted">{items.length ? 'You have every scene shown here.' : 'Nothing found.'}{more && ' There are more — show the next ones.'}</p>}
      <div className="rgrid">{shown.map(it => <RemoteCard key={it.scene.source + it.scene.id} item={it} onChange={x => setItems(list => list.map(y => y.scene.id === x.scene.id ? x : y))} />)}</div>
      {more && <div className="load-more"><button type="button" className="btn" onClick={() => load(page + 1, source)} disabled={busy}>{busy ? 'Loading…' : `Show more (${(data!.total! - items.length).toLocaleString()} left)`}</button></div>}
    </section>
  )
}

/** Follow / unfollow a performer or studio. */
export function FollowButton({ kind, id }: { kind: 'performer' | 'studio'; id: number }) {
  const { toast } = useToast()
  const [fid, setFid] = useState<number | null>(null)
  useEffect(() => { api.get<{ follow_id: number }>(`${V1}/${kind}s/${id}/follow`).then(r => setFid(r.follow_id)).catch(() => setFid(0)) }, [kind, id])
  if (fid == null) return null
  const toggle = async () => {
    try {
      if (fid) { await api.del(`${V1}/follows/${fid}`); setFid(0); toast('Not following any more') }
      else { const r = await api.post<{ id: number }>(`${V1}/follows`, { kind, item_id: id }); setFid(r.id); toast('Following — new scenes show up under Discover (checked daily)') }
      announceChange()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  return <button type="button" className={`btn ${fid ? 'active' : ''}`} onClick={toggle} title={fid ? 'Stop following' : 'Get told about new scenes'}><Icon name={fid ? 'bell' : 'bell'} size={15} />{fid ? 'Following' : 'Follow'}</button>
}
