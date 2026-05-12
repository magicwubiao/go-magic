'use client';
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Canvas, useFrame, useThree } from '@react-three/fiber';
import { Suspense, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import * as THREE from 'three';
const GL = {
    alpha: true,
    antialias: true,
    depth: true,
    outputColorSpace: 'srgb',
    powerPreference: 'high-performance',
    stencil: false
};
const tmp = {
    camDir: new THREE.Vector3(),
    hit: new THREE.Vector3(),
    ndc: new THREE.Vector2(),
    origin: new THREE.Vector3(0, 0, 0),
    plane: new THREE.Plane(),
    ray: new THREE.Raycaster()
};
function useBounds(target) {
    const bounds = useRef(null);
    useLayoutEffect(() => {
        if (!target) {
            return;
        }
        const measure = () => {
            const b = target.getBoundingClientRect();
            bounds.current = {
                height: b.height,
                pageX: b.left + window.scrollX,
                pageY: b.top + window.scrollY,
                width: b.width
            };
        };
        measure();
        const ro = new ResizeObserver(measure);
        ro.observe(target);
        ro.observe(document.body);
        window.addEventListener('resize', measure, { passive: true });
        return () => {
            ro.disconnect();
            window.removeEventListener('resize', measure);
        };
    }, [target]);
    return bounds;
}
function PositionedGroup({ baseZoom, bounds, children }) {
    const ref = useRef(null);
    const { camera, size, viewport } = useThree();
    useFrame(() => {
        const g = ref.current;
        const b = bounds.current;
        if (!g || !b) {
            return;
        }
        const left = b.pageX - window.scrollX;
        const top = b.pageY - window.scrollY;
        tmp.ndc.set(((left + b.width / 2) / size.width) * 2 - 1, 1 - ((top + b.height / 2) / size.height) * 2);
        camera.getWorldDirection(tmp.camDir);
        tmp.plane.setFromNormalAndCoplanarPoint(tmp.camDir, tmp.origin);
        tmp.ray.setFromCamera(tmp.ndc, camera);
        const hit = tmp.ray.ray.intersectPlane(tmp.plane, tmp.hit);
        if (hit) {
            g.position.copy(hit);
        }
        const zoom = camera.zoom ?? 1;
        g.scale.setScalar(Math.min((b.width / size.width) * viewport.width, (b.height / size.height) * viewport.height) * (baseZoom > 0 ? zoom / baseZoom : 1));
    });
    return _jsx("group", { ref: ref, children: children });
}
export function SceneCanvas({ camera, children, className, contained, noEvents, style }) {
    const [container, setContainer] = useState(null);
    const baseZoom = camera?.zoom ?? 150;
    const bounds = useBounds(contained ? (container?.parentElement ?? null) : null);
    useEffect(() => {
        const el = contained && !noEvents ? container : null;
        if (!el) {
            return;
        }
        const lock = () => (document.body.style.userSelect = 'none');
        const unlock = () => (document.body.style.userSelect = '');
        el.addEventListener('pointerdown', lock);
        window.addEventListener('pointerup', unlock);
        return () => {
            el.removeEventListener('pointerdown', lock);
            window.removeEventListener('pointerup', unlock);
        };
    }, [container, contained, noEvents]);
    const cam = useMemo(() => ({
        far: camera?.far ?? 100,
        near: camera?.near ?? -100,
        position: camera?.position ?? [0, 0, 10],
        zoom: baseZoom * (contained ? 1 : 2)
    }), [baseZoom, camera, contained]);
    const canvas = (_jsx(Canvas, { camera: cam, className: className, dpr: [1, 2], eventPrefix: contained ? 'client' : 'offset', eventSource: contained ? (container ?? undefined) : undefined, gl: GL, orthographic: true, style: contained
            ? {
                height: '100dvh',
                inset: 0,
                pointerEvents: 'none',
                position: 'fixed',
                width: '100dvw',
                zIndex: 0,
                ...style
            }
            : { height: '100%', width: '100%', ...style }, children: contained ? (_jsx(PositionedGroup, { baseZoom: baseZoom, bounds: bounds, children: children() })) : (children()) }));
    return contained ? (_jsxs(Suspense, { children: [_jsx("div", { ref: setContainer, style: {
                    height: '100%',
                    inset: 0,
                    pointerEvents: noEvents ? 'none' : 'auto',
                    position: 'absolute',
                    width: '100%',
                    zIndex: 1
                } }), canvas] })) : (canvas);
}
//# sourceMappingURL=scene-canvas.js.map