// Galleries: folders and zips of pictures, a grid and a full-screen viewer.
import { useCallback, useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Gallery, GalleryImage, Ref } from '../lib/types'
import { Picker, RefChips, Stars, usePaged } from '../components/scene'
import { announceChange, useCrumb, useOnChange } from '../components/Layout'
import { Empty, Field, Loading, Modal, errMsg, useDebounce, useToast } from '../components/ui'
import { Icon } from '../components/icons'

export default function Galleries() {
  const { settings } = useApp()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const { items, total, loading, more, next, reload } = usePaged<Gallery>(`${V1}/galleries?q=${encodeURIComponent(dq)}&sort=added`)
  useOnChange(reload)
  return (
    <div>
      <div className="page-head"><div><h1>Galleries</h1><p className="muted">{total} galleries — every folder (or .zip) of pictures without a video in it.</p></div></div>
      <div className="toolbar"><div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search galleries…" /></div></div>
      {!loading && !items.length && <Empty title="No galleries" icon="image">Put a folder of pictures (or a .zip of them) into the library folder.</Empty>}
      <div className={`pgrid ${settings?.discreet ? 'discreet' : ''}`}>
        {items.map(g => (
          <Link key={g.id} to={`/galleries/${g.id}`} className="pcard">
            <div className="pcard-media">{g.cover ? <img src={g.cover} alt="" loading="lazy" /> : <div className="pcard-none"><Icon name="image" size={26} /></div>}{g.favorite && <span className="scard-fav"><Icon name="heart" size={13} /></span>}</div>
            <div className="pcard-body"><div className="pcard-name ellipsis">{g.name}</div><div className="scard-meta"><span>{g.image_count} pictures</span>{g.studio && <span className="ellipsis">{g.studio.name}</span>}</div></div>
          </Link>
        ))}
      </div>
      {more && !loading && <div className="load-more"><button type="button" className="btn" onClick={next}>Show more</button></div>}
    </div>
  )
}

export function GalleryPage() {
  const { id } = useParams()
  const { settings } = useApp()
  const [data, setData] = useState<{ gallery: Gallery; images: GalleryImage[] } | null>(null)
  const [open, setOpen] = useState<number | null>(null)
  const [edit, setEdit] = useState(false)
  const load = useCallback(() => { api.get<{ gallery: Gallery; images: GalleryImage[] }>(`${V1}/galleries/${id}`).then(setData).catch(() => {}) }, [id])
  useEffect(load, [load])
  useCrumb(data?.gallery.name)
  if (!data) return <Loading />
  const g = data.gallery
  const mark = async (patch: { favorite?: boolean; rating?: number }) => { await api.put(`${V1}/galleries/${g.id}/mine`, patch); setData({ ...data, gallery: { ...g, ...patch } }) }
  return (
    <div>
      <div className="page-head">
        <div><h1>{g.name}</h1><p className="muted">{g.image_count} pictures{g.date && ` · ${g.date}`}{g.studio && <> · <Link to={`/studios/${g.studio.id}`}>{g.studio.name}</Link></>}{g.photographer && ` · by ${g.photographer}`}</p></div>
        <div className="row">
          <Stars value={g.rating} onChange={v => mark({ rating: v })} />
          <button type="button" className={`btn icon-only ghost ${g.favorite ? 'fav-on' : ''}`} onClick={() => mark({ favorite: !g.favorite })} aria-label="Favourite"><Icon name="heart" size={18} /></button>
          <button type="button" className="btn" onClick={() => setOpen(0)} disabled={!data.images.length}><Icon name="play" size={15} />Slideshow</button>
          <button type="button" className="btn" onClick={() => setEdit(true)}><Icon name="edit" size={15} />Edit</button>
        </div>
      </div>
      <RefChips items={g.performers} kind="performers" />
      <RefChips items={g.tags} kind="tags" />
      {g.details && <p className="scene-details">{g.details}</p>}
      <div className={`thumb-grid ${settings?.discreet ? 'discreet' : ''}`}>
        {data.images.map((im, i) => <button key={im.id} type="button" className="thumb" onClick={() => setOpen(i)}><img src={im.thumb} alt="" loading="lazy" /></button>)}
      </div>
      {open != null && <Lightbox images={data.images} start={open} onClose={() => setOpen(null)} onCover={async imgId => { await api.put(`${V1}/galleries/${g.id}`, { cover_image: imgId }); load() }} />}
      {edit && <GalleryEditor g={g} onClose={() => setEdit(false)} onSaved={x => { setEdit(false); setData(x); announceChange() }} />}
    </div>
  )
}

