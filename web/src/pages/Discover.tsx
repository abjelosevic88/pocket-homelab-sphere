// Discover: new scenes from who you follow, your wishlist, and who you follow.
import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { ago } from '../lib/format'
import type { DiscoverItem, Follow, WantedItem } from '../lib/types'
import { RemoteCard } from '../components/Discover'
import { announceChange, useOnChange } from '../components/Layout'
import { initialsOf } from '../components/scene'
import { Empty, Loading, Tabs, errMsg, useToast } from '../components/ui'
import { Icon } from '../components/icons'

type Tab = 'new' | 'wanted' | 'following'

export default function Discover() {
  const [params, setParams] = useSearchParams()
  const tab = (params.get('tab') as Tab) || 'new'
  return (
    <div>
      <div className="page-head"><div><h1>Discover</h1><p className="muted">Scenes you don't have yet, from StashDB and TPDB. Find them on a performer's or studio's page; follow them to hear about new ones.</p></div></div>
      <Tabs tabs={[{ id: 'new', label: 'New scenes' }, { id: 'wanted', label: 'Wishlist' }, { id: 'following', label: 'Following' }]} value={tab} onChange={t => setParams({ tab: t }, { replace: true })} />
      <div className="mt">{tab === 'new' ? <NewScenes /> : tab === 'wanted' ? <Wishlist /> : <Following />}</div>
    </div>
  )
}

function NewScenes() {
  const { toast } = useToast()
  const [list, setList] = useState<DiscoverItem[] | null>(null)
  const load = () => api.get<DiscoverItem[]>(`${V1}/releases`).then(setList).catch(e => toast(errMsg(e), 'err'))
  useEffect(() => { load() }, []) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(load)
  if (!list) return <Loading />
  const dismiss = async (id?: number) => { await api.post(`${V1}/releases${id ? `/${id}` : ''}/dismiss`); setList(l => id ? (l || []).filter(x => x.release_id !== id) : []); announceChange() }
  if (!list.length) return <Empty title="Nothing new" icon="sparkles">Follow performers and studios (the bell on their page); their new scenes show up here, checked once a day.</Empty>
  return (
    <>
      <div className="row mb"><span className="muted small">{list.length} new</span><span className="spacer" /><button type="button" className="btn sm ghost" onClick={() => dismiss()}>Hide them all</button></div>
      <div className="rgrid">{list.map(it => <RemoteCard key={it.release_id} item={it} onChange={x => setList(l => (l || []).map(y => y.release_id === x.release_id ? x : y))} onDismiss={() => dismiss(it.release_id)} />)}</div>
    </>
  )
}

function Wishlist() {
  const { toast } = useToast()
  const [list, setList] = useState<WantedItem[] | null>(null)
  const load = () => api.get<WantedItem[]>(`${V1}/wanted`).then(setList).catch(e => toast(errMsg(e), 'err'))
  useEffect(() => { load() }, []) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(load)
  if (!list) return <Loading />
  if (!list.length) return <Empty title="Your wishlist is empty" icon="bookmark">Press “Want” on a scene you don't have. When its file arrives and is looked up (by itself, or with Look up), it is ticked off here.</Empty>
  const waiting = list.filter(w => !w.have_id), found = list.filter(w => w.have_id)
  const item = (w: WantedItem): DiscoverItem => ({ scene: w.scene, have_id: w.have_id, wanted_id: w.id })
  return (
    <>
      {waiting.length > 0 && <div className="rgrid">{waiting.map(w => <RemoteCard key={w.id} item={item(w)} onChange={x => { if (!x.wanted_id) setList(l => (l || []).filter(y => y.id !== w.id)) }} />)}</div>}
      {found.length > 0 && <>
        <h2 className="sec-h mt"><Icon name="check-circle" size={16} />Found — in your library now</h2>
        <div className="rgrid">{found.map(w => <RemoteCard key={w.id} item={item(w)} onChange={() => {}} onDismiss={async () => { await api.del(`${V1}/wanted/${w.id}`); setList(l => (l || []).filter(y => y.id !== w.id)) }} />)}</div>
      </>}
    </>
  )
}

function Following() {
  const { toast } = useToast()
  const [list, setList] = useState<Follow[] | null>(null)
  const load = () => api.get<Follow[]>(`${V1}/follows`).then(setList).catch(e => toast(errMsg(e), 'err'))
  useEffect(() => { load() }, []) // eslint-disable-line react-hooks/exhaustive-deps
  if (!list) return <Loading />
  if (!list.length) return <Empty title="You follow nobody yet" icon="bell">Open a performer or studio and press Follow.</Empty>
  return (
    <div className="card">
      {list.map(f => (
        <div key={f.id} className="dl-row">
          <Link to={`/${f.kind}s/${f.item_id}`} className="follow-pic">{f.image ? <img src={f.image} alt="" /> : <span>{initialsOf(f.name)}</span>}</Link>
          <div className="grow min0">
            <Link to={`/${f.kind}s/${f.item_id}`}><b>{f.name}</b></Link> <span className="muted small">{f.kind === 'performer' ? 'performer' : 'studio'}</span>
            <div className="muted small">{f.last_checked_at ? `checked ${ago(f.last_checked_at)}` : 'checking…'}{f.new ? ` · ${f.new} new` : ''}</div>
            {f.last_error && <div className="bad small">{f.last_error}</div>}
          </div>
          <button type="button" className="btn sm" onClick={async () => { const r = await api.post<{ new: number; error?: string }>(`${V1}/follows/${f.id}/check`); toast(r.error ? r.error : r.new ? `${r.new} new` : 'Nothing new', r.error ? 'err' : 'ok'); load(); announceChange() }}>Check now</button>
          <button type="button" className="icon-btn" aria-label="Stop following" onClick={async () => { await api.del(`${V1}/follows/${f.id}`); load(); announceChange() }}><Icon name="x" size={15} /></button>
        </div>
      ))}
    </div>
  )
}
