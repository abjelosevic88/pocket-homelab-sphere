// The VR view: a video frame shown as the inside of a sphere. One full-screen
// triangle pair; for every pixel the fragment shader works out where that
// direction lands in the frame (180°/360° equirectangular or a fisheye lens,
// one eye of a side-by-side or top/bottom frame). Drag to look around, wheel
// to zoom; in a headset (WebXR) each eye gets its own half of the frame.

export type Projection = 'equirect180' | 'equirect360' | 'fisheye'
export type Stereo = 'mono' | 'sbs' | 'tb'

const VERT = `attribute vec2 aPos; varying vec2 vPos; void main() { vPos = aPos; gl_Position = vec4(aPos, 0.0, 1.0); }`

const FRAG = `precision highp float;
varying vec2 vPos;
uniform mat4 uInv;
uniform sampler2D uTex;
uniform int uProj;   // 1 = 180°, 2 = 360°, 3 = fisheye
uniform int uStereo; // 0 = mono, 1 = side by side, 2 = top/bottom
uniform int uEye;    // 0 = left, 1 = right
uniform float uFov;  // fisheye lens, radians
const float PI = 3.14159265359;
void main() {
  vec4 p = uInv * vec4(vPos, 1.0, 1.0);
  vec3 d = normalize(p.xyz / p.w);
  vec2 uv;
  if (uProj == 3) {
    float theta = acos(clamp(-d.z, -1.0, 1.0));
    if (theta > uFov * 0.5) { gl_FragColor = vec4(0.0, 0.0, 0.0, 1.0); return; }
    float r = theta / uFov;
    float phi = atan(d.y, d.x);
    uv = vec2(0.5 + r * cos(phi), 0.5 - r * sin(phi));
  } else {
    float lon = atan(d.x, -d.z);
    float lat = asin(clamp(d.y, -1.0, 1.0));
    if (uProj == 1) {
      if (abs(lon) > PI * 0.5) { gl_FragColor = vec4(0.0, 0.0, 0.0, 1.0); return; }
      uv = vec2(0.5 + lon / PI, 0.5 - lat / PI);
    } else {
      uv = vec2(0.5 + lon / (2.0 * PI), 0.5 - lat / PI);
    }
  }
  if (uStereo == 1) uv.x = uv.x * 0.5 + (uEye == 1 ? 0.5 : 0.0);
  if (uStereo == 2) uv.y = uv.y * 0.5 + (uEye == 1 ? 0.5 : 0.0);
  gl_FragColor = texture2D(uTex, uv);
}`

type M4 = Float32Array

function perspective(fovY: number, aspect: number, near = 0.1, far = 100): M4 {
  const f = 1 / Math.tan(fovY / 2), nf = 1 / (near - far)
  return new Float32Array([f / aspect, 0, 0, 0, 0, f, 0, 0, 0, 0, (far + near) * nf, -1, 0, 0, 2 * far * near * nf, 0])
}

function mul(a: M4, b: M4): M4 {
  const o = new Float32Array(16)
  for (let c = 0; c < 4; c++) for (let r = 0; r < 4; r++) {
    let s = 0
    for (let k = 0; k < 4; k++) s += a[k * 4 + r] * b[c * 4 + k]
    o[c * 4 + r] = s
  }
  return o
}

function rotX(a: number): M4 { const c = Math.cos(a), s = Math.sin(a); return new Float32Array([1, 0, 0, 0, 0, c, s, 0, 0, -s, c, 0, 0, 0, 0, 1]) }
function rotY(a: number): M4 { const c = Math.cos(a), s = Math.sin(a); return new Float32Array([c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, 0, 0, 0, 1]) }

