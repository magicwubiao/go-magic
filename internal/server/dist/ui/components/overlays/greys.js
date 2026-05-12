'use client';
import { jsx as _jsx } from "react/jsx-runtime";
import { useEffect, useRef, useState } from 'react';
import * as THREE from 'three';
import { useGpuTier } from '../../../hooks/use-gpu-tier';
import { useSmoothControls } from '../../../hooks/use-smooth-controls';
import { cn } from '../../../utils';
import { BLEND_MODES } from '.';
const vert = /*glsl*/ `
  varying vec2 vUv;
  void main() {
    vUv = uv;
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
  }
`;
const sourceFrag = /*glsl*/ `
  uniform sampler2D uTex0, uTex1, uTex2, uTex3;
  uniform float uTime, uZoom, uSpeed, uRotate, uFolds, uDrift;
  varying vec2 vUv;

  vec3 gray(vec3 c) { return vec3(dot(c, vec3(.299, .587, .114))); }
  vec2 rot(vec2 p, float a) { return vec2(p.x * cos(a) - p.y * sin(a), p.x * sin(a) + p.y * cos(a)); }

  vec2 kaleid(vec2 p, float n) {
    float a = mod(atan(p.y, p.x), 6.28318 / n) - 3.14159 / n;
    return length(p) * vec2(cos(a), sin(a));
  }

  vec4 tex(int i, vec2 uv) {
    if (i == 0) return texture2D(uTex0, uv);
    if (i == 1) return texture2D(uTex1, uv);
    if (i == 2) return texture2D(uTex2, uv);
    return texture2D(uTex3, uv);
  }

  void main() {
    vec2 uv = rot(vUv - .5, uTime * uRotate * .05);
    if (uFolds > 1.) uv = kaleid(uv, uFolds);

    float dt = uTime * uDrift * .1;
    uv = uv / uZoom + .5 + vec2(sin(dt * .7) * cos(dt * .3), cos(dt * .5) * sin(dt * .9)) * .15 * uDrift;

    float cycle = mod(uTime * uSpeed * .01, 4.);
    int i0 = int(floor(cycle)), i1 = int(mod(float(i0) + 1., 4.));
    float t = smoothstep(0., 1., fract(cycle));

    vec3 base = mix(gray(vec3(1.) - tex(i0, uv).rgb), gray(vec3(1.) - tex(i1, uv).rgb), t);
    vec2 uvF = vec2(1. - uv.x, uv.y);
    vec3 flip = mix(gray(vec3(1.) - tex(i0, uvF).rgb), gray(vec3(1.) - tex(i1, uvF).rgb), t);

    gl_FragColor = vec4(mix(base, flip, .3 + sin(uTime * .2) * .2), 1.);
  }
`;
const moshFrag = /*glsl*/ `
  uniform sampler2D uCurrent, uPrev, uTex0, uTex1, uTex2, uTex3;
  uniform float uTime, uIntensity, uMotion, uZoom, uSpeed;
  uniform vec2 uRes;
  varying vec2 vUv;

  float hash(vec2 p) { return fract(sin(dot(p, vec2(12.9898, 78.233))) * 43758.5453); }
  vec2 hash2(vec2 p) { return fract(sin(vec2(dot(p, vec2(127.1, 311.7)), dot(p, vec2(269.5, 183.3)))) * 43758.5453); }

  float noise(vec2 p) {
    vec2 i = floor(p), f = fract(p) * fract(p) * (3. - 2. * fract(p));
    return mix(mix(hash(i), hash(i + vec2(1., 0.)), f.x), mix(hash(i + vec2(0., 1.)), hash(i + vec2(1., 1.)), f.x), f.y);
  }

  vec3 gray(vec3 c) { return vec3(dot(c, vec3(.299, .587, .114))); }

  vec2 distort(vec2 uv, float k, float t) {
    float n1 = noise(uv * 8. + t * .5), n2 = noise(uv * 12. + t * .7), flow = noise(uv * 4. + t * .3);
    return uv + vec2(cos(n1 * 6.28 + t * 1.2), sin(n2 * 6.28 + t * .9)) * .02 * k
             + vec2(cos(flow * 6.28 + uv.y * 10.), sin(flow * 6.28 + uv.x * 10.)) * .015 * k;
  }

  vec3 tex(int i, vec2 uv) {
    vec2 zuv = (uv - .5) / uZoom + .5;
    if (i == 0) return gray(vec3(1.) - texture2D(uTex0, zuv).rgb);
    if (i == 1) return gray(vec3(1.) - texture2D(uTex1, zuv).rgb);
    if (i == 2) return gray(vec3(1.) - texture2D(uTex2, zuv).rgb);
    return gray(vec3(1.) - texture2D(uTex3, zuv).rgb);
  }

  void main() {
    vec2 uv = vUv;
    float t = uTime * uSpeed, tS = floor(t * .1), pS = 80.;
    float amt = uIntensity * uMotion * .8 * (.7 + (sin(t * .5) * .5 + .5) * .3);

    vec2 mUV = distort(uv, uIntensity * .4, t);

    float hS = floor(uv.y * pS), hA = smoothstep(0., .8, hash(vec2(hS, tS)));
    float hO = (hash(vec2(hS, tS + 50.)) - .5) * .25 * hA * amt;
    float vS = floor(uv.x * pS), vA = smoothstep(0., .8, hash(vec2(vS, tS + 100.)));
    float vO = (hash(vec2(vS, tS + 150.)) - .5) * .25 * vA * amt;
    mUV += vec2(hO, vO);

    float bS = pS * .25;
    float hBA = step(.5, hash(vec2(floor(uv.y * bS), tS + 200.)));
    float hBO = (hash(vec2(floor(uv.y * bS), 200.)) - .5) * .35 * hBA * amt;
    float vBA = step(.5, hash(vec2(floor(uv.x * bS), tS + 300.)));
    float vBO = (hash(vec2(floor(uv.x * bS), 250.)) - .5) * .35 * vBA * amt;
    mUV += vec2(hBO, vBO);

    vec2 blk = floor(uv * pS * .15);
    mUV += (hash2(vec2(blk.x, blk.y + 500.)) - .5) * .4 * step(.7, hash(vec2(blk.x, blk.y + tS))) * amt;
    mUV = clamp(mUV, 0., 1.);

    vec3 prev = texture2D(uPrev, mUV).rgb;
    prev = mix(prev, texture2D(uPrev, clamp(uv + vec2(hBO, vBO), 0., 1.)).rgb, max(hBA, vBA) * .9);

    float tY = floor(uv.y * pS * .4);
    if (hash(vec2(tY, tS + 400.)) > .75) {
      prev = mix(prev, texture2D(uPrev, clamp(vec2(uv.x + (hash(vec2(tY, 400.)) - .5) * .5 * amt, uv.y), 0., 1.)).rgb, .85);
    }

    if (hA > 0. && amt > .01) {
      prev = mix(prev, gray(texture2D(uPrev, clamp(vec2(uv.x + (gray(prev).r - uv.x) * amt + hO, uv.y), 0., 1.)).rgb), hA);
    }

    float d = mix(mix(.97, .99, noise(uv * 8. + t * .2)), 0., step(.994, hash(vec2(tS, 0.))));
    gl_FragColor = vec4(mix(texture2D(uCurrent, uv).rgb, prev, d), 1.);
  }
`;
const outputFrag = /*glsl*/ `
  uniform sampler2D uInput;
  uniform float uTime, uAlpha, uHue;
  uniform vec3 uColor;
  varying vec2 vUv;

  float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }

  vec3 hueShift(vec3 c, float h) {
    float a = h * 6.28318, s = sin(a), co = cos(a);
    vec3 w = vec3(.299, .587, .114);
    return clamp(vec3(
      dot(c, w) + dot(c, vec3(.701, -.587, -.114) * co + vec3(.168, .330, -.497) * s),
      dot(c, w) + dot(c, vec3(-.299, .413, -.114) * co + vec3(.328, .035, -.363) * s),
      dot(c, w) + dot(c, vec3(-.299, -.587, .886) * co + vec3(-.497, .330, .168) * s)
    ), 0., 1.);
  }

  void main() {
    vec3 m = texture2D(uInput, vUv).rgb;
    m *= 1. - step(.5, fract(vUv.y * 200.)) * .06 * step(.97, hash(vec2(floor(vUv.y * 30.), floor(uTime * .5))));

    float lum = dot(m, vec3(.299, .587, .114));
    gl_FragColor = vec4(hueShift(mix(vec3(lum), uColor * lum * 2., length(uColor)), uHue) * uAlpha, smoothstep(.08, .18, lum * uAlpha));
  }
`;
const TEXTURES = [
    '/anatomy/grays-0.jpg',
    '/anatomy/grays-3.jpg',
    '/anatomy/grays-6.jpg',
    '/anatomy/grays-9.jpg'
];
export function Greys({ className, style }) {
    const gpuTier = useGpuTier();
    const [blendOverride, setBlendOverride] = useState(null);
    const canvasRef = useRef(null);
    const c = useSmoothControls('Effects/Greys', {
        alpha: { max: 1, min: 0, step: 0.01, value: 0.19 },
        blend: { options: BLEND_MODES, value: 'color-burn' },
        color: { value: '#ffac02' },
        drift: { max: 2, min: 0, step: 0.1, value: 0.5 },
        enabled: { value: false },
        folds: { max: 12, min: 1, step: 1, value: 1 },
        hue: { max: 1, min: 0, step: 0.01, value: 0.37 },
        intensity: { max: 3, min: 0, step: 0.1, value: 0.1 },
        motion: { max: 2, min: 0, step: 0.1, value: 0.1 },
        rotate: { max: 2, min: -2, step: 0.1, value: 0.3 },
        speed: { max: 1, min: 0.01, step: 0.01, value: 0.21 },
        zoom: { max: 4, min: 0.5, step: 0.1, value: 0.7 }
    }, { collapsed: true });
    const cRef = useRef(c);
    cRef.current = c;
    useEffect(() => {
        const onKey = (e) => e.key.toLowerCase() === 'x' &&
            setBlendOverride(p => (p === 'screen' ? null : 'screen'));
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, []);
    const enabled = c.enabled && gpuTier === 2;
    useEffect(() => {
        if (!canvasRef.current || !enabled) {
            return;
        }
        const renderer = new THREE.WebGLRenderer({
            alpha: true,
            canvas: canvasRef.current
        });
        const camera = new THREE.OrthographicCamera(-1, 1, 1, -1, 0, 1);
        const geo = new THREE.PlaneGeometry(2, 2);
        const [rtSource, rtA, rtB] = [0, 1, 2].map(() => new THREE.WebGLRenderTarget(innerWidth, innerHeight, {
            magFilter: THREE.NearestFilter,
            minFilter: THREE.NearestFilter
        }));
        const textures = TEXTURES.map(p => {
            const t = new THREE.TextureLoader().load(p);
            t.wrapS = t.wrapT = THREE.ClampToEdgeWrapping;
            t.minFilter = t.magFilter = THREE.LinearFilter;
            return t;
        });
        const texU = Object.fromEntries(textures.map((t, i) => [`uTex${i}`, { value: t }]));
        const srcU = {
            ...texU,
            uDrift: { value: 0 },
            uFolds: { value: 0 },
            uRotate: { value: 0 },
            uSpeed: { value: 0 },
            uTime: { value: 0 },
            uZoom: { value: 0 }
        };
        const moshU = {
            ...texU,
            uCurrent: { value: rtSource.texture },
            uIntensity: { value: 0 },
            uMotion: { value: 0 },
            uPrev: { value: rtA.texture },
            uRes: { value: new THREE.Vector2(innerWidth, innerHeight) },
            uSpeed: { value: 0 },
            uTime: { value: 0 },
            uZoom: { value: 0 }
        };
        const outU = {
            uAlpha: { value: 0 },
            uColor: { value: new THREE.Color() },
            uHue: { value: 0 },
            uInput: { value: rtB.texture },
            uTime: { value: 0 }
        };
        const mkScene = (frag, uniforms, transparent = false) => {
            const s = new THREE.Scene();
            s.add(new THREE.Mesh(geo.clone(), new THREE.ShaderMaterial({
                fragmentShader: frag,
                transparent,
                uniforms: uniforms,
                vertexShader: vert
            })));
            return s;
        };
        const srcScene = mkScene(sourceFrag, srcU);
        const moshScene = mkScene(moshFrag, moshU);
        const outScene = mkScene(outputFrag, outU, true);
        const resize = () => {
            renderer.setSize(innerWidth, innerHeight);
            renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
            [rtSource, rtA, rtB].forEach(rt => rt.setSize(innerWidth, innerHeight));
            moshU.uRes.value.set(innerWidth, innerHeight);
        };
        resize();
        window.addEventListener('resize', resize);
        let raf, ping = true, time = 0;
        const loop = () => {
            raf = requestAnimationFrame(loop);
            time += 0.016;
            const v = cRef.current;
            srcU.uTime.value = time;
            srcU.uSpeed.value = v.speed;
            srcU.uZoom.value = v.zoom;
            srcU.uRotate.value = v.rotate;
            srcU.uFolds.value = v.folds;
            srcU.uDrift.value = v.drift;
            moshU.uTime.value = time;
            moshU.uIntensity.value = v.intensity;
            moshU.uMotion.value = v.motion;
            moshU.uSpeed.value = v.speed;
            moshU.uZoom.value = v.zoom;
            outU.uTime.value = time;
            outU.uAlpha.value = v.alpha;
            outU.uHue.value = v.hue;
            outU.uColor.value.set(typeof v.color === 'string' ? v.color : '#fff');
            renderer.setRenderTarget(rtSource);
            renderer.render(srcScene, camera);
            const [read, write] = ping ? [rtA, rtB] : [rtB, rtA];
            moshU.uPrev.value = read.texture;
            renderer.setRenderTarget(write);
            renderer.render(moshScene, camera);
            outU.uInput.value = write.texture;
            renderer.setRenderTarget(null);
            renderer.render(outScene, camera);
            ping = !ping;
        };
        loop();
        return () => {
            window.removeEventListener('resize', resize);
            cancelAnimationFrame(raf);
            textures.forEach(t => t.dispose());
            [geo, rtSource, rtA, rtB, renderer].forEach(x => x.dispose());
        };
    }, [enabled]);
    if (!enabled) {
        return null;
    }
    return (_jsx("canvas", { className: cn('h-full w-full', className), ref: canvasRef, style: {
            mixBlendMode: (blendOverride ??
                c.blend),
            ...style
        } }));
}
//# sourceMappingURL=greys.js.map