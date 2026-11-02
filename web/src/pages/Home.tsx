// Home: continue watching, what came in lately, VR, favourites and a few to try.
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Home as HomeT } from '../lib/types'
import { PerformerCard, Shelf } from '../components/scene'
import { useOnChange } from '../components/Layout'
import { Empty, Loading, useAsync } from '../components/ui'
import { Icon } from '../components/icons'
import { hourSeed } from '../lib/sphere'

export default function Home() {
  const { user, settings } = useApp()
  const { data, error, reload } = useAsync(() => api.get<HomeT>(`${V1}/home`), [])
  useOnChange(reload)
  if (error) return <div className="callout bad"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const empty = !data.recent.length
  return (
    <div className="home">
      <div className="page-head">
        <div><h1>Hello{user?.name ? `, ${user.name.split(' ')[0]}` : ''}</h1><p className="muted">{empty ? 'Your library is empty for now.' : 'Pick up where you left off, or find something new.'}</p></div>
        <div className="row wrap">
          <Link to={`/scenes?sort=random&seed=${hourSeed()}`} className="btn"><Icon name="shuffle" size={15} />Shuffle</Link>
          <Link to="/downloads" className="btn primary"><Icon name="download" size={15} />Download from a link</Link>
        </div>
      </div>
      {empty && (
        <Empty title="No scenes yet" icon="film">
          Put videos into the library folder (they show up within minutes), paste a link under <Link to="/downloads">Downloads</Link>,
          or bring your Stash library over under <Link to="/settings/library">Settings → Library</Link>.
        </Empty>
      )}
      <Shelf title="Continue watching" icon="play" items={data.continue} more="/scenes?progress=1" />
      <Shelf title="Added lately" icon="clock" items={data.recent} more="/scenes?sort=added" />
      <Shelf title="VR" icon="vr" items={data.vr} more="/scenes?vr=yes" />
      <Shelf title="Favourites" icon="heart" items={data.favorites} more="/scenes?favorite=1" />
      <Shelf title="Top rated" icon="star" items={data.top_rated} more="/scenes?rating=8&sort=rating" />
      <Shelf title="Not watched yet" icon="eye-off" items={data.unwatched} more="/scenes?unwatched=1" />
      {data.performers.length > 0 && (
        <section className="shelf">
          <div className="sec-head"><h2 className="sec-h"><Icon name="user" size={16} />Performers</h2><Link to="/performers" className="small">See all</Link></div>
          <div className={`shelf-row perf ${settings?.discreet ? 'discreet' : ''}`}>{data.performers.map(p => <PerformerCard key={p.id} p={p} />)}</div>
        </section>
      )}
    </div>
  )
}
