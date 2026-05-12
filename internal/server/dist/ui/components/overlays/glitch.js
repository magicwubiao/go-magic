'use client';
import { jsx as _jsx } from "react/jsx-runtime";
import { useEffect, useRef } from 'react';
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
const frag = /*glsl*/ `
  uniform float uTime, uAlpha, uIntensity, uChroma, uSpeed, uSparsity;
  uniform vec3 uColor;
  varying vec2 vUv;

  float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }

  vec2 hash2(vec2 p) {
    vec3 q = fract(vec3(p.xyx) * vec3(.1031, .1030, .0973));
    q += dot(q, q.yzx + 33.33);
    return fract((q.xx + q.yz) * q.zy);
  }

  float dither(vec2 p, float a) {
    return step(mod(floor(p.x) + floor(p.y) * 2.0, 4.0) / 4.0, a);
  }

  void main() {
    vec3 col = vec3(0.0);
    float t = uTime * uSpeed;
    float tSlow = floor(t / 3.0);
    float dit = dither(gl_FragCoord.xy * 0.5, 0.7);

    for (float i = 0.0; i < 6.0; i++) {
      float seed = i * 137.3;
      float epoch = floor((t + hash(vec2(seed, 77.7)) * 200.0) / (4.0 + hash(vec2(seed, 0.0)) * 6.0));
      float life = fract((t + hash(vec2(seed, 77.7)) * 200.0) / (4.0 + hash(vec2(seed, 0.0)) * 6.0));

      if (hash(vec2(epoch, seed)) > 1.0 - uSparsity * 0.7) continue;

      vec2 center = vec2(hash(vec2(epoch, seed + 13.1)), hash(vec2(epoch, seed + 27.3)));
      vec2 size = vec2(0.015 + hash(vec2(epoch, seed + 41.5)) * 0.08, 0.008 + hash(vec2(epoch, seed + 53.7)) * 0.04);
      vec2 d = abs(vUv - center);

      if (d.x < size.x && d.y < size.y) {
        float fade = smoothstep(0.0, 0.05, life) * smoothstep(1.0, 0.95, life);
        vec2 gUV = vUv + (hash2(vec2(epoch, seed + 200.0)) - 0.5) * 0.08 * uIntensity;
        float shift = uChroma * 0.015 * (sin(t * 2.0 + hash(vec2(epoch, seed)) * 6.28) * 0.3 + 0.7);

        col += uColor * vec3(
          hash(gUV * 50.0 + vec2(shift, 0.0) + epoch),
          hash(gUV * 50.0 + epoch),
          hash(gUV * 50.0 - vec2(shift, 0.0) + epoch)
        ) * dither(gl_FragCoord.xy * 0.5, fade * 0.8 + 0.2) * uIntensity * 0.7;
      }
    }

    for (float i = 0.0; i < 12.0; i++) {
      float seed = i * 211.7 + 1000.0;
      float epoch = floor((t + hash(vec2(seed, 77.7)) * 150.0) / (3.0 + hash(vec2(seed, 0.0)) * 5.0));
      float life = fract((t + hash(vec2(seed, 77.7)) * 150.0) / (3.0 + hash(vec2(seed, 0.0)) * 5.0));

      if (hash(vec2(epoch, seed)) > 1.0 - uSparsity * 0.5) continue;

      vec2 pos = vec2(hash(vec2(epoch, seed + 13.1)), hash(vec2(epoch, seed + 27.3)));
      float px = 0.003 + hash(vec2(epoch, seed + 41.5)) * 0.008;

      if (abs(vUv.x - pos.x) < px && abs(vUv.y - pos.y) < px) {
        float fade = smoothstep(0.0, 0.1, life) * smoothstep(1.0, 0.9, life);
        vec3 c = uColor;
        float cs = hash(vec2(epoch, seed + 700.0));

        if (cs < 0.2) c.r *= 1.8 * uChroma;
        else if (cs < 0.4) c.b *= 1.8 * uChroma;

        col += c * dither(gl_FragCoord.xy * 0.5, fade * 0.9) * uIntensity;
      }
    }

    float tearSize = 25.0 + uSparsity * 10.0;
    float tearThresh = 0.85 + uSparsity * 0.1;

    float hY = floor(vUv.y * tearSize);
    if (step(tearThresh, hash(vec2(hY, tSlow))) > 0.0) {
      float shift = (hash(vec2(hY, tSlow + 50.0)) - 0.5) * 0.25 * uIntensity;
      col += uColor * step(0.4, hash(vec2(vUv.x + shift, hY + tSlow))) * dit * uIntensity * 0.5;
    }

    float vX = floor(vUv.x * tearSize);
    if (step(tearThresh, hash(vec2(vX, tSlow + 100.0))) > 0.0) {
      float shift = (hash(vec2(vX, tSlow + 150.0)) - 0.5) * 0.25 * uIntensity;
      col += uColor * step(0.4, hash(vec2(vX + tSlow, vUv.y + shift))) * dit * uIntensity * 0.5;
    }

    gl_FragColor = vec4(col * uAlpha, max(col.r, max(col.g, col.b)) * uAlpha);
  }
`;
export function Glitch({ className, style }) {
    const gpuTier = useGpuTier();
    const c = useSmoothControls('Effects/Glitch', {
        alpha: { max: 2, min: 0, step: 0.01, value: 0.25 },
        blend: { options: BLEND_MODES, value: 'difference' },
        chroma: { max: 3, min: 0, step: 0.01, value: 1.17 },
        color: { value: '#ffe6cb' },
        enabled: { value: true },
        intensity: { max: 1, min: 0, step: 0.01, value: 0.59 },
        sparsity: { max: 1, min: 0, step: 0.01, value: 0.21 },
        speed: { max: 10, min: 0.1, step: 0.1, value: 1 }
    }, { collapsed: true });
    const ref = useRef(null);
    const cRef = useRef(c);
    cRef.current = c;
    const enabled = c.enabled && gpuTier > 0;
    useEffect(() => {
        if (!ref.current || !enabled) {
            return;
        }
        const renderer = new THREE.WebGLRenderer({
            alpha: true,
            canvas: ref.current
        });
        const camera = new THREE.OrthographicCamera(-1, 1, 1, -1, 0, 1);
        const geo = new THREE.PlaneGeometry(2, 2);
        const scene = new THREE.Scene();
        const mat = new THREE.ShaderMaterial({
            fragmentShader: frag,
            transparent: true,
            uniforms: {
                uAlpha: { value: c.alpha },
                uChroma: { value: c.chroma },
                uColor: { value: new THREE.Color(c.color) },
                uIntensity: { value: c.intensity },
                uSparsity: { value: c.sparsity },
                uSpeed: { value: c.speed },
                uTime: { value: 0 }
            },
            vertexShader: vert
        });
        scene.add(new THREE.Mesh(geo, mat));
        const resize = () => {
            renderer.setSize(innerWidth, innerHeight);
            renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
        };
        resize();
        window.addEventListener('resize', resize);
        let raf, time = 0;
        const interval = gpuTier === 1 ? 100 : 0;
        const loop = () => {
            raf = interval
                ? setTimeout(loop, interval)
                : requestAnimationFrame(loop);
            time += interval ? interval / 1000 : 0.016;
            const v = cRef.current;
            mat.uniforms.uTime.value = time;
            mat.uniforms.uAlpha.value = v.alpha;
            mat.uniforms.uChroma.value = v.chroma;
            mat.uniforms.uIntensity.value = v.intensity;
            mat.uniforms.uSparsity.value = v.sparsity;
            mat.uniforms.uSpeed.value = v.speed;
            mat.uniforms.uColor.value.set(v.color);
            renderer.render(scene, camera);
        };
        loop();
        return () => {
            window.removeEventListener('resize', resize);
            if (interval) {
                clearTimeout(raf);
            }
            else {
                cancelAnimationFrame(raf);
            }
            mat.dispose();
            geo.dispose();
            renderer.dispose();
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [enabled, gpuTier]);
    if (!enabled) {
        return null;
    }
    return (_jsx("canvas", { className: cn('h-full w-full', className), ref: ref, style: {
            mixBlendMode: c.blend,
            ...style
        } }));
}
//# sourceMappingURL=glitch.js.map