'use client';
import { jsx as _jsx } from "react/jsx-runtime";
import { useEffect, useRef, useState } from 'react';
import { useGpuTier } from '../../hooks/use-gpu-tier';
import { cn, hexToRgb } from '../../utils';
const NUM_BANDS = 12;
const VERT = `attribute vec2 a;varying vec2 vUv;void main(){vUv=vec2(a.x*.5+.5,.5-a.y*.5);gl_Position=vec4(a,0,1);}`;
const FRAG = `precision highp float;
uniform float t;
uniform vec2 r,imgSize,vel;
uniform sampler2D tex;
uniform float bands[${NUM_BANDS}];
uniform vec3 tint;
uniform float tintStrength;
varying vec2 vUv;

float h(vec2 p){return fract(sin(dot(p,vec2(127.1,311.7)))*43758.5453);}

// cover-style UV: crops the image to fill the canvas, centered
vec2 coverUV(vec2 uv){
  float canvasAspect=r.x/r.y;
  float imgAspect=imgSize.x/imgSize.y;
  vec2 scale=canvasAspect>imgAspect
    ?vec2(1.0,imgAspect/canvasAspect)
    :vec2(canvasAspect/imgAspect,1.0);
  return(uv-0.5)*scale+0.5;
}

void main(){
  vec2 uv=coverUV(vUv);
  float scanY=floor(vUv.y*r.y);

  float bandF=vUv.y*${NUM_BANDS}.0;
  int bandIdx=int(floor(bandF));
  float bandFrac=fract(bandF);

  float strength=0.0;
  for(int i=0;i<${NUM_BANDS};i++){
    if(i==bandIdx) strength=bands[i];
  }

  float neighborStr=0.0;
  int neighborIdx=bandFrac>.5?bandIdx+1:bandIdx-1;
  for(int i=0;i<${NUM_BANDS};i++){
    if(i==neighborIdx) neighborStr=bands[i];
  }
  float edgeBlend=abs(bandFrac-.5)*2.0;
  edgeBlend*=edgeBlend;
  strength=mix(strength,neighborStr,edgeBlend*.3);

  float speed=length(vel);
  float dirBlend=smoothstep(0.0,0.02,speed);
  vec2 dir=speed>.0001?vel/speed:vec2(0);
  dir*=dirBlend;

  float rowSeed=h(vec2(scanY,floor(t*3.)+float(bandIdx)*7.));
  float rowVar=mix(.4,1.0,rowSeed);

  float ySmooth=vUv.y*6.0+t*0.7;
  float yNoise=mix(h(vec2(floor(ySmooth),13.)),h(vec2(floor(ySmooth)+1.0,13.)),smoothstep(0.0,1.0,fract(ySmooth)));
  float colVar=mix(.4,1.0,yNoise);

  float tearShiftX=dir.x*strength*rowVar*0.15;
  float tearShiftY=dir.y*strength*colVar*0.10;

  float bandSeed=h(vec2(float(bandIdx),42.));
  tearShiftX+=strength*(.5-bandSeed)*0.05;

  float yJitter=mix(h(vec2(floor(ySmooth),73.)),h(vec2(floor(ySmooth)+1.0,73.)),smoothstep(0.0,1.0,fract(ySmooth)));
  tearShiftY+=strength*(.5-yJitter)*0.035;

  uv.x+=tearShiftX;
  uv.y+=tearShiftY;

  float sortGate=step(.5,strength)*step(.4,rowSeed);
  uv.x+=dir.x*sortGate*strength*0.03;
  uv.y+=dir.y*sortGate*strength*0.02;

  float caX=abs(tearShiftX)*2.5+sortGate*strength*0.01;
  float caY=abs(tearShiftY)*2.5+sortGate*strength*0.01;
  float cr=texture2D(tex,vec2(uv.x+caX,uv.y+caY)).r;
  float cg=texture2D(tex,uv).g;
  float cb=texture2D(tex,vec2(uv.x-caX,uv.y-caY)).b;

  vec3 col=vec3(cr,cg,cb);

  col*=.97+.03*sin(vUv.y*r.y*3.14159);

  float bandEdge=smoothstep(.02,.0,min(bandFrac,1.0-bandFrac));
  col+=vec3(bandEdge*strength*.1);

  col=mix(col,col*tint,tintStrength);

  gl_FragColor=vec4(col,1.0);
}`;
/**
 * Choreographed motion patterns used when `autoPlay` is set. Each pattern
 * returns a synthetic pointer position in [0,1] and a hover intensity in
 * [0,1] for the current time (seconds). They drive the shader without
 * requiring a real pointer, which is what lets us record the distortion
 * as a GIF / screenshot / poster.
 */
