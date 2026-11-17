import { createApp, h } from 'vue';
import type { DefineComponent } from 'vue';
import { createInertiaApp } from '@inertiajs/vue3';
import AppLayout from '@/layouts/AppLayout.vue';

const pages = import.meta.glob<{ default: DefineComponent & { layout?: unknown } }>('./pages/**/*.vue', { eager: true });

createInertiaApp({
    title: (title) => (title ? `${title} · MyGameHost Panel` : 'MyGameHost Panel'),
    resolve: (name) => {
        const page = pages[`./pages/${name}.vue`];
        if (!page) throw new Error(`Page not found: ${name}`);
        page.default.layout ??= name === 'Login' ? undefined : AppLayout;
        return page;
    },
    setup({ el, App, props, plugin }) {
        createApp({ render: () => h(App, props) }).use(plugin).mount(el);
    },
    progress: { color: 'var(--primary)' },
});