function Lightbox({ images, start, onClose, onCover }: { images: GalleryImage[]; start: number; onClose: () => void; onCover: (id: number) => void }) {
  const [i, setI] = useState(start)
  const [show, setShow] = useState(start === 0)
  const go = (d: number) => setI(x => (x + d + images.length) % images.length)
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      else if (e.key === 'ArrowRight' || e.key === ' ') { e.preventDefault(); go(1) }
      else if (e.key === 'ArrowLeft') go(-1)
      else if (e.key === 's') setShow(s => !s)
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (!show) return; const t = window.setInterval(() => go(1), 4000); return () => window.clearInterval(t) }, [show]) // eslint-disable-line react-hooks/exhaustive-deps
  const im = images[i]
  return createPortal(
    <div className="lightbox" onClick={e => { if (e.target === e.currentTarget) onClose() }}>
      <img src={im.url} alt="" />
      <div className="lb-bar">
        <span className="tnum">{i + 1} / {images.length}</span>
        <span className="spacer" />
        <button type="button" className="pbtn" onClick={() => setShow(s => !s)} title="Slideshow (s)"><Icon name={show ? 'pause' : 'play'} size={17} /></button>
        <button type="button" className="pbtn" onClick={() => onCover(im.id)} title="Use as the cover"><Icon name="image" size={17} /></button>
        <a className="pbtn" href={im.url} download title="Download"><Icon name="download" size={17} /></a>
        <button type="button" className="pbtn" onClick={onClose} title="Close (esc)"><Icon name="x" size={18} /></button>
      </div>
      <button type="button" className="lb-nav prev" onClick={() => go(-1)} aria-label="Previous"><Icon name="chevron-left" size={28} /></button>
      <button type="button" className="lb-nav next" onClick={() => go(1)} aria-label="Next"><Icon name="chevron-right" size={28} /></button>
    </div>, document.body)
}

function GalleryEditor({ g, onClose, onSaved }: { g: Gallery; onClose: () => void; onSaved: (d: { gallery: Gallery; images: GalleryImage[] }) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ title: g.title, date: g.date, details: g.details, photographer: g.photographer })
  const [perfs, setPerfs] = useState<Ref[]>(g.performers)
  const [newPerfs, setNewPerfs] = useState<string[]>([])
  const [tags, setTags] = useState<Ref[]>(g.tags)
  const [newTags, setNewTags] = useState<string[]>([])
  const [studio, setStudio] = useState<Ref[]>(g.studio ? [g.studio] : [])
  const save = async () => {
    try { onSaved(await api.put(`${V1}/galleries/${g.id}`, { ...f, performers: perfs.map(p => p.id), new_performers: newPerfs, tags: tags.map(t => t.id), new_tags: newTags, studio_id: studio[0]?.id || 0 })); toast('Saved') } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <Modal title="Edit gallery" onClose={onClose} footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={save}>Save</button></>}>
      <div className="grid" style={{ gap: 12 }}>
        <Field label="Title"><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} placeholder={g.name} /></Field>
        <div className="two-col tight grid"><Field label="Date"><input value={f.date} onChange={e => setF({ ...f, date: e.target.value })} /></Field><Field label="Photographer"><input value={f.photographer} onChange={e => setF({ ...f, photographer: e.target.value })} /></Field></div>
        <Field label="Studio"><Picker kind="studios" single value={studio} onChange={setStudio} /></Field>
        <Field label="Performers"><Picker kind="performers" value={perfs} onChange={setPerfs} newNames={newPerfs} onNewNames={setNewPerfs} /></Field>
        <Field label="Tags"><Picker kind="tags" value={tags} onChange={setTags} newNames={newTags} onNewNames={setNewTags} /></Field>
        <Field label="Details"><textarea rows={3} value={f.details} onChange={e => setF({ ...f, details: e.target.value })} /></Field>
      </div>
    </Modal>
  )
}