const AUTOPLAY_PATTERNS = {
    aggressive: t => {
        const cycle = 1.4;
        const phase = (t % cycle) / cycle;
        const stab = Math.exp(-((phase - 0.15) ** 2) * 260);
        const angle = Math.floor(t / cycle) * 1.37;
        const mx = 0.5 + Math.cos(angle) * 0.42 * (stab + 0.15);
        const my = 0.5 + Math.sin(angle) * 0.38 * (stab + 0.15);
        return { hover: 0.55 + stab * 0.45, mx, my };
    },
    gentle: t => ({
        hover: 0.45 + Math.sin(t * 0.9) * 0.1,
        mx: 0.5 + Math.sin(t * 0.5) * 0.28,
        my: 0.5 + Math.cos(t * 0.37) * 0.22
    }),
    slash: t => {
        // Long breath -> sword slash -> recoil twitch, repeating.
        const cycle = 3.6;
        const phase = (t % cycle) / cycle;
        const slash = Math.exp(-((phase - 0.28) ** 2) * 180);
        const micro = Math.exp(-((phase - 0.7) ** 2) * 340);
        const driftX = 0.5 + Math.sin(t * 0.7) * 0.16;
        const driftY = 0.55 + Math.cos(t * 0.5) * 0.14;
        // Slash trajectory: bottom-left up into the giant's chest (top-right).
        const slashX = -0.15 + phase * 1.55;
        const slashY = 0.95 - phase * 1.35;
        const mx = driftX * (1 - slash) + slashX * slash;
        const my = driftY * (1 - slash) + slashY * slash;
        return { hover: 0.5 + slash * 0.5 + micro * 0.35, mx, my };
    }
};
export function ImageDistortion({ active = true, autoPlay, className, fallbackClassName, src, style, tint, tintStrength }) {
    const canvasRef = useRef(null);
    const tier = useGpuTier();
    const [loaded, setLoaded] = useState(false);
    const activeRef = useRef(active);
    activeRef.current = active;
    const tintStrengthRef = useRef(tintStrength);
    tintStrengthRef.current = tintStrength;
    const autoPlayRef = useRef(autoPlay);
    autoPlayRef.current = autoPlay;
    const state = useRef({
        bandTargets: new Float32Array(NUM_BANDS),
        bands: new Float32Array(NUM_BANDS),
        hoverTarget: 0,
        imgH: 1,
        imgW: 1,
        mx: 0.5,
        my: 0.5,
        prevMx: 0.5,
        prevMy: 0.5,
        vx: 0,
        vy: 0
    });
    useEffect(() => {
        if (tier === 0) {
            return;
        }
        const c = canvasRef.current;
        if (!c) {
            return;
        }
        const gl = c.getContext('webgl');
        if (!gl) {
            return;
        }
        const compile = (type, source) => {
            const s = gl.createShader(type);
            gl.shaderSource(s, source);
            gl.compileShader(s);
            return s;
        };
        const prog = gl.createProgram();
        gl.attachShader(prog, compile(gl.VERTEX_SHADER, VERT));
        gl.attachShader(prog, compile(gl.FRAGMENT_SHADER, FRAG));
        gl.linkProgram(prog);
        gl.useProgram(prog);
        gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
        gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
        const a = gl.getAttribLocation(prog, 'a');
        gl.enableVertexAttribArray(a);
        gl.vertexAttribPointer(a, 2, gl.FLOAT, false, 0, 0);
        const uT = gl.getUniformLocation(prog, 't');
        const uR = gl.getUniformLocation(prog, 'r');
        const uImgSize = gl.getUniformLocation(prog, 'imgSize');
        const uVel = gl.getUniformLocation(prog, 'vel');
        const uTex = gl.getUniformLocation(prog, 'tex');
        const uTint = gl.getUniformLocation(prog, 'tint');
        const uTintStrength = gl.getUniformLocation(prog, 'tintStrength');
        const uBands = [];
        for (let i = 0; i < NUM_BANDS; i++) {
            uBands.push(gl.getUniformLocation(prog, `bands[${i}]`));
        }
        const texture = gl.createTexture();
        gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, new Uint8Array([0, 0, 0, 255]));
        const img = new Image();
        img.crossOrigin = 'anonymous';
        img.onload = () => {
            state.current.imgW = img.naturalWidth;
            state.current.imgH = img.naturalHeight;
            gl.bindTexture(gl.TEXTURE_2D, texture);
            gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, img);
            setLoaded(true);
        };
        img.src = src;
        gl.activeTexture(gl.TEXTURE0);
        gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.uniform1i(uTex, 0);
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
        const onMove = (e) => {
            const rect = c.getBoundingClientRect();
            state.current.mx = (e.clientX - rect.left) / rect.width;
            state.current.my = (e.clientY - rect.top) / rect.height;
        };
        const onEnter = () => {
            state.current.hoverTarget = 1;
        };
        const onLeave = () => {
            state.current.hoverTarget = 0;
        };
        // When autoPlay drives the distortion we want the poster to look
        // alive regardless of whether a pointer is near the canvas, so we
        // skip the real pointer listeners entirely.
        if (!autoPlayRef.current) {
            c.addEventListener('pointermove', onMove);
            c.addEventListener('pointerenter', onEnter);
            c.addEventListener('pointerleave', onLeave);
        }
        const bandEaseRates = new Float32Array(NUM_BANDS);
        for (let i = 0; i < NUM_BANDS; i++) {
            bandEaseRates[i] = 0.02 + Math.random() * 0.06;
        }
        const tintVec = tint
            ? (() => {
                const [tr, tg, tb] = hexToRgb(tint);
                return [tr / 255, tg / 255, tb / 255];
            })()
            : [1, 1, 1];
        const t0 = performance.now();
        let raf = 0;
        const loop = () => {
            raf = requestAnimationFrame(loop);
            const s = state.current;
            const pattern = autoPlayRef.current
                ? AUTOPLAY_PATTERNS[autoPlayRef.current]
                : null;
            if (pattern) {
                const driven = pattern((performance.now() - t0) / 1e3);
                s.mx = driven.mx;
                s.my = driven.my;
                s.hoverTarget = driven.hover;
            }
            const dvx = s.mx - s.prevMx;
            const dvy = s.my - s.prevMy;
            s.vx += (dvx * 8 - s.vx) * 0.1;
            s.vy += (dvy * 8 - s.vy) * 0.1;
            s.prevMx = s.mx;
            s.prevMy = s.my;
            const speed = Math.sqrt(s.vx * s.vx + s.vy * s.vy);
            for (let i = 0; i < NUM_BANDS; i++) {
                const bandCenter = (i + 0.5) / NUM_BANDS;
                const dist = Math.abs(s.my - bandCenter);
                const proximity = Math.max(0, 1 - dist / 0.3);
                const activation = s.hoverTarget * proximity * (0.4 + Math.min(speed, 1) * 0.6);
                s.bandTargets[i] = activation;
            }
            for (let i = 0; i < NUM_BANDS; i++) {
                const rate = bandEaseRates[i];
                const current = s.bands[i] ?? 0;
                const target = s.bandTargets[i] ?? 0;
                s.bands[i] = current + (target - current) * rate;
                if (s.bands[i] < 0.001) {
                    s.bands[i] = 0;
                }
            }
            gl.uniform1f(uT, (performance.now() - t0) / 1e3);
            gl.uniform2f(uR, c.width, c.height);
            gl.uniform2f(uImgSize, s.imgW, s.imgH);
            gl.uniform2f(uVel, s.vx, s.vy);
            gl.uniform3f(uTint, tintVec[0], tintVec[1], tintVec[2]);
            const ts = tintStrengthRef.current;
            const defaultStrength = tint ? 0.35 : 0;
            const defaultInactive = tint ? 0.15 : 0;
            gl.uniform1f(uTintStrength, activeRef.current
                ? (ts?.active ?? defaultStrength)
                : (ts?.inactive ?? defaultInactive));
            for (let i = 0; i < NUM_BANDS; i++) {
                gl.uniform1f(uBands[i], s.bands[i]);
            }
            gl.bindTexture(gl.TEXTURE_2D, texture);
            gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
        };
        raf = requestAnimationFrame(loop);
        return () => {
            cancelAnimationFrame(raf);
            ro.disconnect();
            c.removeEventListener('pointermove', onMove);
            c.removeEventListener('pointerenter', onEnter);
            c.removeEventListener('pointerleave', onLeave);
            gl.deleteTexture(texture);
            gl.deleteProgram(prog);
            setLoaded(false);
        };
        // autoPlay is intentionally omitted so toggling it at runtime doesn't
        // tear down the shader pipeline. The ref-driven loop reads the live
        // value each frame, so listener attach/detach is handled once on mount.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [src, tier, tint]);
    if (tier === 0) {
        return (
        // eslint-disable-next-line @next/next/no-img-element
        _jsx("img", { alt: "", className: cn('absolute inset-0 h-full w-full object-cover', fallbackClassName ?? className), src: src, style: { mixBlendMode: 'overlay', ...style } }));
    }
    return (_jsx("canvas", { className: cn('absolute inset-0 h-full w-full transition-opacity duration-500', className), ref: canvasRef, style: {
            mixBlendMode: 'overlay',
            opacity: loaded ? 1 : 0,
            ...style
        } }));
}
//# sourceMappingURL=image-distortion.js.map