<script setup lang="ts">
import { MonitorIcon, MoonIcon, SunIcon } from '@lucide/vue';
import { Button } from '@/components/ui/button';
import { useTheme, type Theme } from '@/composables/useTheme';
import { useI18n } from '@/composables/useI18n';

const { theme, set } = useTheme();
const { t } = useI18n();
const order: Theme[] = ['light', 'system', 'dark'];
const next = () => set(order[(order.indexOf(theme.value) + 1) % order.length]);
</script>

<template>
    <Button variant="ghost" size="icon-sm" :aria-label="`${t('Theme')}: ${t(theme === 'system' ? 'System' : theme === 'dark' ? 'Dark' : 'Light')}`" :title="t('Theme')" @click="next">
        <SunIcon v-if="theme === 'light'" /><MoonIcon v-else-if="theme === 'dark'" /><MonitorIcon v-else />
    </Button>
</template>