function invert(m: M4): M4 {
  const inv = new Float32Array(16)
  inv[0] = m[5] * m[10] * m[15] - m[5] * m[11] * m[14] - m[9] * m[6] * m[15] + m[9] * m[7] * m[14] + m[13] * m[6] * m[11] - m[13] * m[7] * m[10]
  inv[4] = -m[4] * m[10] * m[15] + m[4] * m[11] * m[14] + m[8] * m[6] * m[15] - m[8] * m[7] * m[14] - m[12] * m[6] * m[11] + m[12] * m[7] * m[10]
  inv[8] = m[4] * m[9] * m[15] - m[4] * m[11] * m[13] - m[8] * m[5] * m[15] + m[8] * m[7] * m[13] + m[12] * m[5] * m[11] - m[12] * m[7] * m[9]
  inv[12] = -m[4] * m[9] * m[14] + m[4] * m[10] * m[13] + m[8] * m[5] * m[14] - m[8] * m[6] * m[13] - m[12] * m[5] * m[10] + m[12] * m[6] * m[9]
  inv[1] = -m[1] * m[10] * m[15] + m[1] * m[11] * m[14] + m[9] * m[2] * m[15] - m[9] * m[3] * m[14] - m[13] * m[2] * m[11] + m[13] * m[3] * m[10]
  inv[5] = m[0] * m[10] * m[15] - m[0] * m[11] * m[14] - m[8] * m[2] * m[15] + m[8] * m[3] * m[14] + m[12] * m[2] * m[11] - m[12] * m[3] * m[10]
  inv[9] = -m[0] * m[9] * m[15] + m[0] * m[11] * m[13] + m[8] * m[1] * m[15] - m[8] * m[3] * m[13] - m[12] * m[1] * m[11] + m[12] * m[3] * m[9]
  inv[13] = m[0] * m[9] * m[14] - m[0] * m[10] * m[13] - m[8] * m[1] * m[14] + m[8] * m[2] * m[13] + m[12] * m[1] * m[10] - m[12] * m[2] * m[9]
  inv[2] = m[1] * m[6] * m[15] - m[1] * m[7] * m[14] - m[5] * m[2] * m[15] + m[5] * m[3] * m[14] + m[13] * m[2] * m[7] - m[13] * m[3] * m[6]
  inv[6] = -m[0] * m[6] * m[15] + m[0] * m[7] * m[14] + m[4] * m[2] * m[15] - m[4] * m[3] * m[14] - m[12] * m[2] * m[7] + m[12] * m[3] * m[6]
  inv[10] = m[0] * m[5] * m[15] - m[0] * m[7] * m[13] - m[4] * m[1] * m[15] + m[4] * m[3] * m[13] + m[12] * m[1] * m[7] - m[12] * m[3] * m[5]
  inv[14] = -m[0] * m[5] * m[14] + m[0] * m[6] * m[13] + m[4] * m[1] * m[14] - m[4] * m[2] * m[13] - m[12] * m[1] * m[6] + m[12] * m[2] * m[5]
  inv[3] = -m[1] * m[6] * m[11] + m[1] * m[7] * m[10] + m[5] * m[2] * m[11] - m[5] * m[3] * m[10] - m[9] * m[2] * m[7] + m[9] * m[3] * m[6]
  inv[7] = m[0] * m[6] * m[11] - m[0] * m[7] * m[10] - m[4] * m[2] * m[11] + m[4] * m[3] * m[10] + m[8] * m[2] * m[7] - m[8] * m[3] * m[6]
  inv[11] = -m[0] * m[5] * m[11] + m[0] * m[7] * m[9] + m[4] * m[1] * m[11] - m[4] * m[3] * m[9] - m[8] * m[1] * m[7] + m[8] * m[3] * m[5]
  inv[15] = m[0] * m[5] * m[10] - m[0] * m[6] * m[9] - m[4] * m[1] * m[10] + m[4] * m[2] * m[9] + m[8] * m[1] * m[6] - m[8] * m[2] * m[5]
  let det = m[0] * inv[0] + m[1] * inv[4] + m[2] * inv[8] + m[3] * inv[12]
  if (!det) return inv
  det = 1 / det
  for (let i = 0; i < 16; i++) inv[i] *= det
  return inv
}

