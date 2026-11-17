<script setup lang="ts">
import { ref } from 'vue';
import { router, usePage } from '@inertiajs/vue3';
import { LogOutIcon, MenuIcon } from '@lucide/vue';
import Logo from '@/components/app/Logo.vue';
import NavList from '@/components/app/NavList.vue';
import LanguageSwitch from '@/components/app/LanguageSwitch.vue';
import ThemeToggle from '@/components/app/ThemeToggle.vue';
import { Button } from '@/components/ui/button';
import { Sheet, SheetContent, SheetDescription, SheetTitle } from '@/components/ui/sheet';
import { Toaster } from '@/components/ui/sonner';
import { useI18n } from '@/composables/useI18n';

const page = usePage<{ auth: { email: string } }>();
const { t } = useI18n();
const open = ref(false);
const logout = () => router.post('/logout');
</script>

<template>
    <div class="bg-background text-foreground min-h-svh lg:grid lg:grid-cols-[16rem_1fr]">
        <a href="#main" class="bg-primary text-primary-foreground sr-only z-50 rounded-md px-3 py-2 focus:not-sr-only focus:fixed focus:top-2 focus:left-2">{{ t('Skip to content') }}</a>

        <aside class="bg-sidebar text-sidebar-foreground border-sidebar-border sticky top-0 hidden h-svh flex-col border-r lg:flex">
            <div class="flex items-center gap-2.5 px-5 py-5">
                <Logo class="size-8" />
                <div class="leading-tight"><p class="text-sm font-semibold tracking-tight">MyGameHost</p><p class="text-sidebar-muted text-xs">{{ t('Hosting Panel') }}</p></div>
            </div>
            <div class="flex-1 overflow-y-auto px-3 pb-4"><NavList /></div>
            <div class="border-sidebar-border flex items-center justify-between gap-2 border-t px-4 py-3">
                <span class="text-sidebar-muted min-w-0 truncate text-xs" :title="page.props.auth?.email">{{ page.props.auth?.email }}</span>
                <Button variant="ghost" size="icon-sm" :aria-label="t('Sign out')" :title="t('Sign out')" class="text-sidebar-muted hover:bg-sidebar-accent hover:text-sidebar-foreground" @click="logout"><LogOutIcon /></Button>
            </div>
        </aside>

        <div class="flex min-w-0 flex-col">
            <header class="bg-background/85 sticky top-0 z-30 flex h-14 items-center justify-between gap-2 border-b px-4 backdrop-blur sm:px-6 lg:px-8">
                <div class="flex items-center gap-2 lg:hidden">
                    <Button variant="outline" size="icon" :aria-label="t('Open menu')" @click="open = true"><MenuIcon /></Button>
                    <Logo class="size-7" />
                </div>
                <div class="hidden lg:block" />
                <div class="flex items-center gap-1"><LanguageSwitch /><ThemeToggle /></div>
            </header>
            <Sheet v-model:open="open">
                <SheetContent side="left" class="bg-sidebar text-sidebar-foreground w-72 p-0">
                    <SheetTitle class="sr-only">{{ t('Menu') }}</SheetTitle>
                    <SheetDescription class="sr-only">{{ t('Navigation') }}</SheetDescription>
                    <div class="flex items-center gap-2.5 px-5 py-5"><Logo class="size-8" /><p class="text-sm font-semibold">MyGameHost</p></div>
                    <div class="flex-1 overflow-y-auto px-3 pb-4"><NavList @navigate="open = false" /></div>
                    <div class="border-sidebar-border flex items-center justify-between gap-2 border-t p-3">
                        <span class="text-sidebar-muted min-w-0 truncate text-xs">{{ page.props.auth?.email }}</span>
                        <Button variant="ghost" size="sm" class="text-sidebar-muted" @click="logout"><LogOutIcon /> {{ t('Sign out') }}</Button>
                    </div>
                </SheetContent>
            </Sheet>

            <main id="main" class="mx-auto flex w-full max-w-7xl flex-col gap-6 p-4 sm:p-6 lg:p-8"><slot /></main>
        </div>
        <Toaster position="bottom-right" rich-colors />
    </div>
</template>
