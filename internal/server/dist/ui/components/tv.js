'use client';
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useRef } from 'react';
const VERT = /* glsl */ `attribute vec2 a;void main(){gl_Position=vec4(a,0,1);}`;
const FRAG = /* glsl */ `precision highp float;
uniform float t;
uniform vec2 r;

const float FBM_STR = .08;

float h(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }

float n2(vec2 p) {
  vec2 i = floor(p), f = fract(p);
  f = f * f * (3. - 2. * f);

  return mix(
    mix(h(i), h(i + vec2(1, 0)), f.x),
    mix(h(i + vec2(0, 1)), h(i + vec2(1, 1)), f.x),
    f.y
  );
}

float fbm(vec2 p) {
  float v = 0., a = .5;

  for (int i = 0; i < 4; i++) {
    v += a * n2(p);
    p *= 2.1;
    a *= .45;
  }

  return v;
}

float drift(float speed, float s) {
  return fract(t * speed + s + .02 * sin(t * .4 + s * 3.));
}

float brushAt(vec2 uv, float y, float th, float s) {
  float hw = .34 + .08 * h(vec2(s, 77.));
  float cx = .5;
  float xn = (uv.x - (cx - hw)) / (2. * hw);
  float env = smoothstep(0., .03, xn) * smoothstep(1., .97, xn);
  float localTh = th * env;

  if (localTh < .002) return 0.;

  float morph = floor(t * 8.) * .7 + s;
  float top = y - localTh * .5 + fbm(vec2(uv.x * 6., morph)) * FBM_STR;
  float bot = y + localTh * .5 - fbm(vec2(uv.x * 6., morph + 30.)) * FBM_STR;
  float x0 = cx - hw + fbm(vec2(uv.y * 8., morph + 60.)) * FBM_STR;
  float x1 = cx + hw - fbm(vec2(uv.y * 8., morph + 90.)) * FBM_STR;

  float dMin = min(min(uv.y - top, bot - uv.y), min(uv.x - x0, x1 - uv.x));

  float bristle = n2(vec2(uv.x * 60., uv.y * 8. + s)) * .4
    + n2(vec2(uv.x * 25., (uv.y - y) * 120. + s)) * .35
    + n2(vec2(uv.x * 90., uv.y * 3. + s * 2.)) * .25;

  float eaten = smoothstep(.03, 0., dMin) * (1. - smoothstep(.2, .5, bristle));

  return clamp(smoothstep(0., .003, dMin) * (1. - eaten), 0., 1.);
}

void main() {
  vec2 uv = gl_FragCoord.xy / r;
  uv = vec2(uv.x * cos(.095) - uv.y * sin(.095), uv.x * sin(.095) + uv.y * cos(.095));
  uv += vec2(fbm(uv * 4. + t * .06), fbm(uv * 4. + 8. + t * .05)) * .012;

  vec3 c = vec3(.992, .992, .051);

  float smScroll = -drift(.04, 5.) * 2.;
  float sm = 0.;

  for (int i = 0; i < 20; i++) {
    sm = max(sm, brushAt(uv, mod(float(i) * .1 + smScroll, 2.) - .5, .04, float(i) + 10.));
  }

  float d1 = drift(.15, 1.), d2 = drift(.15, 1.37), d3 = drift(.15, 1.58), d4 = drift(.15, 1.82);
  float big = max(
    max(brushAt(uv, 1.1 - d1 * 1.4, .28, 1.), brushAt(uv, 1.1 - d2 * 1.4, .18, 2.)),
    max(brushAt(uv, 1.1 - d3 * 1.4, .3, 3.), brushAt(uv, 1.1 - d4 * 1.4, .15, 4.))
  );

  c = mix(c, vec3(0.), clamp(max(sm, big), 0., 1.));
  c *= .94 + .06 * sin(uv.y * r.y * 6.283);

  vec2 raw = gl_FragCoord.xy / r;
  float dx = min(raw.x - .22, .90 - raw.x);
  float dy = min(raw.y - .29, .86 - raw.y);
  float cycle = floor(t * .4);
  float edge = mix(smoothstep(.22, 0., max(min(dx, dy), 0.)), 1., step(.75, h(vec2(cycle, 13.))))
    * smoothstep(.85, 1., sin(t * 2.5) * .5 + .5)
    * (.7 + .3 * h(vec2(cycle, 7.)));

  float scanY = floor(gl_FragCoord.y);
  float rowNoise = h(vec2(scanY, floor(t * 30.)));
  c *= 1. - edge * max(step(.45, rowNoise), step(.3, h(vec2(gl_FragCoord.x + scanY * 7., floor(t * 45.)))) * step(.2, rowNoise));

  gl_FragColor = vec4(clamp(c, 0., 1.), 1.);
}`;
function useGL(ref) {
    const raf = useRef(0);
    useEffect(() => {
        const c = ref.current;
        if (!c) {
            return;
        }
        const gl = c.getContext('webgl');
        if (!gl) {
            return;
        }
        const sh = (type, src) => {
            const s = gl.createShader(type);
            gl.shaderSource(s, src);
            gl.compileShader(s);
            return s;
        };
        const p = gl.createProgram();
        gl.attachShader(p, sh(gl.VERTEX_SHADER, VERT));
        gl.attachShader(p, sh(gl.FRAGMENT_SHADER, FRAG));
        gl.linkProgram(p);
        gl.useProgram(p);
        gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
        gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
        const a = gl.getAttribLocation(p, 'a');
        gl.enableVertexAttribArray(a);
        gl.vertexAttribPointer(a, 2, gl.FLOAT, false, 0, 0);
        const uT = gl.getUniformLocation(p, 't');
        const uR = gl.getUniformLocation(p, 'r');
        const resize = () => {
            const rect = c.getBoundingClientRect();
            const dpr = Math.min(devicePixelRatio, 2);
            c.width = rect.width * dpr;
            c.height = rect.height * dpr;
            gl.viewport(0, 0, c.width, c.height);
        };
        resize();
        const ro = new ResizeObserver(resize);
        ro.observe(c);
        const t0 = performance.now();
        const loop = () => {
            gl.uniform1f(uT, (performance.now() - t0) / 1e3);
            gl.uniform2f(uR, c.width, c.height);
            gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
            raf.current = requestAnimationFrame(loop);
        };
        raf.current = requestAnimationFrame(loop);
        return () => {
            cancelAnimationFrame(raf.current);
            ro.disconnect();
        };
    }, [ref]);
}
export function TV({ className }) {
    const canvasRef = useRef(null);
    useGL(canvasRef);
    return (_jsxs("div", { className: ['relative', className].filter(Boolean).join(' '), children: [_jsxs("svg", { className: "relative h-full w-full", fill: "none", viewBox: "0 0 210 173", children: [_jsx("path", { d: "M30.8342 2.44471 6.08268 36.683c-.24437.338-.38254.7412-.39689 1.158L1.57754 157.126c-.03891 1.129.82339 2.087 1.95096 2.167l162.4835 11.463c.433.031.866-.074 1.238-.3l35.718-21.69c.607-.369.986-1.02 1.008-1.73l4.102-130.9871c.035-1.1269-.826-2.0806-1.951-2.1604L32.6847 1.58029c-.7248-.05144-1.4247.27551-1.8505.86442Z", fill: "#FDFD0D", stroke: "#FDFD0D", strokeWidth: "3.15" }), _jsx("path", { d: "M203.09 17.1483 35.6844 5.83395l-4.2 121.94805 168.4906 13.076z", fill: "#000", stroke: "#FDFD0D", strokeWidth: "4.2" }), _jsx("path", { d: "M190.491 29.7483 48.2859 18.434l-4.2 98.848 143.2901 10.976z", fill: "#FDFD0D" })] }), _jsx("canvas", { className: "absolute inset-0 h-full w-full", ref: canvasRef, style: {
                    clipPath: 'polygon(23% 10.65%, 90.71% 17.2%, 89.23% 74.13%, 20.99% 67.79%)'
                } })] }));
}
//# sourceMappingURL=tv.js.map