import { computed } from 'vue';
import { router, usePage } from '@inertiajs/vue3';
import ru from '@/locales/ru';
import { postJson } from './useRpc';

export type Locale = 'en' | 'ru';

/**
 * Interface translations. The English text is the key, so a missing translation simply shows English.
 * Placeholders look like {name}.
 */
export function translate(locale: string, key: string, params?: Record<string, string | number>): string {
    let s = locale === 'ru' ? (ru[key] ?? key) : key;
    if (params) for (const [k, v] of Object.entries(params)) s = s.replaceAll(`{${k}}`, String(v));
    return s;
}

export function useI18n() {
    const page = usePage<{ locale: Locale }>();
    const locale = computed<Locale>(() => page.props.locale ?? 'en');
    const t = (key: string, params?: Record<string, string | number>) => translate(locale.value, key, params);
    async function setLocale(l: Locale) {
        await postJson('/locale', { locale: l });
        router.reload();
    }
    return { t, locale, setLocale };
}
