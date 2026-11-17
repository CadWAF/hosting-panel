<script setup lang="ts">
import { TriangleAlertIcon } from '@lucide/vue';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { router } from '@inertiajs/vue3';

defineProps<{ name: string }>();
</script>

<template>
    <Alert variant="destructive" role="alert">
        <TriangleAlertIcon />
        <AlertTitle>{{ name }} service is not running</AlertTitle>
        <AlertDescription>
            <p>The panel and your sites are fine. This part is unavailable until the service is back.</p>
            <Button class="mt-2" variant="outline" size="sm" @click="router.reload()">Retry</Button>
        </AlertDescription>
    </Alert>
</template>
