// Numbers: what you watched and when, your favourites by time, the library.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { fmtBytes } from '../lib/format'
import { longDuration } from '../lib/sphere'
import type { CountRef, Scene, Stats as StatsT } from '../lib/types'
import { SceneGrid } from '../components/scene'
import { Empty, Loading, Segmented, useAsync } from '../components/ui'
import { Icon } from '../components/icons'

export default function Stats() {
  const [days, setDays] = useState(30)
  const { data } = useAsync(() => api.get<StatsT>(`${V1}/stats?days=${days}`), [days])
  if (!data) return <Loading />
  const max = Math.max(1, ...data.days.map(d => d.seconds))
  const maxH = Math.max(1, ...data.hours)
  const lib = data.library || {}
  return (
    <div>
      <div className="page-head">
        <div><h1>Numbers</h1><p className="muted">Only yours — nobody else on the box sees them.</p></div>
        <Segmented value={days} onChange={setDays} options={[{ value: 7, label: 'Week' }, { value: 30, label: 'Month' }, { value: 365, label: 'Year' }]} />
      </div>
      <div className="stats">
        <div className="stat"><Icon name="clock" size={16} /><span className="n">{longDuration(data.seconds)}</span><span className="l">watched</span></div>
        <div className="stat"><Icon name="play" size={16} /><span className="n">{data.plays}</span><span className="l">plays</span></div>
        <div className="stat"><Icon name="droplet" size={16} /><span className="n">{data.os}</span><span className="l">O's</span></div>
        <div className="stat"><Icon name="film" size={16} /><span className="n">{lib.watched ?? 0}<small>/ {lib.total ?? 0}</small></span><span className="l">scenes watched at least once</span></div>
      </div>
      <div className="card pad">
        <h2 className="sec-h"><Icon name="chart" size={16} />Day by day</h2>
        {data.seconds === 0 ? <p className="muted">Nothing watched in this time.</p> : (
          <div className="bars" role="img" aria-label="Minutes watched per day">
            {data.days.map(d => <span key={d.day} className="bar" title={`${d.day}: ${Math.round(d.seconds / 60)} min${d.os ? `, ${d.os} O` : ''}`}><span style={{ height: `${Math.max(d.seconds ? 4 : 0, d.seconds / max * 100)}%` }} />{d.os > 0 && <i />}</span>)}
          </div>
        )}
      </div>
      <div className="two-col grid mt">
        <div className="card pad">
          <h2 className="sec-h"><Icon name="clock" size={16} />Time of day</h2>
          <div className="bars hours">{data.hours.map((n, h) => <span key={h} className="bar" title={`${h}:00 — ${n} sittings`}><span style={{ height: `${n / maxH * 100}%` }} /></span>)}</div>
          <div className="row small muted" style={{ justifyContent: 'space-between' }}><span>0:00</span><span>12:00</span><span>23:00</span></div>
        </div>
        <TopList title="Performers" icon="user" items={data.top_performers} to="performers" />
      </div>
      <div className="two-col grid mt">
        <TopList title="Studios" icon="building" items={data.top_studios} to="studios" />
        <TopList title="Tags" icon="tag" items={data.top_tags} to="tags" />
      </div>
      <div className="card pad mt">
        <h2 className="sec-h"><Icon name="trophy" size={16} />Watched the most</h2>
        {data.top_scenes.length === 0 ? <p className="muted">Nothing yet.</p> : <ol className="top-list">{data.top_scenes.map(s => <li key={s.id}><Link to={`/scenes/${s.id}`} className="ellipsis">{s.name}</Link><span className="muted tnum">{longDuration(s.count)}</span></li>)}</ol>}
      </div>
      <div className="card pad mt">
        <h2 className="sec-h"><Icon name="database" size={16} />The library</h2>
        <p className="muted">{lib.scenes ?? 0} scenes ({lib.vr ?? 0} VR) · {longDuration(lib.seconds ?? 0)} of video · {fmtBytes(lib.bytes ?? 0)} · {lib.performers ?? 0} performers · {lib.studios ?? 0} studios · {lib.tags ?? 0} tags · {lib.markers ?? 0} markers · {lib.galleries ?? 0} galleries · {lib.os ?? 0} O's in all</p>
      </div>
    </div>
  )
}

function TopList({ title, icon, items, to }: { title: string; icon: string; items: CountRef[]; to: string }) {
  return (
    <div className="card pad">
      <h2 className="sec-h"><Icon name={icon} size={16} />{title}</h2>
      {items.length === 0 ? <p className="muted">Nothing yet.</p> : <ol className="top-list">{items.map(x => <li key={x.id}><Link to={`/${to}/${x.id}`} className="ellipsis">{x.name}</Link><span className="muted tnum">{longDuration(x.count)}</span></li>)}</ol>}
    </div>
  )
}

export function Duplicates() {
  const [dist, setDist] = useState(4)
  const { data } = useAsync(() => api.get<Scene[][]>(`${V1}/duplicates?distance=${dist}`), [dist])
  return (
    <div>
      <div className="page-head">
        <div><h1>Duplicates</h1><p className="muted">Scenes whose pictures look the same — usually one video in two files. Keep the better one and delete the other.</p></div>
        <Segmented value={dist} onChange={setDist} options={[{ value: 0, label: 'Exact' }, { value: 4, label: 'Close' }, { value: 8, label: 'Loose' }]} />
      </div>
      {!data ? <Loading /> : data.length === 0 ? <Empty title="No duplicates found" icon="check-circle">Fingerprints are made in the background; new files are compared once theirs are done.</Empty> : data.map((g, i) => (
        <div key={i} className="card pad mb"><div className="sub-h" style={{ marginTop: 0 }}>{g.length} look the same</div><SceneGrid items={g} size="s" /></div>
      ))}
    </div>
  )
}
