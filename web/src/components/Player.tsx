// The player: the file itself when the browser can play it, a converted
// stream when not (seeking asks the server for a new one from that moment);
// the VR view for VR scenes; a scrubber with pictures and markers; plays and
// "where I stopped" go to the server while you watch.
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { api, V1 } from '../lib/api'
import { clock } from '../lib/sphere'
import type { Marker, Scene, SceneDetail, Settings, SpriteMap } from '../lib/types'
import { VRView, type VRShape } from '../lib/vr'
import { Icon } from './icons'
import { type Anchor, Menu, type MenuItem, rectOf, useToast } from './ui'

export interface PlayerHandle { time: () => number; seek: (t: number) => void; play: () => void; pause: () => void; toggle: () => void }

interface Props {
  detail: SceneDetail
  settings: Settings | null
  markers: Marker[]
  startAt?: number
  autoPlay?: boolean
  hasPrev?: boolean
  hasNext?: boolean
  onPrev?: () => void
  onNext?: () => void
  onEnded?: () => void
  onO?: () => void
  onTime?: (t: number) => void
}

const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 2]

type VRMode = 'view' | 'eye' | 'flat'
const NEXT_MODE: Record<VRMode, VRMode> = { view: 'eye', eye: 'flat', flat: 'view' }
const MODE_LABEL: Record<VRMode, string> = { view: 'Look around (VR)', eye: 'One eye, flat (2D)', flat: 'Whole frame as it is' }
const QUALITIES = ['original', '2160', '1440', '1080', '720', '480']

function shapeOf(sc: Scene): VRShape {
  return { projection: sc.projection === 'equirect360' ? 'equirect360' : sc.projection === 'fisheye' ? 'fisheye' : 'equirect180', stereo: sc.stereo || 'sbs', fov: sc.fov || 180 }
}

