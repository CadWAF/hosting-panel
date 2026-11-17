import { defineConfig } from 'vite';
import laravel from 'laravel-vite-plugin';
import tailwindcss from '@tailwindcss/vite';
import vue from '@vitejs/plugin-vue';
import path from 'node:path';
import fs from 'node:fs';
import ts from 'typescript';
import { registerTS } from 'vue/compiler-sfc';

registerTS(() => ts);

export default defineConfig({
    plugins: [
        laravel({ input: ['resources/css/app.css', 'resources/js/app.ts'], refresh: true }),
        vue({ script: { fs: { fileExists: fs.existsSync, readFile: (f) => fs.readFileSync(f, 'utf-8'), realpath: fs.realpathSync } }, template: { transformAssetUrls: { base: null, includeAbsolute: false } } }),
        tailwindcss(),
    ],
    resolve: { alias: { '@': path.resolve(import.meta.dirname, 'resources/js') } },
});
