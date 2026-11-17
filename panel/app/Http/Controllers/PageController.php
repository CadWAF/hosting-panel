<?php

namespace App\Http\Controllers;

use App\Services\BackupClient;
use App\Services\CoreAgent;
use App\Services\CoreAgentUnavailable;
use App\Services\DbManagerClient;
use App\Services\MailClient;
use App\Services\NotifierClient;
use Inertia\Inertia;
use Inertia\Response;

/** Pages only read from the Services; every change goes through RpcController. A dead Service never breaks a page. */
class PageController extends Controller
{
    /** @param callable():array $fn */
    private function load(callable $fn, array $empty): array
    {
        try {
            return $fn() + ['available' => true];
        } catch (CoreAgentUnavailable) {
            return $empty + ['available' => false];
        }
    }

    public function sites(CoreAgent $core): Response
    {
        $d = $this->load(function () use ($core) {
            $installed = collect($core->php()['runtimes'] ?? [])->where('installed', true)->pluck('version')->values()->all();
            $keys = $core->settingKeys();
            sort($keys);

            return ['sites' => $core->sites(), 'installed' => $installed, 'settingKeys' => $keys];
        }, ['sites' => [], 'installed' => [], 'settingKeys' => []]);

        return Inertia::render('Sites', $d + ['phpVersions' => config('panel.php_versions')]);
    }

    public function site(string $domain, CoreAgent $core): Response
    {
        $d = $this->load(fn () => ['site' => $core->site($domain)], ['site' => []]);

        return Inertia::render('SiteDetail', $d + ['domain' => $domain]);
    }

    public function databases(DbManagerClient $db, CoreAgent $core): Response
    {
        $d = $this->load(fn () => ['engines' => $db->engines(), 'databases' => $db->databases()], ['engines' => [], 'databases' => []]);
        try {
            $sites = array_column($core->sites(), 'domain');
        } catch (CoreAgentUnavailable) {
            $sites = [];
        }

        return Inertia::render('Databases', $d + ['sites' => $sites]);
    }

    public function backups(BackupClient $b, CoreAgent $core, DbManagerClient $db): Response
    {
        $d = $this->load(fn () => ['destinations' => $b->destinations(), 'jobs' => $b->jobs(), 'runs' => $b->runs()],
            ['destinations' => [], 'jobs' => [], 'runs' => []]);
        try {
            $sites = collect($core->sites())->map(fn ($s) => ['domain' => $s['domain'], 'user' => $s['user']])->all();
        } catch (CoreAgentUnavailable) {
            $sites = [];
        }
        try {
            $databases = collect($db->databases())->map(fn ($x) => ['value' => $x['engine'].':'.$x['name'], 'label' => $x['engine'].' · '.$x['name']])->all();
        } catch (CoreAgentUnavailable) {
            $databases = [];
        }

        return Inertia::render('Backups', $d + ['sites' => $sites, 'databases' => $databases]);
    }

    public function mail(MailClient $m): Response
    {
        $d = $this->load(fn () => [
            'settings' => $m->settings(),
            'domains' => $m->domains(),
            'mailboxes' => collect($m->mailboxes())->groupBy('domain')->map->values()->all(),
        ], ['settings' => [], 'domains' => [], 'mailboxes' => (object) []]);

        return Inertia::render('Mail', $d);
    }

    public function php(CoreAgent $core): Response
    {
        $d = $this->load(function () use ($core) {
            $p = $core->php();

            return ['runtimes' => $p['runtimes'], 'extensions' => $p['extensions']];
        }, ['runtimes' => [], 'extensions' => []]);

        return Inertia::render('Php', $d);
    }

    public function tasks(CoreAgent $core): Response
    {
        return Inertia::render('Tasks', $this->load(fn () => ['tasks' => $core->tasks()], ['tasks' => []]));
    }

    public function services(CoreAgent $core, NotifierClient $notifier, DbManagerClient $db, BackupClient $backup, MailClient $mail): Response
    {
        try {
            $checks = $notifier->status();
        } catch (CoreAgentUnavailable) {
            $checks = [];
        }
        $services = [
            ['name' => 'Panel', 'up' => true, 'note' => __('You are looking at it')],
            ['name' => 'Core', 'up' => $core->isHealthy(), 'note' => __('Sites, PHP, web servers')],
            ['name' => 'Databases', 'up' => $db->isHealthy(), 'note' => __('Database servers and databases')],
            ['name' => 'Backups', 'up' => $backup->isHealthy(), 'note' => __('Scheduled backups and restores')],
            ['name' => 'Mail', 'up' => $mail->isHealthy(), 'note' => __('Mail domains and mailboxes')],
            ['name' => 'Notifier', 'up' => $notifier->isHealthy(), 'note' => __('Telegram and email alerts')],
        ];

        return Inertia::render('Services', compact('services', 'checks'));
    }
}