export const Player = forwardRef<PlayerHandle, Props>(function Player({ detail, settings, markers, startAt = 0, autoPlay, hasPrev, hasNext, onPrev, onNext, onEnded, onO, onTime }, ref) {
  const { scene, playback } = detail
  const { toast } = useToast()
  const wrap = useRef<HTMLDivElement>(null)
  const video = useRef<HTMLVideoElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const vr = useRef<VRView | null>(null)
  const canDirect = playback.direct_ok && !!playback.mime && !!document.createElement('video').canPlayType(playback.mime)
  const [mode, setMode] = useState<'direct' | 'converted'>(canDirect ? 'direct' : 'converted')
  // a converted VR video needs every pixel: the view shows only a part of the frame
  const [quality, setQuality] = useState<string>(playback.remux_ok && (!canDirect || scene.is_vr) ? 'original' : scene.is_vr ? '2160' : settings?.stream_quality || '1080')
  const [offset, setOffset] = useState(0) // converted streams start at an offset
  const [src, setSrc] = useState('')
  const [playing, setPlaying] = useState(false)
  const [waiting, setWaiting] = useState(false)
  const [t, setT] = useState(startAt)
  const [buffered, setBuffered] = useState(0)
  const [volume, setVolume] = useState(() => Number(localStorage.getItem('psp-volume') ?? (settings?.player_volume ?? 80) / 100))
  const [muted, setMuted] = useState(false)
  const [speed, setSpeed] = useState(1)
  // VR scenes: look around in the VR view, one eye as a flat 2D picture, or the whole frame as it is
  const [vrMode, setVrMode] = useState<VRMode>(scene.is_vr ? ((settings?.vr_default_mode as VRMode) || 'view') : 'flat')
  const stage = useRef<HTMLDivElement>(null)
  const [eyeStyle, setEyeStyle] = useState<React.CSSProperties | undefined>()
  const [eye, setEye] = useState<0 | 1>(0)
  const [xrOK, setXrOK] = useState(false)
  const [inXR, setInXR] = useState(false)
  const [full, setFull] = useState(false)
  const [idle, setIdle] = useState(false)
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [hover, setHover] = useState<{ x: number; t: number } | null>(null)
  const [sprite, setSprite] = useState<SpriteMap | null>(null)
  const [error, setError] = useState('')
  const duration = scene.duration || playback.duration || 0

  useImperativeHandle(ref, () => ({ time: () => cur(), seek, play: () => { video.current?.play().catch(() => {}) }, pause: () => video.current?.pause(), toggle }))

  // ---------- the source ----------
  const convertedURL = useCallback((start: number, q: string) => `${playback.converted}?start=${start.toFixed(2)}&q=${q}`, [playback.converted])
  useEffect(() => {
    setError('')
    if (mode === 'direct') { setOffset(0); setSrc(playback.direct) } else { setOffset(startAt); setSrc(convertedURL(startAt, quality)) }
  }, [scene.id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const v = video.current
    if (!v || !src) return
    if (mode === 'direct' && startAt > 0) {
      const go = () => { v.currentTime = startAt }
      v.addEventListener('loadedmetadata', go, { once: true })
      return () => v.removeEventListener('loadedmetadata', go)
    }
  }, [src]) // eslint-disable-line react-hooks/exhaustive-deps

  const cur = () => (video.current ? offset + video.current.currentTime : 0)

  function seek(to: number) {
    const v = video.current
    if (!v) return
    to = Math.max(0, Math.min(duration ? duration - 0.5 : to, to))
    setT(to)
    if (mode === 'direct') { v.currentTime = to; return }
    // a converted stream only goes forward: ask for a new one from there
    const wasPlaying = !v.paused
    setOffset(to)
    setSrc(convertedURL(to, quality))
    if (wasPlaying) setTimeout(() => video.current?.play().catch(() => {}), 0)
  }
  function toggle() { const v = video.current; if (!v) return; if (v.paused) v.play().catch(() => {}); else v.pause() }

  const switchTo = (m: 'direct' | 'converted', q = quality) => {
    const at = cur()
    const was = !(video.current?.paused ?? true)
    setMode(m)
    setQuality(q)
    if (m === 'direct') { setOffset(0); setSrc(playback.direct); const v = video.current; v?.addEventListener('loadedmetadata', () => { v.currentTime = at; if (was) v.play().catch(() => {}) }, { once: true }) } else { setOffset(at); setSrc(convertedURL(at, q)); if (was) setTimeout(() => video.current?.play().catch(() => {}), 0) }
  }

  const onError = () => {
    const v = video.current
    if (mode === 'direct') { toast('This browser cannot play the file as it is — converting it'); switchTo('converted', playback.remux_ok ? 'original' : quality); return }
    if (quality === 'original' && playback.remux_ok) { switchTo('converted', settings?.stream_quality && settings.stream_quality !== 'original' ? settings.stream_quality : '1080'); return }
    setError(`This video cannot be played${v?.error?.message ? ` (${v.error.message.replace(/^MEDIA_ELEMENT_ERROR:\s*/, '')})` : ''}.`)
  }

  // ---------- watching: the sitting goes to the server ----------
  const play = useRef<{ id: number; watched: number; last: number } | null>(null)
  const startedPlay = useRef(false)
  const beat = useCallback((keepalive = false) => {
    const p = play.current
    if (!p) return
    const body = JSON.stringify({ watched: Math.round(p.watched), position: cur(), duration })
    fetch(`${V1}/plays/${p.id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body, keepalive, credentials: 'same-origin' }).catch(() => {})
  }, [duration, offset]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    play.current = null
    startedPlay.current = false
    return () => { beat(true) }
  }, [scene.id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!playing) return
    if (!startedPlay.current) {
      startedPlay.current = true
      api.post<{ id: number }>(`${V1}/scenes/${scene.id}/plays`, { device: inXR ? 'headset' : 'browser' }).then(r => { play.current = { id: r.id, watched: 0, last: performance.now() } }).catch(() => {})
    }
    if (play.current) play.current.last = performance.now()
    const tick = window.setInterval(() => {
      const p = play.current
      if (!p) return
      const now = performance.now()
      p.watched += (now - p.last) / 1000 * (video.current?.playbackRate || 1)
      p.last = now
    }, 1000)
    const send = window.setInterval(() => beat(), 15000)
    return () => { window.clearInterval(tick); window.clearInterval(send); beat() }
  }, [playing]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const h = () => { if (document.visibilityState === 'hidden') beat(true) }
    document.addEventListener('visibilitychange', h)
    window.addEventListener('pagehide', h)
    return () => { document.removeEventListener('visibilitychange', h); window.removeEventListener('pagehide', h) }
  }, [beat])

  // ---------- the scrubber pictures ----------
  useEffect(() => {
    setSprite(null)
    if (!playback.sprite_map) return
    api.get<SpriteMap>(playback.sprite_map).then(setSprite).catch(() => {})
  }, [playback.sprite_map])

  // ---------- VR ----------
  useEffect(() => {
    if (!scene.is_vr || vrMode !== 'view' || !canvas.current || !video.current) return
    try {
      const v = new VRView(canvas.current, video.current, shapeOf(scene))
      v.eye = eye
      v.onXRChange = setInXR
      vr.current = v
    } catch (e) { setError((e as Error).message); setVrMode('flat') }
    VRView.xrSupported().then(setXrOK)
    return () => { vr.current?.dispose(); vr.current = null }
  }, [scene.id, scene.is_vr, vrMode]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { vr.current?.setShape(shapeOf(scene)) }, [scene.projection, scene.stereo, scene.fov]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (vr.current) { vr.current.eye = eye; vr.current.setShape(shapeOf(scene)) } }, [eye]) // eslint-disable-line react-hooks/exhaustive-deps
  const drag = useRef<{ x: number; y: number; moved: boolean } | null>(null)
  const onPointerDown = (e: React.PointerEvent) => { if (!vr.current) return; drag.current = { x: e.clientX, y: e.clientY, moved: false }; (e.target as HTMLElement).setPointerCapture(e.pointerId) }
  const onPointerMove = (e: React.PointerEvent) => {
    const d = drag.current
    if (!d || !vr.current || !canvas.current) return
    const k = (vr.current.zoom / canvas.current.clientHeight)
    const dx = e.clientX - d.x, dy = e.clientY - d.y
    if (Math.abs(dx) + Math.abs(dy) > 3) d.moved = true
    vr.current.look(-dx * k, dy * k)
    d.x = e.clientX; d.y = e.clientY
  }
  const onPointerUp = () => { const d = drag.current; drag.current = null; if (d && !d.moved) toggle() }
  const onWheel = (e: React.WheelEvent) => { if (vr.current) { vr.current.setZoom(vr.current.zoomDeg + e.deltaY * 0.05) } }

  // ---------- one eye, flat ----------
  // Shows one side (or half) of the frame filling the player, at the shape a
  // single eye really has (square for 180°, 2:1 for 360°), so squeezed
  // re-encodes look right too.
  const eyeMode = scene.is_vr && vrMode === 'eye'
  useEffect(() => {
    if (!eyeMode) { setEyeStyle(undefined); return }
    const st = stage.current, v = video.current
    if (!st || !v) return
    const layout = () => {
      if (!v.videoWidth) return
      const sbs = scene.stereo === 'sbs', tb = scene.stereo === 'tb'
      const natural = (v.videoWidth / (sbs ? 2 : 1)) / (v.videoHeight / (tb ? 2 : 1))
      const aspect = scene.projection === 'equirect360' ? 2 : scene.projection === 'equirect180' || scene.projection === 'fisheye' ? 1 : natural
      const SW = st.clientWidth, SH = st.clientHeight
      const bh = Math.min(SH, SW / aspect), bw = bh * aspect
      const second = eye === 1
      setEyeStyle({
        position: 'absolute', maxWidth: 'none', objectFit: 'fill',
        width: bw * (sbs ? 2 : 1), height: bh * (tb ? 2 : 1),
        left: (SW - bw) / 2 - (sbs && second ? bw : 0), top: (SH - bh) / 2 - (tb && second ? bh : 0),
        clipPath: sbs ? (second ? 'inset(0 0 0 50%)' : 'inset(0 50% 0 0)') : tb ? (second ? 'inset(50% 0 0 0)' : 'inset(0 0 50% 0)') : undefined,
      })
    }
    layout()
    v.addEventListener('loadedmetadata', layout)
    const ro = new ResizeObserver(layout)
    ro.observe(st)
    return () => { v.removeEventListener('loadedmetadata', layout); ro.disconnect() }
  }, [eyeMode, eye, src, scene.stereo, scene.projection]) // eslint-disable-line react-hooks/exhaustive-deps

  // ---------- volume, speed, fullscreen ----------
  useEffect(() => { const v = video.current; if (v) { v.volume = volume; v.muted = muted } localStorage.setItem('psp-volume', String(volume)) }, [volume, muted, src])
  useEffect(() => { if (video.current) video.current.playbackRate = speed }, [speed, src])
  useEffect(() => {
    const h = () => setFull(document.fullscreenElement === wrap.current)
    document.addEventListener('fullscreenchange', h)
    return () => document.removeEventListener('fullscreenchange', h)
  }, [])
  const fullscreen = () => { if (document.fullscreenElement) document.exitFullscreen().catch(() => {}); else wrap.current?.requestFullscreen?.().catch(() => {}) }

  // the controls fade while playing and the pointer rests
  const idleTimer = useRef(0)
  const wake = () => { setIdle(false); window.clearTimeout(idleTimer.current); idleTimer.current = window.setTimeout(() => setIdle(true), 2600) }
  useEffect(() => () => window.clearTimeout(idleTimer.current), [])

  // ---------- keys ----------
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const tg = e.target as HTMLElement
      if (tg.closest('input, textarea, select, [contenteditable="true"], .modal-backdrop, .menu, .sel-pop') || e.metaKey || e.ctrlKey || e.altKey) return
      const skip = settings?.skip_seconds || 10
      const v = video.current
      if (!v) return
      switch (e.key) {
        case ' ': case 'k': e.preventDefault(); toggle(); break
        case 'ArrowLeft': e.preventDefault(); seek(cur() - (e.shiftKey ? 60 : skip)); break
        case 'ArrowRight': e.preventDefault(); seek(cur() + (e.shiftKey ? 60 : skip)); break
        case 'j': seek(cur() - skip); break
        case 'l': seek(cur() + skip); break
        case 'ArrowUp': e.preventDefault(); setVolume(x => Math.min(1, x + 0.05)); setMuted(false); break
        case 'ArrowDown': e.preventDefault(); setVolume(x => Math.max(0, x - 0.05)); break
        case 'm': setMuted(x => !x); break
        case 'f': fullscreen(); break
        case ',': if (v.paused && mode === 'direct') v.currentTime = Math.max(0, v.currentTime - 1 / 30); break
        case '.': if (v.paused && mode === 'direct') v.currentTime += 1 / 30; break
        case '[': setSpeed(s => SPEEDS[Math.max(0, SPEEDS.indexOf(s) - 1)]); break
        case ']': setSpeed(s => SPEEDS[Math.min(SPEEDS.length - 1, SPEEDS.indexOf(s) + 1)]); break
        case 'o': onO?.(); break
        case 'v': if (scene.is_vr) setVrMode(m => NEXT_MODE[m]); break
        case 'r': vr.current?.reset(); break
        case 'n': if (hasNext) onNext?.(); break
        case 'p': if (hasPrev) onPrev?.(); break
        default:
          if (/^[0-9]$/.test(e.key) && duration) seek(duration * Number(e.key) / 10)
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  // ---------- events ----------
  const onTimeUpdate = () => {
    const v = video.current
    if (!v) return
    const now = offset + v.currentTime
    setT(now)
    onTime?.(now)
    if (v.buffered.length) setBuffered(offset + v.buffered.end(v.buffered.length - 1))
  }

  const pct = (x: number) => duration ? `${Math.max(0, Math.min(100, x / duration * 100))}%` : '0%'
  const bar = useRef<HTMLDivElement>(null)
  const timeAt = (clientX: number) => { const r = bar.current!.getBoundingClientRect(); return Math.max(0, Math.min(1, (clientX - r.left) / r.width)) * duration }
  const scrubbing = useRef(false)

  const thumbStyle = (at: number, scale = 1): React.CSSProperties | undefined => {
    if (!sprite || !playback.sprite) return undefined
    const i = Math.min(sprite.count - 1, Math.max(0, Math.floor(at / sprite.interval)))
    const x = (i % sprite.cols) * sprite.width, y = Math.floor(i / sprite.cols) * sprite.height
    return { width: sprite.width * scale, height: sprite.height * scale, backgroundImage: `url(${playback.sprite})`, backgroundPosition: `-${x * scale}px -${y * scale}px`, backgroundSize: `${sprite.cols * sprite.width * scale}px ${sprite.rows * sprite.height * scale}px` }
  }

  const gearItems = (): MenuItem[] => {
    const items: MenuItem[] = [{ heading: 'Speed' }, ...SPEEDS.map(s => ({ label: `${s}×`, checked: s === speed, onSelect: () => setSpeed(s) }))]
    items.push('sep', { heading: 'Quality' })
    if (canDirect) items.push({ label: 'Original file', hint: 'best, seeks instantly', checked: mode === 'direct', onSelect: () => switchTo('direct') })
    for (const q of QUALITIES) {
      if (q === 'original') { if (playback.remux_ok) items.push({ label: 'Original quality, repackaged', checked: mode === 'converted' && quality === 'original', onSelect: () => switchTo('converted', 'original') }); continue }
      if (scene.height && Number(q) > scene.height) continue
      items.push({ label: `${q}p (converted)`, checked: mode === 'converted' && quality === q, onSelect: () => switchTo('converted', q) })
    }
    if (scene.is_vr) items.push('sep', { heading: 'VR' }, ...(['view', 'eye', 'flat'] as VRMode[]).map(m => ({ label: MODE_LABEL[m], checked: vrMode === m, onSelect: () => setVrMode(m) })))
    if (scene.is_vr && scene.stereo !== 'mono') items.push('sep', { heading: 'Eye' }, { label: scene.stereo === 'tb' ? 'Top' : 'Left', checked: eye === 0, onSelect: () => setEye(0) }, { label: scene.stereo === 'tb' ? 'Bottom' : 'Right', checked: eye === 1, onSelect: () => setEye(1) })
    return items
  }

  const showVR = scene.is_vr && vrMode === 'view'
  return (
    <div ref={wrap} className={`player ${full ? 'is-full' : ''} ${idle && playing ? 'is-idle' : ''} ${showVR ? 'is-vr' : ''}`} onPointerMove={wake} onMouseLeave={() => playing && setIdle(true)}>
      <div ref={stage} className={`player-stage ${eyeMode ? 'is-eye' : ''}`}>
        <video ref={video} src={src || undefined} className={showVR ? 'hidden-video' : ''} style={eyeStyle} playsInline preload="metadata" autoPlay={autoPlay}
          poster={scene.cover || undefined}
          onPlay={() => setPlaying(true)} onPause={() => setPlaying(false)} onWaiting={() => setWaiting(true)} onPlaying={() => setWaiting(false)} onCanPlay={() => setWaiting(false)}
          onTimeUpdate={onTimeUpdate} onEnded={() => { setPlaying(false); onEnded?.() }} onError={onError}
          onClick={() => !showVR && toggle()} onDoubleClick={() => !showVR && fullscreen()} />
        {showVR && <canvas ref={canvas} className="player-vr" onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onWheel={onWheel} onDoubleClick={fullscreen} />}
        {(!playing && !waiting && !error) && <button type="button" className="player-big" aria-label="Play" onClick={toggle}><Icon name="play" size={34} /></button>}
        {waiting && <div className="player-wait"><span className="spinner" /></div>}
        {error && <div className="player-error"><Icon name="alert" size={18} /><span>{error}</span></div>}
        {playback.missing && <div className="player-error"><Icon name="alert" size={18} /><span>The file is not in the library any more.</span></div>}
        {showVR && !playing && !error && <div className="player-hint">Drag to look around · scroll to zoom</div>}
      </div>
      <div className="player-controls" onPointerDown={e => e.stopPropagation()}>
        <div ref={bar} className="player-bar"
          onPointerDown={e => { scrubbing.current = true; (e.target as HTMLElement).setPointerCapture(e.pointerId); setT(timeAt(e.clientX)) }}
          onPointerMove={e => { const at = timeAt(e.clientX); setHover({ x: e.clientX - bar.current!.getBoundingClientRect().left, t: at }); if (scrubbing.current) setT(at) }}
          onPointerUp={e => { if (scrubbing.current) { scrubbing.current = false; seek(timeAt(e.clientX)) } }}
          onPointerLeave={() => { if (!scrubbing.current) setHover(null) }}>
          <div className="player-track">
            <div className="player-buffer" style={{ width: pct(buffered) }} />
            <div className="player-played" style={{ width: pct(t) }} />
            {markers.map(m => <span key={m.id} className="player-mark" style={{ left: pct(m.seconds) }} title={m.title || m.tag?.name} />)}
            <span className="player-knob" style={{ left: pct(t) }} />
          </div>
          {hover && duration > 0 && (
            <div className="player-tip" style={{ left: Math.max(70, Math.min((bar.current?.clientWidth || 0) - 70, hover.x)) }}>
              {sprite && <div className="player-tip-pic" style={thumbStyle(hover.t)} />}
              <span>{clock(hover.t)}</span>
            </div>
          )}
        </div>
        <div className="player-row">
          <button type="button" className="pbtn" onClick={onPrev} disabled={!hasPrev} aria-label="Previous scene" title="Previous (p)"><Icon name="skip-back" size={17} /></button>
          <button type="button" className="pbtn" onClick={toggle} aria-label={playing ? 'Pause' : 'Play'} title="Play / pause (space)"><Icon name={playing ? 'pause' : 'play'} size={19} /></button>
          <button type="button" className="pbtn" onClick={onNext} disabled={!hasNext} aria-label="Next scene" title="Next (n)"><Icon name="skip-forward" size={17} /></button>
          <button type="button" className="pbtn hide-sm" onClick={() => seek(cur() - (settings?.skip_seconds || 10))} aria-label="Back" title="Back (←)"><Icon name="rotate-ccw" size={17} /></button>
          <button type="button" className="pbtn hide-sm" onClick={() => seek(cur() + (settings?.skip_seconds || 10))} aria-label="Forward" title="Forward (→)"><Icon name="rotate-cw" size={17} /></button>
          <span className="pvol hide-sm">
            <button type="button" className="pbtn" onClick={() => setMuted(m => !m)} aria-label={muted ? 'Sound on' : 'Mute'} title="Mute (m)"><Icon name={muted || volume === 0 ? 'volume-x' : 'volume'} size={17} /></button>
            <input type="range" min={0} max={1} step={0.01} value={muted ? 0 : volume} onChange={e => { setVolume(Number(e.target.value)); setMuted(false) }} aria-label="Volume" />
          </span>
          <span className="ptime tnum">{clock(t)} / {clock(duration)}</span>
          <span className="spacer" />
          {speed !== 1 && <span className="pbadge">{speed}×</span>}
          {mode === 'converted' && <span className="pbadge" title="The server converts this video while you watch">{quality === 'original' ? 'repackaged' : `${scene.height ? Math.min(Number(quality), scene.height) : quality}p`}</span>}
          <button type="button" className="pbtn" onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))} aria-label="Speed and quality" title="Speed and quality"><Icon name="settings" size={17} /></button>
          {scene.is_vr && <button type="button" className={`pbtn vr-mode ${vrMode !== 'flat' ? 'on' : ''}`} onClick={() => setVrMode(m => NEXT_MODE[m])} aria-label={`Now: ${MODE_LABEL[vrMode]}. Switch to: ${MODE_LABEL[NEXT_MODE[vrMode]]}`} title={`${MODE_LABEL[vrMode]} — click for ${MODE_LABEL[NEXT_MODE[vrMode]].toLowerCase()} (v)`}><Icon name="vr" size={19} />{vrMode === 'eye' && <span className="vr-mode-tag">2D</span>}</button>}
          {showVR && xrOK && <button type="button" className="pbtn" onClick={() => vr.current?.enterXR().catch(e => toast((e as Error).message, 'err'))} aria-label="Enter headset" title="Watch in the headset"><Icon name="orbit" size={17} /></button>}
          <button type="button" className="pbtn" onClick={fullscreen} aria-label={full ? 'Leave full screen' : 'Full screen'} title="Full screen (f)"><Icon name={full ? 'minimize' : 'maximize'} size={17} /></button>
        </div>
      </div>
      {menu && <Menu anchor={menu} align="right" width={240} items={gearItems()} onClose={() => setMenu(null)} />}
    </div>
  )
})

/** The strip of pictures under the player, like a film: click one to jump there. */
export function Scrubber({ detail, time, markers, onSeek }: { detail: SceneDetail; time: number; markers: Marker[]; onSeek: (t: number) => void }) {
  const [sprite, setSprite] = useState<SpriteMap | null>(null)
  const strip = useRef<HTMLDivElement>(null)
  const { playback, scene } = detail
  useEffect(() => { setSprite(null); if (playback.sprite_map) api.get<SpriteMap>(playback.sprite_map).then(setSprite).catch(() => {}) }, [playback.sprite_map])
  const scale = 0.85
  const active = sprite ? Math.min(sprite.count - 1, Math.floor(time / sprite.interval)) : -1
  useEffect(() => {
    const el = strip.current
    if (!el || !sprite || active < 0) return
    const w = sprite.width * scale + 4
    const want = active * w - el.clientWidth / 2 + w / 2
    if (Math.abs(el.scrollLeft - want) > el.clientWidth / 2) el.scrollTo({ left: want, behavior: 'smooth' })
  }, [active, sprite])
  if (!sprite || !playback.sprite) return null
  const dur = scene.duration
  return (
    <div className="scrubber">
      <button type="button" className="scrub-nav" aria-label="Earlier" onClick={() => strip.current?.scrollBy({ left: -strip.current.clientWidth * 0.8, behavior: 'smooth' })}><Icon name="chevron-left" size={16} /></button>
      <div ref={strip} className="scrub-strip">
        {Array.from({ length: sprite.count }, (_, i) => {
          const x = (i % sprite.cols) * sprite.width, y = Math.floor(i / sprite.cols) * sprite.height
          const at = i * sprite.interval
          const mk = markers.filter(m => m.seconds >= at && m.seconds < at + sprite.interval)
          return (
            <button key={i} type="button" className={`scrub-pic ${i === active ? 'active' : ''}`} onClick={() => onSeek(at)} title={clock(at)}
              style={{ width: sprite.width * scale, height: sprite.height * scale, backgroundImage: `url(${playback.sprite})`, backgroundPosition: `-${x * scale}px -${y * scale}px`, backgroundSize: `${sprite.cols * sprite.width * scale}px ${sprite.rows * sprite.height * scale}px` }}>
              <span className="scrub-time">{clock(at)}{i + 1 < sprite.count && dur ? '' : ''}</span>
              {mk.length > 0 && <span className="scrub-mark">{mk[0].title || mk[0].tag?.name}</span>}
            </button>
          )
        })}
      </div>
      <button type="button" className="scrub-nav" aria-label="Later" onClick={() => strip.current?.scrollBy({ left: strip.current.clientWidth * 0.8, behavior: 'smooth' })}><Icon name="chevron-right" size={16} /></button>
    </div>
  )
}
