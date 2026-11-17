import { ref } from 'vue';

export type Theme = 'light' | 'system' | 'dark';

function read(): Theme {
    try {
        const t = localStorage.getItem('hp-theme');
        return t === 'light' || t === 'dark' ? t : 'system';
    } catch {
        return 'system';
    }
}

const theme = ref<Theme>(read());

function apply(t: Theme) {
    const dark = t === 'system' ? matchMedia('(prefers-color-scheme: dark)').matches : t === 'dark';
    document.documentElement.classList.toggle('dark', dark);
}

export function useTheme() {
    function set(t: Theme) {
        theme.value = t;
        try {
            t === 'system' ? localStorage.removeItem('hp-theme') : localStorage.setItem('hp-theme', t);
        } catch {
            /* storage blocked: the choice just won't persist */
        }
        apply(t);
    }
    return { theme, set };
}
