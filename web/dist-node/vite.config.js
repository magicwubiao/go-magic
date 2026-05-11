import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import { resolve } from 'path';
export default defineConfig({
    plugins: [vue()],
    resolve: {
        alias: {
            '@': resolve(__dirname, 'src'),
        },
    },
    server: {
        port: 5000,
        proxy: {
            '/api': {
                target: 'http://localhost:5001',
                changeOrigin: true,
            },
            '/ws': {
                target: 'ws://localhost:5001',
                ws: true,
            },
        },
    },
    build: {
        outDir: 'dist',
        sourcemap: false,
    },
});
