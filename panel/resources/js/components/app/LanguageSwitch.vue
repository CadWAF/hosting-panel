<script setup lang="ts">
import { LanguagesIcon } from '@lucide/vue';
import { Button } from '@/components/ui/button';
import { useI18n } from '@/composables/useI18n';

const { locale, setLocale, t } = useI18n();
</script>

<template>
    <Button variant="ghost" size="sm" class="gap-1.5 px-2 font-semibold" :aria-label="t('Language')" :title="t('Language')" @click="setLocale(locale === 'en' ? 'ru' : 'en')">
        <LanguagesIcon class="size-4" /> {{ locale === 'en' ? 'EN' : 'RU' }}
    </Button>
</template>