export interface VRShape { projection: Projection; stereo: Stereo; fov: number }

/* eslint-disable @typescript-eslint/no-explicit-any */
export class VRView {
  yaw = 0
  pitch = -0.2 // a little down: VR scenes are filmed from eye height, the action is below
  zoom = 95 * Math.PI / 180
  eye: 0 | 1 = 0
  private gl: WebGLRenderingContext
  private prog: WebGLProgram
  private tex: WebGLTexture
  private loc: Record<string, WebGLUniformLocation | null> = {}
  private shape: VRShape
  private raf = 0
  private xr: any = null
  private xrRef: any = null
  private dirty = true
  private disposed = false
  onXRChange?: (inXR: boolean) => void

  constructor(private canvas: HTMLCanvasElement, private video: HTMLVideoElement, shape: VRShape) {
    const gl = canvas.getContext('webgl', { antialias: false, preserveDrawingBuffer: false, xrCompatible: true } as any) as WebGLRenderingContext | null
    if (!gl) throw new Error('This browser cannot show VR videos (no WebGL).')
    this.gl = gl
    this.shape = shape
    const sh = (type: number, src: string) => {
      const s = gl.createShader(type)!
      gl.shaderSource(s, src)
      gl.compileShader(s)
      if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s) || 'shader')
      return s
    }
    const prog = gl.createProgram()!
    gl.attachShader(prog, sh(gl.VERTEX_SHADER, VERT))
    gl.attachShader(prog, sh(gl.FRAGMENT_SHADER, FRAG))
    gl.linkProgram(prog)
    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(prog) || 'link')
    this.prog = prog
    gl.useProgram(prog)
    const buf = gl.createBuffer()
    gl.bindBuffer(gl.ARRAY_BUFFER, buf)
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, -1, 1, 1, -1, 1, 1]), gl.STATIC_DRAW)
    const aPos = gl.getAttribLocation(prog, 'aPos')
    gl.enableVertexAttribArray(aPos)
    gl.vertexAttribPointer(aPos, 2, gl.FLOAT, false, 0, 0)
    for (const n of ['uInv', 'uTex', 'uProj', 'uStereo', 'uEye', 'uFov']) this.loc[n] = gl.getUniformLocation(prog, n)
    this.tex = gl.createTexture()!
    gl.bindTexture(gl.TEXTURE_2D, this.tex)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, new Uint8Array([0, 0, 0, 255]))
    this.loop()
  }

  setShape(shape: VRShape) { this.shape = shape; this.dirty = true }

  /** Turn the view (radians); pitch stays between straight down and straight up. */
  look(dYaw: number, dPitch: number) {
    this.yaw += dYaw
    this.pitch = Math.max(-Math.PI / 2 + 0.01, Math.min(Math.PI / 2 - 0.01, this.pitch + dPitch))
    if (this.shape.projection !== 'equirect360') this.yaw = Math.max(-Math.PI / 2, Math.min(Math.PI / 2, this.yaw))
    this.dirty = true
  }

  setZoom(deg: number) { this.zoom = Math.max(30, Math.min(140, deg)) * Math.PI / 180; this.dirty = true }
  get zoomDeg() { return this.zoom * 180 / Math.PI }
  reset() { this.yaw = 0; this.pitch = -0.2; this.zoom = 95 * Math.PI / 180; this.dirty = true }

  private upload() {
    const v = this.video
    if (v.readyState < 2 || !v.videoWidth) return false
    const gl = this.gl
    gl.bindTexture(gl.TEXTURE_2D, this.tex)
    try { gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, v) } catch { return false }
    return true
  }

  private draw(inv: M4, eye: number) {
    const gl = this.gl
    gl.useProgram(this.prog)
    gl.uniformMatrix4fv(this.loc.uInv, false, inv)
    gl.uniform1i(this.loc.uTex, 0)
    gl.uniform1i(this.loc.uProj, this.shape.projection === 'equirect360' ? 2 : this.shape.projection === 'fisheye' ? 3 : 1)
    gl.uniform1i(this.loc.uStereo, this.shape.stereo === 'sbs' ? 1 : this.shape.stereo === 'tb' ? 2 : 0)
    gl.uniform1i(this.loc.uEye, eye)
    gl.uniform1f(this.loc.uFov, Math.max(this.shape.fov || 180, 120) * Math.PI / 180)
    gl.drawArrays(gl.TRIANGLES, 0, 6)
  }

  private lastTime = -1
  private loop = () => {
    if (this.disposed || this.xr) return
    this.raf = requestAnimationFrame(this.loop)
    const c = this.canvas
    const dpr = Math.min(window.devicePixelRatio || 1, 2)
    const w = Math.round(c.clientWidth * dpr), h = Math.round(c.clientHeight * dpr)
    if (!w || !h) return
    if (c.width !== w || c.height !== h) { c.width = w; c.height = h; this.dirty = true }
    const t = this.video.currentTime
    if (t !== this.lastTime || this.video.paused === false) { if (this.upload()) { this.lastTime = t; this.dirty = true } }
    if (!this.dirty) return
    this.dirty = false
    const gl = this.gl
    gl.bindFramebuffer(gl.FRAMEBUFFER, null)
    gl.viewport(0, 0, w, h)
    const proj = perspective(this.zoom, w / h)
    const view = mul(rotX(-this.pitch), rotY(this.yaw)) // yaw > 0 looks right, pitch > 0 looks up
    this.draw(invert(mul(proj, view)), this.eye)
  }

  // ---------- headset (WebXR) ----------

  static async xrSupported(): Promise<boolean> {
    const xr = (navigator as any).xr
    if (!xr?.isSessionSupported) return false
    try { return await xr.isSessionSupported('immersive-vr') } catch { return false }
  }

  async enterXR() {
    const xr = (navigator as any).xr
    if (!xr) throw new Error('This browser has no WebXR.')
    const session = await xr.requestSession('immersive-vr', { optionalFeatures: ['local-floor'] })
    const gl = this.gl as any
    if (gl.makeXRCompatible) await gl.makeXRCompatible()
    const XRLayer = (window as any).XRWebGLLayer
    session.updateRenderState({ baseLayer: new XRLayer(session, gl) })
    this.xrRef = await session.requestReferenceSpace('local')
    this.xr = session
    cancelAnimationFrame(this.raf)
    const yaw0 = this.yaw
    session.addEventListener('select', () => { if (this.video.paused) this.video.play().catch(() => {}); else this.video.pause() })
    session.addEventListener('end', () => { this.xr = null; this.xrRef = null; this.dirty = true; this.onXRChange?.(false); this.loop() })
    this.onXRChange?.(true)
    const frame = (_t: number, f: any) => {
      if (!this.xr) return
      session.requestAnimationFrame(frame)
      const pose = f.getViewerPose(this.xrRef)
      if (!pose) return
      this.upload()
      const layer = session.renderState.baseLayer
      gl.bindFramebuffer(gl.FRAMEBUFFER, layer.framebuffer)
      for (const view of pose.views) {
        const vp = layer.getViewport(view)
        gl.viewport(vp.x, vp.y, vp.width, vp.height)
        const v = new Float32Array(view.transform.inverse.matrix)
        v[12] = 0; v[13] = 0; v[14] = 0 // directions only: the video is infinitely far away
        const m = mul(new Float32Array(view.projectionMatrix), mul(v, rotY(yaw0)))
        this.draw(invert(m), view.eye === 'right' ? 1 : 0)
      }
    }
    session.requestAnimationFrame(frame)
  }

  async exitXR() { if (this.xr) await this.xr.end().catch(() => {}) }

  dispose() {
    this.disposed = true
    cancelAnimationFrame(this.raf)
    this.exitXR()
    const ext = this.gl.getExtension('WEBGL_lose_context')
    ext?.loseContext()
  }
}
