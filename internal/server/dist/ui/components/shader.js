'use client';
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useThree } from '@react-three/fiber';
import { useEffect, useRef } from 'react';
import * as THREE from 'three';
import { useCappedFrame } from '../../hooks/use-capped-frame';
const defaultUniforms = {
    uResolution: new THREE.Uniform(new THREE.Vector4()),
    uTime: new THREE.Uniform(0)
};
export function Shader({ children, defines, depthTest, fragmentShader, uniforms, vertexShader, ...props }) {
    const invalidate = useThree(st => st.invalidate);
    const { size, viewport } = useThree();
    const isMobile = size.width < 1024;
    const uniformsRef = useRef({
        ...defaultUniforms,
        ...(uniforms ?? {})
    });
    useCappedFrame(({ clock }) => {
        const w = size.width * viewport.dpr;
        const h = size.height * viewport.dpr;
        uniformsRef.current.uTime.value = clock.getElapsedTime();
        uniformsRef.current.uResolution.value.copy(new THREE.Vector4(w, h, w / h, viewport.dpr));
    }, isMobile ? 0 : 80);
    useEffect(() => void (uniforms && Object.assign(uniformsRef.current, uniforms)), [uniforms]);
    useEffect(() => void (isMobile && invalidate(80)), [invalidate, isMobile]);
    const materialProps = {
        defines: defines ?? {},
        depthTest,
        fragmentShader,
        side: THREE.DoubleSide,
        transparent: true,
        uniforms: uniformsRef.current,
        vertexShader
    };
    if (typeof children === 'function') {
        return children(_jsx("shaderMaterial", { ...materialProps }));
    }
    return (_jsxs("mesh", { ...props, children: [children ?? _jsx("planeGeometry", {}), _jsx("shaderMaterial", { ...materialProps })] }));
}
//# sourceMappingURL=shader.js.map