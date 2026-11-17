<script setup lang="ts">
import { computed, ref } from 'vue';
import { Link, router, usePage } from '@inertiajs/vue3';
import {
    ActivityIcon, DatabaseIcon, GlobeIcon, ListChecksIcon, LogOutIcon, MailIcon, MenuIcon, MonitorIcon, MoonIcon, ArchiveIcon, ServerCogIcon, SunIcon, CodeXmlIcon,
} from '@lucide/vue';
import Logo from '@/components/app/Logo.vue';
import { Button } from '@/components/ui/button';
import { Sheet, SheetContent, SheetDescription, SheetTitle } from '@/components/ui/sheet';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { Toaster } from '@/components/ui/sonner';
import { useTheme, type Theme } from '@/composables/useTheme';

const page = usePage<{ auth: { email: string } }>();
const nav = [
    { label: 'Sites', href: '/sites', icon: GlobeIcon },
    { label: 'Databases', href: '/databases', icon: DatabaseIcon },
    { label: 'Backups', href: '/backups', icon: ArchiveIcon },
    { label: 'Mail', href: '/mail', icon: MailIcon },
    { label: 'PHP', href: '/php', icon: CodeXmlIcon },
    { label: 'Tasks', href: '/tasks', icon: ListChecksIcon },
    { label: 'Services', href: '/services', icon: ActivityIcon },
];
const path = computed(() => page.url.split('?')[0]);
const isActive = (href: string) => path.value === href || path.value.startsWith(href + '/');
const open = ref(false);
const { theme, set } = useTheme();
const logout = () => router.post('/logout');
</script>

<template>
    <div class="bg-background text-foreground min-h-svh lg:grid lg:grid-cols-[15rem_1fr]">
        <a href="#main" class="bg-primary text-primary-foreground sr-only z-50 rounded-md px-3 py-2 focus:not-sr-only focus:fixed focus:top-2 focus:left-2">Skip to content</a>

        <aside class="bg-sidebar text-sidebar-foreground border-sidebar-border sticky top-0 hidden h-svh flex-col border-r lg:flex">
            <div class="flex items-center gap-2.5 px-4 py-4">
                <Logo class="size-8" />
                <div class="leading-tight"><p class="text-sm font-semibold">MyGameHost</p><p class="text-sidebar-muted text-xs">Hosting Panel</p></div>
            </div>
            <nav class="flex flex-1 flex-col gap-0.5 px-2" aria-label="Main">
                <Link
                    v-for="n in nav" :key="n.href" :href="n.href"
                    class="text-sidebar-foreground/80 hover:bg-sidebar-accent hover:text-sidebar-foreground flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium transition-colors"
                    :class="{ 'bg-sidebar-accent text-sidebar-foreground': isActive(n.href) }"
                    :aria-current="isActive(n.href) ? 'page' : undefined"
                >
                    <component :is="n.icon" class="size-4" /> {{ n.label }}
                </Link>
            </nav>
            <div class="border-sidebar-border flex flex-col gap-3 border-t p-3">
                <ToggleGroup type="single" :model-value="theme" size="sm" variant="outline" class="w-full" aria-label="Theme" @update:model-value="(v) => v && set(v as Theme)">
                    <ToggleGroupItem value="light" aria-label="Light" class="flex-1"><SunIcon /></ToggleGroupItem>
                    <ToggleGroupItem value="system" aria-label="System" class="flex-1"><MonitorIcon /></ToggleGroupItem>
                    <ToggleGroupItem value="dark" aria-label="Dark" class="flex-1"><MoonIcon /></ToggleGroupItem>
                </ToggleGroup>
                <div class="flex items-center justify-between gap-2">
                    <span class="text-sidebar-muted min-w-0 truncate text-xs" :title="page.props.auth?.email">{{ page.props.auth?.email }}</span>
                    <Button variant="ghost" size="icon-sm" aria-label="Sign out" class="hover:bg-sidebar-accent" @click="logout"><LogOutIcon /></Button>
                </div>
            </div>
        </aside>

        <div class="flex min-w-0 flex-col">
            <header class="bg-background/90 sticky top-0 z-30 flex items-center justify-between border-b px-4 py-2.5 backdrop-blur lg:hidden">
                <div class="flex items-center gap-2"><Logo class="size-7" /><span class="text-sm font-semibold">MyGameHost</span></div>
                <Button variant="outline" size="icon" aria-label="Open menu" @click="open = true"><MenuIcon /></Button>
            </header>
            <Sheet v-model:open="open">
                <SheetContent side="left" class="bg-sidebar text-sidebar-foreground w-64 p-0">
                    <SheetTitle class="sr-only">Menu</SheetTitle>
                    <SheetDescription class="sr-only">Navigation</SheetDescription>
                    <div class="flex items-center gap-2.5 px-4 py-4"><Logo class="size-8" /><p class="text-sm font-semibold">MyGameHost</p></div>
                    <nav class="flex flex-col gap-0.5 px-2" aria-label="Main">
                        <Link
                            v-for="n in nav" :key="n.href" :href="n.href" @click="open = false"
                            class="hover:bg-sidebar-accent flex items-center gap-2.5 rounded-md px-3 py-2.5 text-sm font-medium"
                            :class="{ 'bg-sidebar-accent': isActive(n.href) }"
                        >
                            <component :is="n.icon" class="size-4" /> {{ n.label }}
                        </Link>
                    </nav>
                    <div class="border-sidebar-border mt-auto flex flex-col gap-3 border-t p-3">
                        <ToggleGroup type="single" :model-value="theme" size="sm" variant="outline" class="w-full" aria-label="Theme" @update:model-value="(v) => v && set(v as Theme)">
                            <ToggleGroupItem value="light" aria-label="Light" class="flex-1"><SunIcon /></ToggleGroupItem>
                            <ToggleGroupItem value="system" aria-label="System" class="flex-1"><MonitorIcon /></ToggleGroupItem>
                            <ToggleGroupItem value="dark" aria-label="Dark" class="flex-1"><MoonIcon /></ToggleGroupItem>
                        </ToggleGroup>
                        <Button variant="outline" @click="logout"><LogOutIcon /> Sign out</Button>
                    </div>
                </SheetContent>
            </Sheet>

            <main id="main" class="mx-auto flex w-full max-w-6xl flex-col gap-6 p-4 sm:p-6 lg:p-8"><slot /></main>
        </div>
        <Toaster position="bottom-right" rich-colors />
    </div>
</template>
