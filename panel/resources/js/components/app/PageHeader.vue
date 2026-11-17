<script setup lang="ts">
defineProps<{ title: string; description?: string }>();
</script>

<template>
    <header class="flex flex-wrap items-end justify-between gap-4">
        <div class="min-w-0">
            <h1 class="text-[1.625rem] leading-tight font-semibold text-balance">{{ title }}</h1>
            <p v-if="description" class="text-muted-foreground mt-1 max-w-2xl text-pretty">{{ description }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
            <slot />
        </div>
    </header>
</template>
