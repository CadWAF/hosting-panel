<script setup lang="ts">
import { TriangleAlertIcon } from '@lucide/vue';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { router } from '@inertiajs/vue3';
import { useI18n } from '@/composables/useI18n';

const { t } = useI18n();

defineProps<{ name: string }>();
</script>

<template>
    <Alert variant="destructive" role="alert">
        <TriangleAlertIcon />
        <AlertTitle>{{ t('{name} service is not running', { name }) }}</AlertTitle>
        <AlertDescription>
            <p>{{ t('The panel and your sites are fine. This part is unavailable until the service is back.') }}</p>
            <Button class="mt-2" variant="outline" size="sm" @click="router.reload()">{{ t('Retry') }}</Button>
        </AlertDescription>
    </Alert>
</template>
